//go:build linux && amd64

package restic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/domain"
)

const testOperation = "12345678-1234-1234-1234-123456789abc"
const testTag = "virmill-operation:" + testOperation
const testSnapshot = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testPassword = "fixture-only-password\n"

func fixture(t *testing.T) (Repository, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	repo := Repository{Path: filepath.Join(root, "repository"), PasswordFile: filepath.Join(root, "password")}
	if err := os.Mkdir(repo.Path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repo.PasswordFile, []byte(testPassword), 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source set")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "member"), []byte("generated captured member"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(source, 0700) })
	return repo, source
}
func record(path, id string) Snapshot {
	return Snapshot{ID: id, Tags: []string{testTag}, Paths: []string{path}, Time: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
}
func encoded(v any) []byte { b, _ := json.Marshal(v); return b }
func assertError(t *testing.T, err error, code string) {
	t.Helper()
	var d *domain.Error
	if !errors.As(err, &d) || d.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
	if strings.Contains(err.Error(), testPassword) || strings.Contains(err.Error(), "opaque-secret") {
		t.Fatal("diagnostic leaked")
	}
}

func TestResticFixedCommandsAndSnapshotBinding(t *testing.T) {
	r, source := fixture(t)
	snap := record(source, testSnapshot)
	destination := filepath.Join(filepath.Dir(r.Path), "new restored set")
	calls := []string{}
	backed := false
	tool := Tool{command: func(ctx context.Context, in invocation) ([]byte, error) {
		calls = append(calls, in.label)
		prefix := []string{"--json", "--quiet", "--no-cache", "--repo", "/proc/self/fd/4", "--password-file", "/proc/self/fd/5"}
		if len(in.args) < len(prefix) || !reflect.DeepEqual(in.args[:len(prefix)], prefix) || len(in.files) < 2 {
			t.Fatal("unbound fixed command", in.args)
		}
		if strings.Contains(strings.Join(in.args, " "), r.PasswordFile) || strings.Contains(strings.Join(in.args, " "), testPassword) {
			t.Fatal("password reached argv")
		}
		b := make([]byte, len(testPassword))
		if _, err := in.files[1].ReadAt(b, 0); err != nil || string(b) != testPassword {
			t.Fatal("password was not inherited through held file", err)
		}
		args := in.args[len(prefix):]
		switch in.label {
		case "snapshots":
			if reflect.DeepEqual(args, []string{"snapshots", "--", testSnapshot}) {
				return encoded([]Snapshot{snap}), nil
			}
			if !reflect.DeepEqual(args, []string{"snapshots", "--tag", testTag}) {
				t.Fatal("wrong operation filter", args)
			}
			if backed {
				return encoded([]Snapshot{snap}), nil
			}
			return []byte("[]"), nil
		case "backup":
			if !reflect.DeepEqual(args, []string{"backup", "--force", "--tag", testTag, "--", "."}) || len(in.files) != 3 || in.dir != fmt.Sprintf("/proc/self/fd/%d", in.files[2].Fd()) {
				t.Fatal("source is not pinned relative backup", args, in.dir)
			}
			backed = true
			return []byte(`{"message_type":"summary","snapshot_id":"` + testSnapshot + `"}`), nil
		case "restore":
			if !reflect.DeepEqual(args, []string{"restore", "--target", destination, "--overwrite", "never", "--verify", "--", testSnapshot}) || len(in.files) != 3 {
				t.Fatal("restore weakened no-overwrite", args)
			}
			if err := os.WriteFile(fmt.Sprintf("/proc/self/fd/%d/member", in.files[2].Fd()), []byte("generated captured member"), 0400); err != nil {
				t.Fatal(err)
			}
			return []byte(`{"message_type":"summary","files_restored":1}`), nil
		case "check":
			if !reflect.DeepEqual(args, []string{"check", "--read-data"}) {
				t.Fatal("partial check", args)
			}
			return []byte(`{"message_type":"summary","num_errors":0}`), nil
		}
		t.Fatal("unexpected command", in.label)
		return nil, nil
	}}
	got, err := tool.Backup(context.Background(), r, source, testOperation)
	if err != nil || !reflect.DeepEqual(got, snap) {
		t.Fatal("backup mapping lost", got, err)
	}
	if _, err = tool.Backup(context.Background(), r, source, testOperation); err == nil {
		t.Fatal("same operation replayed")
	}
	if err = tool.Restore(context.Background(), r, testSnapshot, destination); err != nil {
		t.Fatal(err)
	}
	if err = tool.Check(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"snapshots", "backup", "snapshots", "snapshots", "snapshots", "restore", "check"}) {
		t.Fatal("unexpected commands", calls)
	}
	for _, path := range []string{filepath.Join(source, "member"), filepath.Join(destination, "member")} {
		b, err := os.ReadFile(path)
		if err != nil || string(b) != "generated captured member" {
			t.Fatal("source/restore bytes changed", path, err)
		}
	}
}

func TestResticInitAndRestoreNeverOverwriteAndRetainFailure(t *testing.T) {
	for _, operation := range []string{"init", "restore"} {
		t.Run(operation, func(t *testing.T) {
			r, source := fixture(t)
			destination := filepath.Join(filepath.Dir(r.Path), "new-target")
			calls := 0
			tool := Tool{command: func(ctx context.Context, in invocation) ([]byte, error) {
				calls++
				if in.label == "snapshots" {
					return encoded([]Snapshot{record(source, testSnapshot)}), nil
				}
				var root *os.File
				if operation == "init" {
					root = in.files[0]
					if !reflect.DeepEqual(in.args[7:], []string{"init", "--repository-version", "2"}) {
						t.Fatal(in.args)
					}
				} else {
					root = in.files[2]
				}
				if err := os.WriteFile(fmt.Sprintf("/proc/self/fd/%d/retained-partial", root.Fd()), []byte("partial"), 0600); err != nil {
					t.Fatal(err)
				}
				return nil, domain.Fail("OPERATION_FAILED", "injected partial command failure")
			}}
			run := func() error {
				if operation == "init" {
					copy := r
					copy.Path = destination
					return tool.Init(context.Background(), copy)
				}
				return tool.Restore(context.Background(), r, testSnapshot, destination)
			}
			if err := run(); err == nil {
				t.Fatal("injected failure reported success")
			}
			b, err := os.ReadFile(filepath.Join(destination, "retained-partial"))
			if err != nil || string(b) != "partial" {
				t.Fatal("failure cleaned partial", err)
			}
			before := calls
			if err = run(); err == nil {
				t.Fatal("existing destination reused")
			}
			expected := before
			if operation == "restore" {
				expected++
			}
			if calls != expected {
				t.Fatal("failed work replayed", calls, before)
			}
		})
	}
}

func TestResticPathCredentialAndTreeRefusals(t *testing.T) {
	for _, fault := range []string{"relative-repo", "url-repo", "dot-root", "repo-symlink", "ancestor-symlink", "credential-symlink", "credential-hardlink", "credential-world-readable", "credential-empty", "credential-oversized", "credential-fifo", "source-symlink", "source-fifo", "source-hardlink", "source-contains-key", "overlap", "destination-existing", "invalid-operation", "short-snapshot"} {
		t.Run(fault, func(t *testing.T) {
			r, source := fixture(t)
			base := filepath.Dir(r.Path)
			calls := 0
			tool := Tool{command: func(context.Context, invocation) ([]byte, error) {
				calls++
				return nil, errors.New("unexpected process")
			}}
			switch fault {
			case "relative-repo":
				r.Path = "relative"
			case "url-repo":
				r.Path = "sftp:user@host:/repository"
			case "dot-root":
				r.Path = base + "/./repository"
			case "repo-symlink":
				link := filepath.Join(base, "alias")
				if err := os.Symlink(r.Path, link); err != nil {
					t.Fatal(err)
				}
				r.Path = link
			case "ancestor-symlink":
				link := filepath.Join(base, "alias")
				if err := os.Symlink(base, link); err != nil {
					t.Fatal(err)
				}
				r.PasswordFile = filepath.Join(link, "password")
			case "credential-symlink":
				link := filepath.Join(base, "alias")
				_ = os.Symlink(r.PasswordFile, link)
				r.PasswordFile = link
			case "credential-hardlink":
				if err := os.Link(r.PasswordFile, filepath.Join(base, "linked")); err != nil {
					t.Fatal(err)
				}
			case "credential-world-readable":
				_ = os.Chmod(r.PasswordFile, 0644)
			case "credential-empty":
				_ = os.WriteFile(r.PasswordFile, nil, 0600)
			case "credential-oversized":
				_ = os.WriteFile(r.PasswordFile, make([]byte, maxPassword+1), 0600)
			case "credential-fifo":
				r.PasswordFile = filepath.Join(base, "fifo")
				if err := unix.Mkfifo(r.PasswordFile, 0600); err != nil {
					t.Fatal(err)
				}
			case "source-symlink", "source-fifo", "source-hardlink":
				_ = os.Chmod(source, 0700)
				path := filepath.Join(source, "unsafe")
				var err error
				if fault == "source-symlink" {
					err = os.Symlink(r.PasswordFile, path)
				} else if fault == "source-fifo" {
					err = unix.Mkfifo(path, 0600)
				} else {
					err = os.Link(filepath.Join(source, "member"), path)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "source-contains-key":
				source = base
			case "overlap":
				source = r.Path
			}
			var err error
			switch fault {
			case "destination-existing":
				err = tool.Init(context.Background(), r)
			case "invalid-operation":
				_, err = tool.Observe(context.Background(), r, "../operation")
			case "short-snapshot":
				err = tool.Restore(context.Background(), r, "latest", filepath.Join(base, "restore"))
			default:
				_, err = tool.Backup(context.Background(), r, source, testOperation)
			}
			if err == nil || calls != 0 {
				t.Fatal("invalid path/type/identity reached command", calls, err)
			}
		})
	}
}

func TestResticSnapshotParserRejectsConfusionAndKeepsAmbiguity(t *testing.T) {
	good := record("/private/source", testSnapshot)
	raw := string(encoded([]Snapshot{good}))
	for _, bad := range []string{"null", `{}`, raw + `[]`, strings.Replace(raw, `"id":`, `"id":"`+testSnapshot+`","id":`, 1), strings.Replace(raw, `"id":`, `"ID":`, 1), strings.Replace(raw, testTag, testTag+"other", 1), strings.Replace(raw, testSnapshot, "abcd", 1), strings.Replace(raw, `["/private/source"]`, `["/private/source","/other"]`, 1), strings.Replace(raw, `"/private/source"`, `"/private/../source"`, 1), strings.Replace(raw, `"2026-09-08T12:00:00Z"`, `null`, 1), strings.Repeat(" ", maxOutput+1)} {
		if got, err := snapshots([]byte(bad), testTag); err == nil || got != nil {
			t.Fatal("unbound/malformed snapshots accepted")
		}
	}
	a := good
	b := good
	b.ID = strings.Repeat("b", 64)
	got, err := snapshots(encoded([]Snapshot{b, a}), testTag)
	if err != nil || len(got) != 2 || got[0].ID != a.ID {
		t.Fatal("ambiguity was hidden", got, err)
	}
	if got, err = snapshots(encoded([]Snapshot{a, a}), testTag); err == nil || got != nil {
		t.Fatal("duplicate snapshot identity accepted")
	}
}

func TestResticBackupUncertainEffectsNeverBecomeSuccess(t *testing.T) {
	for _, fault := range []string{"partial-exit", "missing-summary", "duplicate-summary", "dry-run", "wrong-summary-id", "wrong-path", "ambiguous", "source-change", "password-replacement", "repo-replacement", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			r, source := fixture(t)
			observes := 0
			writes := 0
			tool := Tool{command: func(ctx context.Context, in invocation) ([]byte, error) {
				if in.label == "snapshots" {
					observes++
					if observes == 1 {
						return []byte("[]"), nil
					}
					s := record(source, testSnapshot)
					if fault == "wrong-path" {
						s.Paths = []string{"/other/source"}
					}
					if fault == "ambiguous" {
						b := s
						b.ID = strings.Repeat("b", 64)
						return encoded([]Snapshot{s, b}), nil
					}
					return encoded([]Snapshot{s}), nil
				}
				writes++
				line := `{"message_type":"summary","snapshot_id":"` + testSnapshot + `"}`
				switch fault {
				case "partial-exit":
					return []byte(line), domain.Fail("INCOMPLETE_BACKUP", "partial source read")
				case "missing-summary":
					return []byte(`{"message_type":"status"}`), nil
				case "duplicate-summary":
					return []byte(line + "\n" + line), nil
				case "dry-run":
					return []byte(strings.Replace(line, `"snapshot_id"`, `"dry_run":true,"snapshot_id"`, 1)), nil
				case "wrong-summary-id":
					line = strings.ReplaceAll(line, testSnapshot, strings.Repeat("b", 64))
				case "source-change":
					path := filepath.Join(source, "member")
					_ = os.Chmod(path, 0600)
					_ = os.WriteFile(path, []byte("changed"), 0600)
				case "password-replacement":
					_ = os.Rename(r.PasswordFile, r.PasswordFile+".old")
					_ = os.WriteFile(r.PasswordFile, []byte(testPassword), 0600)
				case "repo-replacement":
					_ = os.Rename(r.Path, r.Path+".old")
					_ = os.Mkdir(r.Path, 0700)
				case "canceled":
					return nil, context.Canceled
				}
				return []byte(line), nil
			}}
			got, err := tool.Backup(context.Background(), r, source, testOperation)
			if err == nil || !reflect.DeepEqual(got, Snapshot{}) || writes != 1 {
				t.Fatal("uncertain backup became success or replayed", got, err, writes)
			}
		})
	}
}

