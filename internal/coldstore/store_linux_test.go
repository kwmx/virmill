//go:build linux && amd64

package coldstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/domain"
)

func fixture(t *testing.T) (protection.CaptureManifest, []Source) {
	t.Helper()
	vm := "11111111-2222-4333-8444-555555555555"
	m := protection.CaptureManifest{
		APIVersion: domain.APIVersion, Kind: "ColdRecoveryPoint", Version: 1,
		ID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", OperationID: "aaaaaaaa-bbbb-4ccc-8ddd-ffffffffffff",
		SourceVM:  domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: vm},
		StartedAt: time.Date(2026, 9, 8, 1, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 9, 8, 1, 0, 1, 0, time.UTC),
		SourceFingerprint: strings.Repeat("a", 64), StateBefore: "stopped", StateAfter: "stopped",
		NativeVersions:      map[string]string{"libvirt": "synthetic", "qemu": "synthetic", "swtpm": "synthetic"},
		Source:              domain.ColdSourceLayout{Architecture: "x86_64", Machine: "q35", State: domain.ColdStateLayout{VMID: vm, SecretReferences: []string{}, TPM: &domain.ColdTPM{Model: "tpm-crb", Version: "2.0", SourceType: "dir", SourcePath: "/generated/source/tpm"}}, External: []domain.ColdDependency{}, Disks: []domain.ColdDiskSource{{Target: "vda", Device: "disk", Bus: "virtio", Source: domain.ColdStorageSource{Type: "file", File: "/generated/source/disk.raw", Format: "raw"}, Backing: []domain.ColdStorageSource{}}}},
		Disks:               []protection.CapturedDisk{{Target: "vda", MemberID: "disk", Format: "raw", Independent: true}},
		PersistentXMLMember: "xml", EffectiveXMLMember: "xml", AuxiliaryInventoryMember: "inventory",
		TPMMembers: []protection.CapturedTPMFile{{Name: "permanent", MemberID: "tpm"}}, Secrets: []protection.CapturedSecret{}, IndependentlyRecoverable: true,
	}
	var sources []Source
	for _, item := range []struct {
		id, kind, name string
		data           []byte
	}{
		{"disk", "disk", "disks/vda.raw", bytes.Repeat([]byte("generated disk bytes"), 15000)},
		{"xml", "persistent-xml", "config/persistent.xml", []byte("<domain/>")},
		{"tpm", "tpm", "auxiliary/tpm/permanent", []byte{}},
		{"inventory", "auxiliary-inventory", "auxiliary/inventory.json", []byte(`{"testOnly":true}`)},
	} {
		member := protection.CaptureMember{ID: item.id, Kind: item.kind, Path: item.name, Size: int64(len(item.data)), SHA256: digest(item.data)}
		m.Members = append(m.Members, member)
		sources = append(sources, Source{Member: member, Reader: bytes.NewReader(item.data)})
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	return m, sources
}

func catalog(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// Only generated test directories are made removable after assertions. The
	// production API never changes published permissions or removes partials.
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				return os.Chmod(p, 0700)
			}
			return err
		})
	})
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func noReceipt(t *testing.T, got Receipt, err error) {
	t.Helper()
	if err == nil || !reflect.DeepEqual(got, Receipt{}) {
		t.Fatal("failure returned a receipt", got, err)
	}
}

