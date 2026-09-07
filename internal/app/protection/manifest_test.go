//go:build linux

package protection

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

func TestFirmwareTPMCompletenessAndIndependentArtifactChecks(t *testing.T) {
	dir := t.TempDir()
	m := Manifest{APIVersion: "virmill/v1", ID: "fixture-backup", VMID: "fixture-vm", Mode: "cold", Consistency: "cold-complete", HasNVRAM: true, HasTPM: true, IndependentlyRecoverable: true}
	for _, kind := range []string{"disk", "persistent-xml", "nvram", "tpm"} {
		b := []byte("synthetic " + kind)
		h := sha256.Sum256(b)
		os.WriteFile(filepath.Join(dir, kind), b, 0600)
		m.Required = append(m.Required, kind)
		m.Members = append(m.Members, Member{ID: kind, Kind: kind, Path: kind, Size: int64(len(b)), SHA256: hex.EncodeToString(h[:])})
	}
	if e := m.Verify(dir); e != nil {
		t.Fatal(e)
	}
	missing := m
	missing.Members = missing.Members[:3]
	if missing.Validate() == nil {
		t.Fatal("missing TPM accepted")
	}
	live := m
	live.Mode = "live-disk"
	if live.Validate() == nil {
		t.Fatal("live auxiliary completeness fabricated")
	}
	dependent := m
	dependent.ExternalSecrets = []string{"unavailable-secret"}
	if dependent.Validate() == nil {
		t.Fatal("external secret called independent")
	}
	os.WriteFile(filepath.Join(dir, "nvram"), []byte("corrupt"), 0600)
	if m.Verify(dir) == nil {
		t.Fatal("firmware corruption ignored")
	}
}

func manifestDeclaration() Manifest {
	m := Manifest{APIVersion: domain.APIVersion, ID: "backup-fixture", VMID: "vm-fixture", Mode: "cold", Consistency: "cold-complete"}
	for _, kind := range []string{"disk", "persistent-xml"} {
		manifestAddDeclaration(&m, kind, kind, []byte("synthetic "+kind))
	}
	return m
}

func manifestAddDeclaration(m *Manifest, id, kind string, data []byte) {
	hash := sha256.Sum256(data)
	m.Required = append(m.Required, id)
	m.Members = append(m.Members, Member{ID: id, Kind: kind, Path: id, Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:])})
}

