//go:build linux && amd64

package image

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"virmill.local/core/internal/backend/fileidentity"
	platform "virmill.local/core/internal/platform/linux"
)

func TestSourceDescriptionGraphValidation(t *testing.T) {
	for _, format := range []string{"raw", "qcow2", "vmdk", "vdi", "vpc", "vhdx"} {
		t.Run(format, func(t *testing.T) {
			info := Info{Filename: "/source/disk", Format: format, VirtualSize: 8192, Backing: "/unapproved/base.raw", BackingFormat: "raw"}
			if err := checkDescription(info, "disk", map[string]bool{"disk": true}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, edit := range []func(*Info){
		func(i *Info) { i.Format = "luks" }, func(i *Info) { i.Encrypted = true }, func(i *Info) { i.Dirty = true }, func(i *Info) { i.VirtualSize = 0 }, func(i *Info) { i.Filename = "/host/secret" },
		func(i *Info) {
			i.Children = []Child{{Name: "extent", Info: Info{Filename: "/source/missing", Format: "file"}}}
		},
		func(i *Info) { i.Specific = []byte(`{"data":{"data-file":"/source/external"}}`) },
		func(i *Info) { i.Specific = []byte(`{"data":{"extents":[{"filename":"/etc/shadow"}]}}`) },
	} {
		info := Info{Filename: "/source/disk", Format: "qcow2", VirtualSize: 8192}
		edit(&info)
		if err := checkDescription(info, "disk", map[string]bool{"disk": true}); err == nil {
			t.Fatal("unsafe graph accepted", info)
		}
	}
}
func TestSourceDescriptionRejectsInvalidSelectionBeforeTool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Tool{}).DescribeFiles(ctx, nil, t.TempDir(), "disk"); err != context.Canceled {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape", "/etc/shadow", "https://example.invalid/disk"} {
		if _, err := (Tool{}).DescribeFiles(context.Background(), nil, t.TempDir(), name); err == nil {
			t.Fatal("invalid file accepted")
		}
	}
}
func TestRealAutoDetectedSourceFormatsInConfinement(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit generated-file confined QEMU run required; no guest boot claim")
	}
	for _, format := range []string{"raw", "qcow2", "vmdk", "vdi", "vpc", "vhdx"} {
		t.Run(format, func(t *testing.T) {
			dir, work := privateTemp(t), privateTemp(t)
			name := filepath.Join(dir, "misleading.extension")
			fixtureQEMU(t, "create", "-f", format, name, "8M")
			f, id, err := fileidentity.Open(name, false, true)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			info, err := (Tool{}).DescribeFiles(context.Background(), []platform.DiskSourceFile{{Path: "disk", File: f}}, work, "disk")
			if err != nil {
				t.Fatal(err)
			}
			after, err := fileidentity.Observe(name, false)
			if err != nil || after != id {
				t.Fatal("source changed", err)
			}
			if info.Format != format || info.VirtualSize < 8<<20 {
				t.Fatalf("wrong autodetection: %+v", info)
			}
		})
	}
}
func TestRealAutoDescriptionMissingBackingRemainsUnopened(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit confined generated-file run required")
	}
	dir, work := privateTemp(t), privateTemp(t)
	name := filepath.Join(dir, "child.qcow2")
	fixtureQEMU(t, "create", "-f", "qcow2", "-u", "-b", "missing.raw", "-F", "raw", name, "8M")
	f, _, err := fileidentity.Open(name, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := (Tool{}).DescribeFiles(context.Background(), []platform.DiskSourceFile{{Path: "child.qcow2", File: f}}, work, "child.qcow2")
	if err != nil || info.Backing != "missing.raw" {
		t.Fatal("missing backing declaration not retained", info, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing.raw")); !os.IsNotExist(err) {
		t.Fatal("unexpected backing file", err)
	}
}

func TestRealAutoDescriptionVMDKExtentsRequireExplicitSelection(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit confined generated VMDK extent fixture required")
	}
	dir, work := privateTemp(t), privateTemp(t)
	name := filepath.Join(dir, "multi.vmdk")
	fixtureQEMU(t, "create", "-f", "vmdk", "-o", "subformat=twoGbMaxExtentSparse", name, "8M")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	sources := []platform.DiskSourceFile{}
	for _, entry := range entries {
		f, _, e := fileidentity.Open(filepath.Join(dir, entry.Name()), false, true)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		sources = append(sources, platform.DiskSourceFile{Path: entry.Name(), File: f})
	}
	if len(sources) < 2 {
		t.Fatal("fixture did not generate an external extent")
	}
	info, err := (Tool{}).DescribeFiles(context.Background(), sources, work, "multi.vmdk")
	if err != nil || info.Format != "vmdk" || info.VirtualSize != 8<<20 {
		t.Fatal("selected VMDK extent metadata unavailable", info, err)
	}
	for _, source := range sources {
		if source.Path == "multi.vmdk" {
			if _, err := (Tool{}).DescribeFiles(context.Background(), []platform.DiskSourceFile{source}, work, "multi.vmdk"); err == nil {
				t.Fatal("unselected extent was exposed")
			}
		}
	}
}