func TestPublishExactSetPositionalReadersAndInspect(t *testing.T) {
	root := catalog(t)
	m, sources := fixture(t)
	// An advanced shared offset must not truncate transferred source content.
	f, err := os.CreateTemp(t.TempDir(), "held-source-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	data := bytes.Repeat([]byte("generated disk bytes"), 15000)
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	sources[0].Reader = f
	wantOffset, err := f.Seek(7, io.SeekStart)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Publish(context.Background(), root, m, sources)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != 1 || r.SnapshotID != m.ID || r.OperationID != m.OperationID || !reflect.DeepEqual(r.Manifest, m) {
		t.Fatal("publication binding changed", r)
	}
	if offset, err := f.Seek(0, io.SeekCurrent); err != nil || offset != wantOffset {
		t.Fatal("publication consumed the shared source offset", offset, err)
	}
	if _, err = os.Stat(filepath.Join(root, "."+m.ID+".partial")); !os.IsNotExist(err) {
		t.Fatal("staging survived successful rename", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, m.ID, "manifest.json"))
	if err != nil || digest(raw) != r.ManifestSHA256 {
		t.Fatal("manifest digest differs", err)
	}
	for _, member := range m.Members {
		p := filepath.Join(root, m.ID, member.Path)
		got, err := os.ReadFile(p)
		info, statErr := os.Lstat(p)
		if err != nil || statErr != nil || int64(len(got)) != member.Size || digest(got) != member.SHA256 || info.Mode().Perm() != 0400 {
			t.Fatal("member was not published exactly and privately", member.ID, err, statErr)
		}
	}
	inspected, err := Inspect(context.Background(), root, m.ID)
	if err != nil || !reflect.DeepEqual(inspected, r) {
		t.Fatal("inspection changed durable receipt", inspected, err)
	}
	raw, err = os.ReadFile(f.Name())
	if err != nil || !bytes.Equal(raw, data) {
		t.Fatal("source was modified", err)
	}
	before, _ := os.ReadFile(filepath.Join(root, m.ID, "receipt.json"))
	got, err := Publish(context.Background(), root, m, sources)
	noReceipt(t, got, err)
	after, _ := os.ReadFile(filepath.Join(root, m.ID, "receipt.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("replay overwrote the receipt")
	}
}

func TestPublishRejectsInputsBeforeCreatingStaging(t *testing.T) {
	for _, fault := range []string{"missing", "extra", "duplicate", "nil", "typed-nil", "mismatched", "escape", "reserved", "reserved-child", "reserved-case", "invalid-manifest", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			root := catalog(t)
			m, sources := fixture(t)
			ctx := context.Background()
			switch fault {
			case "missing":
				sources = sources[1:]
			case "extra":
				sources = append(sources, sources[0])
			case "duplicate":
				sources[1] = sources[0]
			case "nil":
				sources[0].Reader = nil
			case "typed-nil":
				sources[0].Reader = (*bytes.Reader)(nil)
			case "mismatched":
				sources[0].Member.SHA256 = strings.Repeat("f", 64)
			case "escape":
				m.Members[0].Path = "../outside"
			case "reserved":
				m.Members[0].Path = "manifest.json"
			case "reserved-child":
				m.Members[0].Path = "receipt.json/member"
			case "reserved-case":
				m.Members[0].Path = "Manifest.JSON"
			case "invalid-manifest":
				m.StateAfter = "running"
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			got, err := Publish(ctx, root, m, sources)
			noReceipt(t, got, err)
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("invalid input created capture state", entries, err)
			}
		})
	}
}

type readerAtFunc func([]byte, int64) (int, error)

func (f readerAtFunc) ReadAt(p []byte, off int64) (int, error) { return f(p, off) }

func TestPublishReaderFailureRetainsPartialNeverComplete(t *testing.T) {
	for _, fault := range []string{"short", "long", "hash", "zero-tpm-extra", "negative-count", "oversized-count", "no-progress", "read-error", "cancel-mid-copy"} {
		t.Run(fault, func(t *testing.T) {
			root := catalog(t)
			m, sources := fixture(t)
			ctx := context.Background()
			switch fault {
			case "short":
				sources[0].Reader = bytes.NewReader([]byte("short"))
			case "long":
				sources[0].Reader = bytes.NewReader(bytes.Repeat([]byte{'a'}, int(m.Members[0].Size)+1))
			case "hash":
				sources[0].Reader = bytes.NewReader(bytes.Repeat([]byte{'a'}, int(m.Members[0].Size)))
			case "zero-tpm-extra":
				sources[2].Reader = bytes.NewReader([]byte{'x'})
			case "negative-count":
				sources[0].Reader = readerAtFunc(func([]byte, int64) (int, error) { return -1, nil })
			case "oversized-count":
				sources[0].Reader = readerAtFunc(func(p []byte, _ int64) (int, error) { return len(p) + 1, nil })
			case "no-progress":
				sources[0].Reader = readerAtFunc(func([]byte, int64) (int, error) { return 0, nil })
			case "read-error":
				sources[0].Reader = readerAtFunc(func([]byte, int64) (int, error) { return 0, unix.EIO })
			case "cancel-mid-copy":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				original := sources[0].Reader
				sources[0].Reader = readerAtFunc(func(p []byte, off int64) (int, error) { n, err := original.ReadAt(p, off); cancel(); return n, err })
			}
			got, err := Publish(ctx, root, m, sources)
			noReceipt(t, got, err)
			partial := filepath.Join(root, "."+m.ID+".partial")
			if info, err := os.Stat(partial); err != nil || !info.IsDir() {
				t.Fatal("failed copy erased its partial state", err)
			}
			got, err = Inspect(context.Background(), root, m.ID)
			noReceipt(t, got, err)
			_, repaired := fixture(t)
			got, err = Publish(context.Background(), root, m, repaired)
			noReceipt(t, got, err)
		})
	}
}