func TestResticCheckAndRestoreRefuseStructuredFailure(t *testing.T) {
	for _, good := range []string{`{"message_type":"summary","num_errors":0,"broken_packs":null,"suggest_repair_index":false,"suggest_prune":false}`, `{"message_type":"summary","num_errors":0,"broken_packs":[]}`} {
		r, _ := fixture(t)
		tool := Tool{command: func(context.Context, invocation) ([]byte, error) { return []byte(good), nil }}
		if err := tool.Check(context.Background(), r); err != nil {
			t.Fatal("healthy native summary refused", err)
		}
	}
	for _, bad := range []string{`{"message_type":"summary"}`, `{"message_type":"summary","num_errors":null}`, `{"message_type":"summary","num_errors":0,"broken_packs":null,"Broken_Packs":[]}`, `{"message_type":"summary","num_errors":1}`, `{"message_type":"summary","num_errors":0,"broken_packs":["bad"]}`, `{"message_type":"summary","num_errors":0,"suggest_repair_index":true}`, `{"message_type":"error","message":"opaque-secret"}`, `{"message_type":"summary","num_errors":-1}`} {
		r, _ := fixture(t)
		tool := Tool{command: func(context.Context, invocation) ([]byte, error) { return []byte(bad), nil }}
		if err := tool.Check(context.Background(), r); err == nil || strings.Contains(err.Error(), "opaque-secret") {
			t.Fatal("corruption accepted or diagnostics leaked", err)
		}
	}
	r, source := fixture(t)
	tool := Tool{command: func(_ context.Context, in invocation) ([]byte, error) {
		if in.label == "snapshots" {
			return encoded([]Snapshot{record(source, testSnapshot)}), nil
		}
		return []byte(`{"message_type":"summary","files_skipped":1}`), nil
	}}
	dest := filepath.Join(filepath.Dir(r.Path), "restore")
	if err := tool.Restore(context.Background(), r, testSnapshot, dest); err == nil {
		t.Fatal("skipped restoration accepted")
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal("partial restore removed")
	}
}