func manifestFixture(t *testing.T) (Manifest, string) {
	t.Helper()
	m, root := manifestDeclaration(), t.TempDir()
	for _, member := range m.Members {
		if err := os.WriteFile(filepath.Join(root, member.Path), []byte("synthetic "+member.Kind), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return m, root
}

func TestManifestValidationRejectsUnboundedAndContradictoryClaims(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Manifest)
	}{
		{"version", func(m *Manifest) { m.APIVersion = "virmill/v2" }},
		{"empty-identity", func(m *Manifest) { m.ID = "" }},
		{"identity-control", func(m *Manifest) { m.VMID = "vm\nfixture" }},
		{"identity-format-control", func(m *Manifest) { m.ID = "vm\u200dhidden" }},
		{"identity-too-long", func(m *Manifest) { m.ID = strings.Repeat("a", 257) }},
		{"empty-consistency", func(m *Manifest) { m.Consistency = "" }},
		{"invented-consistency", func(m *Manifest) { m.Consistency = "boot-verified" }},
		{"cold-live-consistency", func(m *Manifest) { m.Consistency = "crash-consistent" }},
		{"live-cold-consistency", func(m *Manifest) { m.Mode = "live-disk" }},
		{"unknown-mode", func(m *Manifest) { m.Mode = "memory" }},
		{"no-members", func(m *Manifest) { m.Members = nil }},
		{"no-required", func(m *Manifest) { m.Required = nil }},
		{"empty-member-id", func(m *Manifest) { m.Members[0].ID = "" }},
		{"duplicate-member-id", func(m *Manifest) { m.Members[1].ID = m.Members[0].ID }},
		{"duplicate-required-id", func(m *Manifest) { m.Required[1] = m.Required[0] }},
		{"missing-required-id", func(m *Manifest) { m.Required[0] = "missing" }},
		{"undeclared-member", func(m *Manifest) { m.Required = m.Required[:1] }},
		{"unknown-kind", func(m *Manifest) { m.Members[0].Kind = "socket" }},
		{"missing-disk", func(m *Manifest) { m.Members[0].Kind = "secret" }},
		{"missing-xml", func(m *Manifest) { m.Members[1].Kind = "disk" }},
		{"duplicate-xml", func(m *Manifest) { manifestAddDeclaration(m, "other-xml", "persistent-xml", []byte("xml")) }},
		{"duplicate-live-xml", func(m *Manifest) {
			manifestAddDeclaration(m, "live-1", "live-xml", []byte("xml"))
			manifestAddDeclaration(m, "live-2", "live-xml", []byte("xml"))
		}},
		{"missing-nvram", func(m *Manifest) { m.HasNVRAM = true }},
		{"unexpected-nvram", func(m *Manifest) { manifestAddDeclaration(m, "nvram", "nvram", []byte("state")) }},
		{"duplicate-nvram", func(m *Manifest) {
			m.HasNVRAM = true
			manifestAddDeclaration(m, "nvram-1", "nvram", []byte("state"))
			manifestAddDeclaration(m, "nvram-2", "nvram", []byte("state"))
		}},
		{"missing-tpm", func(m *Manifest) { m.HasTPM = true }},
		{"unexpected-tpm", func(m *Manifest) { manifestAddDeclaration(m, "tpm", "tpm", []byte("state")) }},
		{"live-auxiliary-independent", func(m *Manifest) {
			m.Mode, m.Consistency, m.HasTPM, m.IndependentlyRecoverable = "live-disk", "filesystem-quiesced", true, true
			manifestAddDeclaration(m, "tpm", "tpm", []byte("state"))
		}},
		{"external-secret-independent", func(m *Manifest) { m.ExternalSecrets, m.IndependentlyRecoverable = []string{"external"}, true }},
		{"empty-secret-reference", func(m *Manifest) { m.ExternalSecrets = []string{""} }},
		{"duplicate-secret-reference", func(m *Manifest) { m.ExternalSecrets = []string{"ref", "ref"} }},
		{"zero-size", func(m *Manifest) { m.Members[0].Size = 0 }},
		{"negative-size", func(m *Manifest) { m.Members[0].Size = -1 }},
		{"size-limit", func(m *Manifest) { m.Members[0].Size = manifestMemberSizeLimit + 1 }},
		{"size-overflow", func(m *Manifest) { m.Members[0].Size = math.MaxInt64 }},
		{"empty-digest", func(m *Manifest) { m.Members[0].SHA256 = "" }},
		{"wrong-digest-width", func(m *Manifest) { m.Members[0].SHA256 = strings.Repeat("a", 62) }},
		{"invalid-digest", func(m *Manifest) { m.Members[0].SHA256 = strings.Repeat("g", 64) }},
		{"uppercase-digest", func(m *Manifest) { m.Members[0].SHA256 = strings.ToUpper(m.Members[0].SHA256) }},
		{"duplicate-path", func(m *Manifest) { m.Members[1].Path = strings.ToUpper(m.Members[0].Path) }},
		{"absolute-path", func(m *Manifest) { m.Members[0].Path = "/tmp/outside" }},
		{"dot-dot-path", func(m *Manifest) { m.Members[0].Path = "../outside" }},
		{"noncanonical-path", func(m *Manifest) { m.Members[0].Path = "member/../disk" }},
		{"trailing-slash", func(m *Manifest) { m.Members[0].Path = "disk/" }},
		{"backslash-path", func(m *Manifest) { m.Members[0].Path = `dir\disk` }},
		{"path-file-parent-conflict", func(m *Manifest) { m.Members[1].Path = "DISK/child" }},
		{"too-many-members", func(m *Manifest) {
			for i := 0; i < manifestMemberLimit; i++ {
				manifestAddDeclaration(m, fmt.Sprintf("disk-%d", i), "disk", []byte("x"))
			}
		}},
		{"too-many-references", func(m *Manifest) {
			for i := 0; i <= manifestReferenceLimit; i++ {
				m.ExternalSecrets = append(m.ExternalSecrets, fmt.Sprintf("ref-%d", i))
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := manifestDeclaration()
			tc.edit(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("invalid declaration accepted")
			}
		})
	}
}

func TestManifestValidationPreservesSupportedDeclarationsAndFiniteBounds(t *testing.T) {
	for _, consistency := range []string{"crash-consistent", "filesystem-quiesced", "application-quiesced"} {
		m := manifestDeclaration()
		m.Mode, m.Consistency = "live-disk", consistency
		manifestAddDeclaration(&m, "live", "live-xml", []byte("effective XML"))
		manifestAddDeclaration(&m, "secret", "secret", []byte("encrypted secret"))
		m.ExternalSecrets = []string{"operator-retained-reference"}
		if err := m.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	m := manifestDeclaration()
	for len(m.Members) < manifestMemberLimit {
		manifestAddDeclaration(&m, fmt.Sprintf("disk-%d", len(m.Members)), "disk", []byte("x"))
	}
	if err := m.Validate(); err != nil {
		t.Fatal("maximum bounded member count rejected", err)
	}
	m = manifestDeclaration()
	manifestAddDeclaration(&m, "disk-2", "disk", []byte("x"))
	manifestAddDeclaration(&m, "disk-3", "disk", []byte("x"))
	for i := range m.Members {
		m.Members[i].Size = manifestMemberSizeLimit
	}
	if err := m.Validate(); err != nil {
		t.Fatal("exact aggregate limit rejected", err)
	}
	manifestAddDeclaration(&m, "one-byte-too-many", "disk", []byte("x"))
	if err := m.Validate(); err == nil {
		t.Fatal("aggregate size limit was bypassed")
	}
}

func TestManifestEveryDeclaredMemberMustMatch(t *testing.T) {
	for _, change := range []string{"missing", "shorter", "longer", "same-size-corruption", "directory"} {
		t.Run(change, func(t *testing.T) {
			m, root := manifestFixture(t)
			filename := filepath.Join(root, "persistent-xml")
			switch change {
			case "missing", "directory":
				if err := os.Remove(filename); err != nil {
					t.Fatal(err)
				}
				if change == "directory" {
					if err := os.Mkdir(filename, 0700); err != nil {
						t.Fatal(err)
					}
				}
			case "shorter":
				if err := os.WriteFile(filename, []byte("short"), 0600); err != nil {
					t.Fatal(err)
				}
			case "longer":
				if err := os.WriteFile(filename, []byte(strings.Repeat("x", 100)), 0600); err != nil {
					t.Fatal(err)
				}
			case "same-size-corruption":
				if err := os.WriteFile(filename, bytes.Repeat([]byte{'x'}, int(m.Members[1].Size)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.VerifyContext(context.Background(), root); err == nil {
				t.Fatal("a valid first disk hid an invalid later declared member")
			}
		})
	}
}

func TestManifestRefusesSymlinkComponentsAndAcceptsOrdinaryNestedFiles(t *testing.T) {
	for _, kind := range []string{"member-inside", "member-outside", "ancestor", "root"} {
		t.Run(kind, func(t *testing.T) {
			m, root := manifestFixture(t)
			selected := root
			switch kind {
			case "member-inside", "member-outside":
				target := filepath.Join(root, "retained-disk")
				if kind == "member-outside" {
					target = filepath.Join(t.TempDir(), "retained-disk")
				}
				if err := os.Rename(filepath.Join(root, "disk"), target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(root, "disk")); err != nil {
					t.Fatal(err)
				}
			case "ancestor":
				if err := os.Mkdir(filepath.Join(root, "real"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(root, "disk"), filepath.Join(root, "real", "disk")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("real", filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
				m.Members[0].Path = "alias/disk"
			case "root":
				selected = filepath.Join(t.TempDir(), "root-link")
				if err := os.Symlink(root, selected); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.Verify(selected); err == nil {
				t.Fatal("symlink path accepted")
			}
		})
	}
	m, root := manifestFixture(t)
	if err := os.Mkdir(filepath.Join(root, "ordinary"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "disk"), filepath.Join(root, "ordinary", "disk")); err != nil {
		t.Fatal(err)
	}
	m.Members[0].Path = "ordinary/disk"
	if err := m.Verify(root); err != nil {
		t.Fatal("ordinary nested member rejected", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(relative); err != nil {
		t.Fatal("canonical relative artifact root rejected", err)
	}
}

func TestManifestHeldSetDetectsSubstitutionAndMetadataChanges(t *testing.T) {
	for _, mutation := range []string{"regular-replacement", "symlink-replacement", "fifo-replacement", "hardlink-added", "same-size-write-restored-mtime", "mode-change", "root-replacement"} {
		t.Run(mutation, func(t *testing.T) {
			m, directory := manifestFixture(t)
			root, err := openManifestRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer root.file.Close()
			var held []manifestHeldFile
			for _, member := range m.Members {
				f, err := root.openMember(member.Path, true)
				if err != nil {
					t.Fatal(err)
				}
				defer f.file.Close()
				held = append(held, f)
			}
			// Verify a member, then disturb it while another member is still held.
			if _, err := copyManifestBytes(context.Background(), io.Discard, held[0].file, m.Members[0].Size); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(directory, "disk")
			switch mutation {
			case "regular-replacement", "symlink-replacement", "fifo-replacement":
				if err := os.Rename(filename, filename+".retained"); err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "regular-replacement":
					if err := os.WriteFile(filename, []byte("synthetic disk"), 0600); err != nil {
						t.Fatal(err)
					}
				case "symlink-replacement":
					if err := os.Symlink("disk.retained", filename); err != nil {
						t.Fatal(err)
					}
				case "fifo-replacement":
					if err := unix.Mkfifo(filename, 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "hardlink-added":
				if err := os.Link(filename, filepath.Join(t.TempDir(), "outside-alias")); err != nil {
					t.Fatal(err)
				}
			case "same-size-write-restored-mtime":
				st, err := os.Stat(filename)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, bytes.Repeat([]byte{'x'}, int(st.Size())), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(filename, st.ModTime(), st.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "mode-change":
				if err := os.Chmod(filename, 0400); err != nil {
					t.Fatal(err)
				}
			case "root-replacement":
				if err := os.Rename(directory, directory+"-retained"); err != nil {
					t.Fatal(err)
				}
				defer os.RemoveAll(directory + "-retained")
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := root.recheck(context.Background(), held); err == nil {
				t.Fatal("held member or directory substitution was accepted")
			}
		})
	}
}

func TestManifestFIFOReplacementDoesNotBlock(t *testing.T) {
	if mode := os.Getenv("VIRMILL_MANIFEST_FIFO_CHILD"); mode != "" {
		m, directory := manifestFixture(t)
		filename := filepath.Join(directory, "disk")
		if mode == "document" {
			filename = filepath.Join(directory, "manifest.json")
		} else {
			// Model the old inspection-to-open gap deterministically. The current
			// verifier must reject the FIFO in its bounded open, before reading.
			if _, err := os.Lstat(filename); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filename); err != nil {
				t.Fatal(err)
			}
		}
		if err := unix.Mkfifo(filename, 0600); err != nil {
			t.Fatal(err)
		}
		var err error
		if mode == "document" {
			_, err = ReadManifestContext(context.Background(), filename)
		} else {
			err = m.Verify(directory)
		}
		if err == nil {
			t.Fatal("FIFO accepted as a recovery file")
		}
		return
	}
	for _, mode := range []string{"member", "document"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestManifestFIFOReplacementDoesNotBlock$", "-test.count=1")
			cmd.Env = append(os.Environ(), "VIRMILL_MANIFEST_FIFO_CHILD="+mode)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("opening a replacement FIFO blocked until subprocess deadline")
			}
			if err != nil {
				t.Fatalf("FIFO refusal regression failed: %v\n%s", err, out)
			}
		})
	}
}

type manifestCancelReader struct {
	cancel context.CancelFunc
	read   bool
}

func (r *manifestCancelReader) Read(b []byte) (int, error) {
	r.read = true
	copy(b, "data")
	r.cancel()
	return 4, nil
}

func TestManifestCancellationAndBoundedReads(t *testing.T) {
	m, root := manifestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.VerifyContext(ctx, filepath.Join(root, "does-not-exist")); !errors.Is(err, context.Canceled) {
		t.Fatal("verification ignored cancellation before filesystem access", err)
	}
	if got, err := ReadManifestContext(ctx, filepath.Join(root, "does-not-exist")); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, Manifest{}) {
		t.Fatal("manifest read ignored cancellation or returned partial data", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	r := &manifestCancelReader{cancel: cancel}
	var out bytes.Buffer
	if _, err := copyManifestBytes(ctx, &out, r, 100); !errors.Is(err, context.Canceled) || !r.read || out.Len() != 0 {
		t.Fatal("cancellation after read was not checked before accepting bytes", err)
	}
	var bounded bytes.Buffer
	n, err := copyManifestBytes(context.Background(), &bounded, strings.NewReader("123456789"), 3)
	if err != nil || n != 4 || bounded.String() != "1234" {
		t.Fatal("reader exceeded declared bytes plus one", n, err)
	}
}

func TestReadManifestContextStrictStableBoundedFile(t *testing.T) {
	m, root := manifestFixture(t)
	valid, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(filename, valid, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadManifestContext(context.Background(), filename)
	if err != nil || !reflect.DeepEqual(m, got) {
		t.Fatal("valid unchanged legacy manifest did not round trip", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, filename)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ReadManifestContext(context.Background(), relative); err != nil || !reflect.DeepEqual(m, got) {
		t.Fatal("canonical relative manifest path rejected", err)
	}
	if err := got.VerifyContext(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"unknown-field", []byte(strings.TrimSuffix(string(valid), "}") + `,"invented":true}`)},
		{"duplicate-field", []byte(strings.Replace(string(valid), `"backupID":`, `"backupID":"first","backupID":`, 1))},
		{"escaped-duplicate-field", []byte(strings.Replace(string(valid), `"backupID":`, `"backup\u0049D":"first","backupID":`, 1))},
		{"trailing-value", append(append([]byte{}, valid...), []byte(` {}`)...)},
		{"future-version", []byte(strings.Replace(string(valid), domain.APIVersion, "virmill/v2", 1))},
		{"invalid-kind", []byte(strings.Replace(string(valid), `"kind":"disk"`, `"kind":"unknown"`, 1))},
		{"empty-file", nil},
		{"truncated", valid[:len(valid)-1]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filename, tc.raw, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := ReadManifestContext(context.Background(), filename)
			if err == nil || !reflect.DeepEqual(got, Manifest{}) {
				t.Fatal("invalid document returned success or a partial manifest", err)
			}
		})
	}
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(manifestDocumentLimit + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifestContext(context.Background(), filename); err == nil {
		t.Fatal("oversized sparse manifest was accepted")
	}
}

func TestReadManifestRefusesAliasesAndHiddenTraversal(t *testing.T) {
	for _, kind := range []string{"symlink", "ancestor-symlink", "hardlink", "directory", "hidden-traversal"} {
		t.Run(kind, func(t *testing.T) {
			m, root := manifestFixture(t)
			body, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(root, "manifest.json")
			if err := os.WriteFile(filename, body, 0600); err != nil {
				t.Fatal(err)
			}
			selected := filename
			switch kind {
			case "symlink":
				selected = filepath.Join(root, "manifest-link.json")
				if err := os.Symlink(filename, selected); err != nil {
					t.Fatal(err)
				}
			case "ancestor-symlink":
				alias := filepath.Join(t.TempDir(), "directory-link")
				if err := os.Symlink(root, alias); err != nil {
					t.Fatal(err)
				}
				selected = filepath.Join(alias, "manifest.json")
			case "hardlink":
				if err := os.Link(filename, filepath.Join(t.TempDir(), "outside-manifest")); err != nil {
					t.Fatal(err)
				}
			case "directory":
				selected = root
			case "hidden-traversal":
				selected = root + "/not-traversed/../manifest.json"
			}
			if got, err := ReadManifestContext(context.Background(), selected); err == nil || !reflect.DeepEqual(got, Manifest{}) {
				t.Fatal("manifest alias/special file/noncanonical path accepted", err)
			}
		})
	}
}

func FuzzManifestValidation(f *testing.F) {
	seed, err := json.Marshal(manifestDeclaration())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(`{"apiVersion":"virmill/v1","members":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var m Manifest
		if err := wire.Decode(data, &m); err != nil {
			return
		}
		_ = m.Validate()
	})
}

func TestManifestRejectsHardlinkedMember(t *testing.T) {
	root := t.TempDir()
	m := Manifest{APIVersion: "virmill/v1", ID: "fixture-backup", VMID: "fixture-vm", Mode: "cold", Consistency: "cold-complete"}
	for _, kind := range []string{"disk", "persistent-xml"} {
		body := []byte("synthetic " + kind)
		hash := sha256.Sum256(body)
		if err := os.WriteFile(filepath.Join(root, kind), body, 0600); err != nil {
			t.Fatal(err)
		}
		m.Required = append(m.Required, kind)
		m.Members = append(m.Members, Member{ID: kind, Kind: kind, Path: kind, Size: int64(len(body)), SHA256: hex.EncodeToString(hash[:])})
	}
	if err := os.Link(filepath.Join(root, "disk"), filepath.Join(t.TempDir(), "outside-alias")); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(root); err == nil {
		t.Fatal("hardlinked backup member accepted")
	}
}