type faultDisk struct {
	localDurability
	events          []string
	failAt          int
	afterRename     bool
	renameCollision func()
}

var injected = errors.New("injected durability boundary failure")

func (d *faultDisk) event(name string) error {
	d.events = append(d.events, name)
	if len(d.events) == d.failAt {
		return injected
	}
	return nil
}
func (d *faultDisk) Sync(f *os.File) error {
	if err := d.event("sync:" + f.Name()); err != nil {
		return err
	}
	return d.localDurability.Sync(f)
}
func (d *faultDisk) Rename(a int, b string, c int, name string) error {
	if err := d.event("rename"); err != nil {
		return err
	}
	if d.renameCollision != nil {
		d.renameCollision()
	}
	if err := d.localDurability.Rename(a, b, c, name); err != nil {
		return err
	}
	if d.afterRename {
		return injected
	}
	return nil
}

func TestPublishEveryDurabilityFailureAndLostAcknowledgement(t *testing.T) {
	m, sources := fixture(t)
	baseline := &faultDisk{}
	if _, err := publish(context.Background(), catalog(t), m, sources, baseline); err != nil {
		t.Fatal(err)
	}
	renameIndex := 0
	for i, event := range baseline.events {
		if event == "rename" {
			renameIndex = i + 1
			break
		}
	}
	if renameIndex == 0 {
		t.Fatal("publication never used atomic rename")
	}
	for _, member := range append(append([]protection.CaptureMember{}, m.Members...), protection.CaptureMember{Path: "manifest.json"}, protection.CaptureMember{Path: "receipt.json"}) {
		found := false
		for _, event := range baseline.events[:renameIndex-1] {
			found = found || event == "sync:"+member.Path
		}
		if !found {
			t.Fatal("member not synced before rename", member.Path)
		}
	}
	for point := 1; point <= len(baseline.events); point++ {
		t.Run(baseline.events[point-1], func(t *testing.T) {
			root := catalog(t)
			disk := &faultDisk{failAt: point}
			got, err := publish(context.Background(), root, m, sources, disk)
			noReceipt(t, got, err)
			if !errors.Is(err, injected) {
				t.Fatal("fault was not exercised", err)
			}
			got, err = Inspect(context.Background(), root, m.ID)
			if point <= renameIndex {
				noReceipt(t, got, err)
			} else if err != nil || got.SnapshotID != m.ID {
				t.Fatal("post-rename recovery could not inspect durable complete bytes", err)
			}
		})
	}
	t.Run("rename-acknowledgement-lost", func(t *testing.T) {
		root := catalog(t)
		got, err := publish(context.Background(), root, m, sources, &faultDisk{afterRename: true})
		noReceipt(t, got, err)
		got, err = Inspect(context.Background(), root, m.ID)
		if err != nil || got.OperationID != m.OperationID {
			t.Fatal("inspection did not recover renamed set", err)
		}
		got, err = Publish(context.Background(), root, m, sources)
		noReceipt(t, got, err)
	})
	t.Run("rename-does-not-replace-racing-destination", func(t *testing.T) {
		root := catalog(t)
		target := filepath.Join(root, m.ID)
		disk := &faultDisk{renameCollision: func() {
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "sentinel"), []byte("unrelated generated set"), 0600); err != nil {
				t.Fatal(err)
			}
		}}
		got, err := publish(context.Background(), root, m, sources, disk)
		noReceipt(t, got, err)
		if !errors.Is(err, unix.EEXIST) {
			t.Fatal("rename did not refuse existing destination", err)
		}
		b, err := os.ReadFile(filepath.Join(target, "sentinel"))
		if err != nil || string(b) != "unrelated generated set" {
			t.Fatal("rename replaced existing destination", err)
		}
	})
}

