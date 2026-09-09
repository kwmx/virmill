//go:build linux && amd64

package importing

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/backend/image"
	platform "virmill.local/core/internal/platform/linux"
)

type metadataFixtureTool struct {
	FilesDiskTool // Unexpected hashing/conversion/plan calls panic in these tests.
	infos         map[string]image.Info
	calls         []string
	change        func()
}

func (f *metadataFixtureTool) DescribeFiles(ctx context.Context, sources []platform.DiskSourceFile, work, name string) (image.Info, error) {
	if err := ctx.Err(); err != nil {
		return image.Info{}, err
	}
	st, err := os.Stat(work)
	if err != nil || st.Mode().Perm() != 0700 {
		panic("metadata workspace is not private")
	}
	f.calls = append(f.calls, name)
	if f.change != nil {
		f.change()
	}
	return f.infos[name], nil
}
func sourceFixtureFile(t *testing.T, root, name string, size int64) string {
	t.Helper()
	p := filepath.Join(root, name)
	f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Truncate(size); e != nil {
		t.Fatal(e)
	}
	f.Close()
	return p
}
func sourceFixtureDescribe(t *testing.T, s *DiskSetService, p string) importer.SourceDescription {
	t.Helper()
	v, e := s.DescribeSource(context.Background(), uint32(os.Getuid()), app.Request{Path: p})
	if e != nil {
		t.Fatal(e)
	}
	return v.(importer.SourceDescription)
}
func TestSourceDescriptionISOUsesBoundedRecognitionWithoutDiskTool(t *testing.T) {
	root := t.TempDir()
	p := sourceFixtureFile(t, root, "installation.iso", 8<<30)
	f, e := os.OpenFile(p, os.O_WRONLY, 0)
	if e != nil {
		t.Fatal(e)
	}
	header := make([]byte, 72)
	header[0] = 1
	copy(header[1:], "CD001")
	header[6] = 1
	copy(header[40:], "INSTALL MEDIA                    ")
	if _, e = f.WriteAt(header, 16*2048); e != nil {
		t.Fatal(e)
	}
	f.Close()
	d := sourceFixtureDescribe(t, &DiskSetService{}, p)
	if d.Kind != "iso" || d.Name != "INSTALL MEDIA" || len(d.Disks) != 0 || d.PhysicalBytes != 8<<30 || !reflect.DeepEqual(d.Files, []string{"installation.iso"}) {
		t.Fatal(d)
	}
	b, _ := json.Marshal(d)
	if strings.Contains(string(b), "sha256") || strings.Contains(string(b), "vcpus") {
		t.Fatal("invented verification/hardware", string(b))
	}
}
func TestSourceDescriptionDiskAutoFormatAndBackingWarning(t *testing.T) {
	for _, format := range []string{"raw", "qcow2", "vmdk", "vdi", "vpc", "vhdx"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			p := sourceFixtureFile(t, root, "source.unknown", 1<<30)
			tool := &metadataFixtureTool{infos: map[string]image.Info{"source.unknown": {Format: format, VirtualSize: 4 << 30, Backing: "missing.raw", BackingFormat: "raw"}}}
			d := sourceFixtureDescribe(t, &DiskSetService{FilesTool: tool}, p)
			if d.Kind != "disk" || d.Format != format || d.Disks[0].VirtualBytes != 4<<30 || d.Disks[0].BackingPath != "missing.raw" || !strings.Contains(strings.Join(d.Warnings, " "), "missing") {
				t.Fatal(d)
			}
			if len(tool.calls) != 1 {
				t.Fatal(tool.calls)
			}
		})
	}
}
func TestSourceDescriptionDirectoryPreservesMultipleDisksAndExtentFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"boot.vmdk", "boot-flat.vmdk", "data.qcow2", "base.raw", "notes.txt"} {
		sourceFixtureFile(t, root, name, 4096)
	}
	if e := os.Mkdir(filepath.Join(root, "nested"), 0700); e != nil {
		t.Fatal(e)
	}
	tool := &metadataFixtureTool{infos: map[string]image.Info{
		"boot.vmdk":      {Format: "vmdk", VirtualSize: 1 << 30, Specific: []byte(`{"data":{"extents":[{"filename":"/source/boot-flat.vmdk"}]}}`)},
		"boot-flat.vmdk": {Format: "raw", VirtualSize: 1 << 30}, "data.qcow2": {Format: "qcow2", VirtualSize: 2 << 30, Backing: "base.raw", BackingFormat: "raw"}, "base.raw": {Format: "raw", VirtualSize: 2 << 30},
	}}
	d := sourceFixtureDescribe(t, &DiskSetService{FilesTool: tool}, root)
	if len(d.Disks) != 2 || d.Disks[0].Path != "boot.vmdk" || d.Disks[1].Path != "data.qcow2" || !reflect.DeepEqual(d.Files, []string{"base.raw", "boot-flat.vmdk", "boot.vmdk", "data.qcow2"}) {
		t.Fatal("extent became guest disk or missing selected dependencies", d)
	}
	if !strings.Contains(strings.Join(d.Warnings, " "), "Subfolders") {
		t.Fatal("nested scope not disclosed")
	}
}
func TestSourceDescriptionRejectsChangedSourceAndUnrelatedInputs(t *testing.T) {
	root := t.TempDir()
	p := sourceFixtureFile(t, root, "disk.raw", 4096)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &DiskSetService{}
	if _, e := s.DescribeSource(ctx, uint32(os.Getuid()), app.Request{Path: p}); e != context.Canceled {
		t.Fatal(e)
	}
	for _, r := range []app.Request{{Path: p, ID: "job"}, {Path: p, Input: map[string]any{"offlineSources": true}}, {Path: p, Connection: "qemu+ssh://other/system"}, {Path: "relative.raw"}} {
		if _, e := s.DescribeSource(context.Background(), uint32(os.Getuid()), r); e == nil {
			t.Fatal("invalid request accepted", r)
		}
	}
	if _, e := s.DescribeSource(context.Background(), uint32(os.Getuid())+1, app.Request{Path: p}); e == nil {
		t.Fatal("wrong user accepted")
	}
	link := filepath.Join(root, "symlink.raw")
	if e := os.Symlink(p, link); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DescribeSource(context.Background(), uint32(os.Getuid()), app.Request{Path: link}); e == nil {
		t.Fatal("symlink accepted")
	}
	tool := &metadataFixtureTool{infos: map[string]image.Info{"disk.raw": {Format: "raw", VirtualSize: 4096}}, change: func() {
		if e := os.WriteFile(p, []byte("changed fixture"), 0600); e != nil {
			t.Fatal(e)
		}
	}}
	if _, e := (&DiskSetService{FilesTool: tool}).DescribeSource(context.Background(), uint32(os.Getuid()), app.Request{Path: p}); e == nil || !strings.Contains(e.Error(), "changed") {
		t.Fatal("source replacement accepted", e)
	}
}
func TestSourceDescriptionRejectsExcessiveOrEmptySelection(t *testing.T) {
	root := t.TempDir()
	sourceFixtureFile(t, root, "empty.raw", 0)
	if _, e := (&DiskSetService{}).DescribeSource(context.Background(), uint32(os.Getuid()), app.Request{Path: root}); e == nil {
		t.Fatal("empty source accepted")
	}
	for _, ref := range []string{"/etc/shadow", "https://host/disk", "../../outside", "../source/base.raw"} {
		if sourceBacking("top.qcow2", ref) != "" {
			t.Fatal("outside backing exposed", ref)
		}
	}
	root = t.TempDir()
	for i := 0; i < 257; i++ {
		sourceFixtureFile(t, root, strings.Repeat("a", i/26+1)+string(rune('a'+i%26))+".raw", 1)
	}
	if _, e := (&DiskSetService{}).DescribeSource(context.Background(), uint32(os.Getuid()), app.Request{Path: root}); e == nil || !strings.Contains(e.Error(), "256") {
		t.Fatal("unbounded source directory", e)
	}
}

func TestSourceDescriptionContainerBytesNeverBecomeRawDisks(t *testing.T) {
	fixtures := map[string][]byte{"7z": {'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}, "zip": {'P', 'K', 3, 4}, "gzip": {0x1f, 0x8b}, "xz": {0xfd, '7', 'z', 'X', 'Z', 0}, "ovf": []byte("<?xml version='1.0'?><Envelope/>"), "vbox": []byte("<VirtualBox/>"), "vmx": []byte(".encoding = \"UTF-8\"")}
	tar := make([]byte, 512)
	copy(tar[257:], "ustar")
	fixtures["tar"] = tar
	for kind, bytes := range fixtures {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, "disguised.raw")
			if err := os.WriteFile(p, bytes, 0600); err != nil {
				t.Fatal(err)
			}
			tool := &metadataFixtureTool{}
			_, err := (&DiskSetService{FilesTool: tool}).DescribeSource(context.Background(), uint32(os.Getuid()), app.Request{Path: p})
			if err == nil || len(tool.calls) != 0 {
				t.Fatal("container reached QEMU raw fallback", err, tool.calls)
			}
		})
	}
}
