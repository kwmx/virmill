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

func TestObjectsMatchAcrossAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pool", "disk")
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := Observe(path, false)
	if err != nil {
		t.Fatal(err)
	}
	want, err := GenerationObject(identity.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(path, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Dir(path), filepath.Join(dir, "dirlink")); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{path, filepath.Join(dir, "link"), filepath.Join(dir, "dirlink", "disk"), filepath.Join(dir, "pool", "..", "pool", "disk"), dir + "//pool/./disk"} {
		got, kind, e := Resolve(alias)
		if e != nil || got != want || kind != 0o100000 {
			t.Fatal("alias did not resolve to the same object", alias, got, want, e)
		}
	}
	f, err := os.Open(filepath.Join(dir, "link"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got, _, e := FileObject(f); e != nil || got != want {
		t.Fatal("open alias differs", got, e)
	}
	if err = os.WriteFile(filepath.Join(dir, "other"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, _, e := Resolve(filepath.Join(dir, "other")); e != nil || got == want {
		t.Fatal("distinct file matched", got, e)
	}
	for _, bad := range []string{"", "linux-statx-v1:1:2:3", "linux-statx-v1:1:2:3:4:5", "other:1:2:3:4:000000005"} {
		if _, e := GenerationObject(bad); e == nil {
			t.Fatal("malformed generation accepted", bad)
		}
	}
}
