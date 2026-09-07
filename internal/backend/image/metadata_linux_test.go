//go:build linux && amd64

package image

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"virmill.local/core/internal/backend/fileidentity"
)

func TestRealSingleFileMetadataDoesNotFollowBackingAndPinsOpenFile(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit generated-file confined metadata test required")
	}
	dir, work := privateTemp(t), privateTemp(t)
	name := filepath.Join(dir, "child.qcow2")
	// -u is used only to generate a trusted fixture declaration pointing at a
	// nonexistent host file. The production inspection command never uses -u/-U.
	fixtureQEMU(t, "create", "-f", "qcow2", "-u", "-b", filepath.Join(dir, "missing.raw"), "-F", "raw", name, "8M")
	f, initial, err := fileidentity.Open(name, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = os.Rename(name, name+".held"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, []byte("replacement is not a qcow2"), 0600); err != nil {
		t.Fatal(err)
	}
	info, pinned, err := (Tool{}).InspectFile(context.Background(), f, work, "qcow2")
	if err != nil {
		t.Fatal(err)
	}
	if pinned.Generation != initial.Generation || info.Backing != filepath.Join(dir, "missing.raw") || info.BackingFormat != "raw" || info.VirtualSize != 8<<20 {
		t.Fatalf("incorrect held-file declaration: %+v %+v", info, pinned)
	}
	if _, err = os.Stat(filepath.Join(dir, "missing.raw")); !os.IsNotExist(err) {
		t.Fatal("backing fixture unexpectedly exists", err)
	}
	t.Log("real confined QEMU metadata on a held generated file; missing backing was reported without exposure; no VM boot")
}

func TestSingleFileMetadataRejectsUnsafeInfo(t *testing.T) {
	valid := Info{Filename: "/source/image", Format: "qcow2", VirtualSize: 8192, Backing: "../base.raw", BackingFormat: "raw"}
	if err := checkFileInfo(valid, "qcow2"); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*Info){
		func(i *Info) { i.Dirty = true }, func(i *Info) { i.Encrypted = true }, func(i *Info) { i.Format = "raw" },
		func(i *Info) {
			i.Children = []Child{{Name: "external", Info: Info{Filename: "/host/secret", Format: "file"}}}
		},
		func(i *Info) { i.Specific = []byte(`{"type":"qcow2","data":{"data-file":"outside.raw"}}`) },
		func(i *Info) { i.Specific = []byte(`{"type":"qcow2","data":{"corrupt":true}}`) },
	} {
		bad := valid
		edit(&bad)
		if err := checkFileInfo(bad, "qcow2"); err == nil {
			t.Fatalf("unsafe metadata accepted: %+v", bad)
		}
	}
}