func TestInspectRejectsIncompleteTamperedAndSpecialSets(t *testing.T) {
	for _, fault := range []string{"extra", "extra-directory", "missing-member", "hash", "receipt-hash", "receipt-operation", "receipt-extra", "receipt-duplicate", "receipt-case-alias", "manifest", "symlink", "directory-symlink", "fifo", "hardlink", "writable-member"} {
		t.Run(fault, func(t *testing.T) {
			root := catalog(t)
			m, sources := fixture(t)
			if _, err := Publish(context.Background(), root, m, sources); err != nil {
				t.Fatal(err)
			}
			set := filepath.Join(root, m.ID)
			member := filepath.Join(set, m.Members[0].Path)
			for _, p := range []string{set, filepath.Dir(member)} {
				if err := os.Chmod(p, 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(p string, raw []byte) {
				t.Helper()
				if err := os.Chmod(p, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(p, 0400); err != nil {
					t.Fatal(err)
				}
			}
			switch fault {
			case "extra":
				if err := os.WriteFile(filepath.Join(set, "unexpected"), []byte("extra"), 0400); err != nil {
					t.Fatal(err)
				}
			case "extra-directory":
				if err := os.Mkdir(filepath.Join(set, "unexpected"), 0500); err != nil {
					t.Fatal(err)
				}
			case "missing-member":
				if err := os.Remove(member); err != nil {
					t.Fatal(err)
				}
			case "hash":
				write(member, bytes.Repeat([]byte{'x'}, int(m.Members[0].Size)))
			case "manifest":
				write(filepath.Join(set, "manifest.json"), []byte(`{}`))
			case "receipt-hash", "receipt-operation", "receipt-extra", "receipt-duplicate", "receipt-case-alias":
				p := filepath.Join(set, "receipt.json")
				raw, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				switch fault {
				case "receipt-hash":
					var r Receipt
					if err = json.Unmarshal(raw, &r); err != nil {
						t.Fatal(err)
					}
					r.ManifestSHA256 = strings.Repeat("0", 64)
					raw, _ = json.Marshal(r)
				case "receipt-operation":
					raw = bytes.Replace(raw, []byte(m.OperationID), []byte(m.ID), 1)
				case "receipt-extra":
					raw = append([]byte(`{"unexpected":true,`), raw[1:]...)
				case "receipt-duplicate":
					raw = append([]byte(`{"version":1,`), raw[1:]...)
				case "receipt-case-alias":
					raw = bytes.Replace(raw, []byte(`"version"`), []byte(`"Version"`), 1)
				}
				write(p, raw)
			case "symlink", "fifo", "hardlink":
				if err := os.Remove(member); err != nil {
					t.Fatal(err)
				}
				switch fault {
				case "symlink":
					if err := os.Symlink(filepath.Join(set, "manifest.json"), member); err != nil {
						t.Fatal(err)
					}
				case "fifo":
					if err := unix.Mkfifo(member, 0400); err != nil {
						t.Fatal(err)
					}
				case "hardlink":
					if err := os.Link(filepath.Join(set, "manifest.json"), member); err != nil {
						t.Fatal(err)
					}
				}
			case "directory-symlink":
				original := filepath.Dir(member)
				moved := filepath.Join(root, "moved-members")
				if err := os.Rename(original, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, original); err != nil {
					t.Fatal(err)
				}
			case "writable-member":
				if err := os.Chmod(member, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if fault != "directory-symlink" {
				if err := os.Chmod(filepath.Dir(member), 0500); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(set, 0500); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := Inspect(ctx, root, m.ID)
			noReceipt(t, got, err)
		})
	}
}

func TestCatalogIdentityModesAndNoSymlinkTraversal(t *testing.T) {
	for _, fault := range []string{"relative", "root", "noncanonical", "root-symlink", "ancestor-symlink", "public-mode", "replaced-during-copy"} {
		t.Run(fault, func(t *testing.T) {
			base := catalog(t)
			root := filepath.Join(base, "catalog")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			m, sources := fixture(t)
			switch fault {
			case "relative":
				root = "relative"
			case "root":
				root = "/"
			case "noncanonical":
				root += "/."
			case "root-symlink":
				target := root
				root = filepath.Join(base, "link")
				if err := os.Symlink(target, root); err != nil {
					t.Fatal(err)
				}
			case "ancestor-symlink":
				if err := os.Symlink(base, filepath.Join(base, "link")); err != nil {
					t.Fatal(err)
				}
				root = filepath.Join(base, "link", "catalog")
			case "public-mode":
				if err := os.Chmod(root, 0755); err != nil {
					t.Fatal(err)
				}
			case "replaced-during-copy":
				original := sources[0].Reader
				replaced := false
				sources[0].Reader = readerAtFunc(func(p []byte, off int64) (int, error) {
					if !replaced {
						replaced = true
						if err := os.Rename(root, root+"-retained"); err != nil {
							return 0, err
						}
						if err := os.Mkdir(root, 0700); err != nil {
							return 0, err
						}
					}
					return original.ReadAt(p, off)
				})
			}
			got, err := Publish(context.Background(), root, m, sources)
			noReceipt(t, got, err)
		})
	}
}

type crashDisk struct {
	localDurability
	point string
}

func (d crashDisk) Sync(f *os.File) error {
	if err := d.localDurability.Sync(f); err != nil {
		return err
	}
	if d.point == "member" && f.Name() == "disks/vda.raw" {
		os.Exit(86)
	}
	return nil
}
func (d crashDisk) Rename(a int, b string, c int, name string) error {
	if err := d.localDurability.Rename(a, b, c, name); err != nil {
		return err
	}
	if d.point == "rename" {
		os.Exit(86)
	}
	return nil
}

// The child exits without Go defers at a real generated-file boundary. This is
// coordinator process-loss evidence, not a simulated disk power-loss claim.
func TestPublishProcessExitRecovery(t *testing.T) {
	if root := os.Getenv("VIRMILL_COLDSTORE_CRASH_TEST_ROOT"); root != "" {
		m, sources := fixture(t)
		_, err := publish(context.Background(), root, m, sources, crashDisk{point: os.Getenv("VIRMILL_COLDSTORE_CRASH_TEST_POINT")})
		t.Fatal("crash boundary was not reached", err)
	}
	for _, point := range []string{"member", "rename"} {
		t.Run(point, func(t *testing.T) {
			root := catalog(t)
			m, sources := fixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPublishProcessExitRecovery$")
			cmd.Env = append(os.Environ(), "VIRMILL_COLDSTORE_CRASH_TEST_ROOT="+root, "VIRMILL_COLDSTORE_CRASH_TEST_POINT="+point)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 86 || ctx.Err() != nil {
				t.Fatalf("expected bounded worker process exit86: %v %s", err, output)
			}
			got, err := Inspect(context.Background(), root, m.ID)
			if point == "member" {
				noReceipt(t, got, err)
				if _, err = os.Stat(filepath.Join(root, "."+m.ID+".partial", m.Members[0].Path)); err != nil {
					t.Fatal("crash discarded written member", err)
				}
			} else if err != nil || got.OperationID != m.OperationID {
				t.Fatal("existing final set did not recover by inspection", err)
			}
			got, err = Publish(context.Background(), root, m, sources)
			noReceipt(t, got, err)
		})
	}
}
