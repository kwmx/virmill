//go:build linux

package fileidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityDetectsPathReplacementAndPreservesOpenObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "disk")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	f, original, err := Open(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	replacement, err := Observe(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Generation == original.Generation {
		t.Fatal("same-content replacement kept original identity")
	}
	pinned, err := InspectFile(f, false)
	if err != nil || pinned.Generation != original.Generation {
		t.Fatal("open object was redirected by rename", pinned, err)
	}
	if err = os.Link(path, path+".linked"); err != nil {
		t.Fatal(err)
	}
	if _, err = Observe(path, false); err == nil {
		t.Fatal("shared hard link accepted as exclusive managed file")
	}
}

func TestIdentityRefusesSymlinkComponentsAndSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "disk"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := Observe(filepath.Join(dir, "alias", "disk"), false); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	if _, err := Observe(target, false); err == nil {
		t.Fatal("directory accepted as disk")
	}
	if _, err := Observe("/dev/null", false); err == nil {
		t.Fatal("host special device accepted")
	}
}
