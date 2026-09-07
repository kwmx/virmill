//go:build linux && amd64

package image

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackingGraphRejectsUnapprovedDependencies(t *testing.T) {
	members := map[string]bool{"top.qcow2": true, "base.raw": true}
	valid := []Info{{Filename: "/source/top.qcow2", Format: "qcow2", VirtualSize: 4096, Backing: "base.raw", FullBacking: "/source/base.raw", BackingFormat: "raw"}, {Filename: "/source/base.raw", Format: "raw", VirtualSize: 4096}}
	if err := CheckChain(valid, "qcow2", "top.qcow2", 8192, members); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func([]Info){
		func(v []Info) { v[0].Backing = "/etc/passwd" },
		func(v []Info) { v[0].Backing = "http://example.invalid/disk" },
		func(v []Info) { v[0].BackingFormat = "" },
		func(v []Info) { v[1].Filename = "/source/unlisted.raw" },
		func(v []Info) { v[0].Encrypted = true },
		func(v []Info) { v[0].VirtualSize = 1 << 40 },
		func(v []Info) {
			v[0].Specific = json.RawMessage(`{"type":"vmdk","data":{"extents":[{"filename":"/etc/passwd"}]}}`)
		},
	} {
		bad := append([]Info{}, valid...)
		edit(bad)
		if err := CheckChain(bad, "qcow2", "top.qcow2", 8192, members); err == nil {
			t.Fatal("unsafe image dependency accepted", bad)
		}
	}
}

func privateTemp(t *testing.T) string {
	t.Helper()
	p := t.TempDir()
	if err := os.Chmod(p, 0700); err != nil {
		t.Fatal(err)
	}
	return p
}
func fixtureQEMU(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("/usr/bin/qemu-img", args...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("trusted fixture generation: %v %s", err, b)
	}
}

func TestRealConfinedFormatConversions(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit disk-tool fixture run required; this is file conversion, not VM boot")
	}
	ctx := context.Background()
	tool := Tool{}
	id, err := tool.Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Actual converter: %s; executable SHA-256 %s", id.Version, id.SHA256)
	for _, format := range []string{"raw", "qcow2", "vmdk", "vdi", "vpc", "vhdx"} {
		t.Run(format, func(t *testing.T) {
			source := privateTemp(t)
			workspace := privateTemp(t)
			raw := filepath.Join(source, "fixture.raw")
			f, err := os.OpenFile(raw, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.Truncate(8 << 20); err != nil {
				t.Fatal(err)
			}
			if _, err = f.WriteAt([]byte("Virmill synthetic block-content fixture"), 4096); err != nil {
				t.Fatal(err)
			}
			f.Close()
			name := "fixture.raw"
			if format != "raw" {
				name = "disk." + format
				fixtureQEMU(t, "convert", "-f", "raw", "-O", format, raw, filepath.Join(source, name))
			}
			chain, err := tool.Inspect(ctx, source, workspace, name, format, 16<<20, map[string]bool{name: true})
			if err != nil {
				b, _ := run(ctx, source, workspace, 64<<20, "info", "--output=json", "--backing-chain", "-f", format, "/source/"+name)
				t.Logf("Fixture image information: %s", b)
				t.Fatal(err)
			}
			if err = tool.Convert(ctx, source, workspace, name, format, chain[0].VirtualSize, 32<<20); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(filepath.Join(workspace, "disk.qcow2")); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s source converted, checked and compared at %d virtual bytes; no VM defined or booted", format, chain[0].VirtualSize)
		})
	}
}

func TestRealMissingAndEscapingVMDKExtents(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit confined image fixture execution required")
	}
	source := privateTemp(t)
	workspace := privateTemp(t)
	fixtureQEMU(t, "create", "-f", "vmdk", "-o", "subformat=twoGbMaxExtentFlat", filepath.Join(source, "split.vmdk"), "8M")
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	member := map[string]bool{}
	extent := ""
	for _, entry := range entries {
		member[entry.Name()] = true
		if strings.Contains(entry.Name(), "-f001") {
			extent = entry.Name()
		}
	}
	if extent == "" {
		t.Fatal("split fixture extent missing")
	}
	if _, err = (Tool{}).Inspect(context.Background(), source, workspace, "split.vmdk", "vmdk", 16<<20, member); err != nil {
		t.Fatal("valid split fixture refused", err)
	}
	if err = os.Remove(filepath.Join(source, extent)); err != nil {
		t.Fatal(err)
	}
	if _, err = (Tool{}).Inspect(context.Background(), source, workspace, "split.vmdk", "vmdk", 16<<20, member); err == nil {
		t.Fatal("missing extent accepted")
	}
	descriptor, err := os.ReadFile(filepath.Join(source, "split.vmdk"))
	if err != nil {
		t.Fatal(err)
	}
	descriptor = []byte(strings.ReplaceAll(string(descriptor), extent, "/etc/passwd"))
	os.WriteFile(filepath.Join(source, "split.vmdk"), descriptor, 0600)
	if _, err = (Tool{}).Inspect(context.Background(), source, workspace, "split.vmdk", "vmdk", 16<<20, member); err == nil {
		t.Fatal("external extent accepted")
	}
}

func TestRealContainedBackingChainFlattensAndMissingBaseFails(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit isolated disk-tool fixture execution required")
	}
	source, workspace := privateTemp(t), privateTemp(t)
	raw := filepath.Join(source, "seed.raw")
	f, err := os.OpenFile(raw, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(8 << 20); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte("Virmill backing dependency fixture"), 4096); err != nil {
		t.Fatal(err)
	}
	f.Close()
	fixtureQEMU(t, "convert", "-f", "raw", "-O", "qcow2", raw, filepath.Join(source, "base.qcow2"))
	fixtureQEMU(t, "create", "-f", "qcow2", "-F", "qcow2", "-b", "base.qcow2", filepath.Join(source, "top.qcow2"))
	tool := Tool{}
	chain, err := tool.Inspect(context.Background(), source, workspace, "top.qcow2", "qcow2", 16<<20, map[string]bool{"top.qcow2": true, "base.qcow2": true})
	if err != nil || len(chain) != 2 {
		t.Fatal("contained backing graph rejected", chain, err)
	}
	if err = tool.Convert(context.Background(), source, workspace, "top.qcow2", "qcow2", chain[0].VirtualSize, 32<<20); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(source, "base.qcow2")); err != nil {
		t.Fatal(err)
	}
	if _, err = tool.Inspect(context.Background(), source, privateTemp(t), "top.qcow2", "qcow2", 16<<20, map[string]bool{"top.qcow2": true, "base.qcow2": true}); err == nil {
		t.Fatal("missing backing file accepted")
	}
}
