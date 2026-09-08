//go:build linux && amd64

package coldstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealRestoredVerifiesBeforePermissionChange(t *testing.T) {
	for _, fault := range []string{"none", "digest", "member", "extra", "mode", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			root := catalog(t)
			m, sources := fixture(t)
			want, err := Publish(context.Background(), root, m, sources)
			if err != nil {
				t.Fatal(err)
			}
			set := filepath.Join(root, m.ID)
			if err := os.Chmod(set, 0700); err != nil {
				t.Fatal(err)
			}
			expected := want.ManifestSHA256
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "digest":
				expected = strings.Repeat("0", 64)
			case "member":
				f := filepath.Join(set, m.Members[0].Path)
				_ = os.Chmod(f, 0600)
				_ = os.WriteFile(f, []byte("corruption"), 0400)
				_ = os.Chmod(f, 0400)
			case "extra":
				_ = os.WriteFile(filepath.Join(set, "extra"), []byte("extra"), 0400)
			case "mode":
				_ = os.Chmod(filepath.Join(set, "manifest.json"), 0600)
			case "canceled":
				cancel()
			}
			got, err := SealRestored(ctx, root, m.ID, expected)
			st, statErr := os.Stat(set)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if fault != "none" {
				noReceipt(t, got, err)
				if st.Mode().Perm() != 0700 {
					t.Fatal("unverified set was sealed")
				}
				return
			}
			if err != nil || got.ManifestSHA256 != want.ManifestSHA256 || st.Mode().Perm() != 0500 {
				t.Fatal(got, err, st)
			}
			if _, err = SealRestored(ctx, root, m.ID, expected); err != nil {
				t.Fatal("lost-ack observation failed", err)
			}
			if _, err = Inspect(ctx, root, m.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type sealingFailure struct {
	target string
	failed bool
}

func (f *sealingFailure) Sync(file *os.File) error {
	st, err := file.Stat()
	if err != nil {
		return err
	}
	if file.Name() == f.target && st.Mode().Perm() == 0500 {
		f.failed = true
		return errors.New("generated sealing sync failure")
	}
	return file.Sync()
}
func (*sealingFailure) Rename(int, string, int, string) error { return errors.New("unexpected rename") }

func TestSealRestoredFailedFinalSyncRetainsVerifiableSet(t *testing.T) {
	root := catalog(t)
	m, sources := fixture(t)
	want, err := Publish(context.Background(), root, m, sources)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(root, m.ID), 0700); err != nil {
		t.Fatal(err)
	}
	cat, err := openCatalog(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer cat.file.Close()
	disk := &sealingFailure{target: m.ID}
	got, err := inspectSetMode(context.Background(), cat, m.ID, m.ID, disk, 0700, want.ManifestSHA256)
	noReceipt(t, got, err)
	if !disk.failed {
		t.Fatal("seal sync not exercised")
	}
	if _, err = SealRestored(context.Background(), root, m.ID, want.ManifestSHA256); err != nil {
		t.Fatal("retained sealed set not recoverable", err)
	}
}
