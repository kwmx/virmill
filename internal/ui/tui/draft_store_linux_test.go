//go:build linux

package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

type testDraftChoices struct {
	Source   string            `json:"source"`
	CPU      string            `json:"cpu"`
	Advanced testDraftAdvanced `json:"advanced"`
}
type testDraftAdvanced struct {
	Firmware string   `json:"firmware"`
	NICs     []string `json:"nics"`
	Agent    bool     `json:"agent"`
}

func newTestDraftStore(t *testing.T) (*DraftStore, string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("ordinary-user-only frontend")
	}
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", base)
	s, err := NewDraftStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, filepath.Join(base, "virmill", "tui-drafts")
}

func TestDraftStoreRoundTripRestartAndDelete(t *testing.T) {
	s, dir := newTestDraftStore(t)
	choices := testDraftChoices{Source: "/home/user/appliance.ova", CPU: "not finished", Advanced: testDraftAdvanced{Firmware: "uefi-secure", NICs: []string{"lab", "internet"}, Agent: true}}
	var got testDraftChoices
	if _, err := s.Load("import", &got); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	generation, err := s.Save("import", "", choices)
	if err != nil {
		t.Fatal(err)
	}
	if !validDraftGeneration(generation) {
		t.Fatal("invalid generation")
	}
	for _, path := range []string{filepath.Dir(dir), dir} {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0700 {
			t.Fatalf("private directory: %v %v", st, err)
		}
	}
	st, err := os.Stat(filepath.Join(dir, "import.json"))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("private file: %v %v", st, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewDraftStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	loaded, err := s.Load("import", &got)
	if err != nil || loaded != generation || got.Source != choices.Source || got.CPU != choices.CPU || got.Advanced.Firmware != "uefi-secure" || !got.Advanced.Agent || len(got.Advanced.NICs) != 2 {
		t.Fatalf("lost draft choices: %#v, %q, %v", got, loaded, err)
	}
	got.CPU = "4"
	updated, err := s.Save("import", loaded, got)
	if err != nil || updated == loaded {
		t.Fatalf("update: %q %v", updated, err)
	}
	if err := s.Delete("import", updated); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("import", &got); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestDraftStoreGenerationConflicts(t *testing.T) {
	s, dir := newTestDraftStore(t)
	choices := testDraftChoices{Source: "first"}
	g, err := s.Save("creation", "", choices)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "creation.json"))
	if _, err := s.Save("creation", "", choices); !errors.Is(err, ErrDraftConflict) {
		t.Fatal(err)
	}
	if err := s.Delete("creation", strings.Repeat("1", 32)); !errors.Is(err, ErrDraftConflict) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "creation.json"))
	if string(before) != string(after) {
		t.Fatal("conflict modified saved file")
	}
	if err := s.Delete("creation", g); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("creation", g, choices); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("disappearance was treated as create", err)
	}
	if err := s.Delete("creation", g); !errors.Is(err, ErrDraftConflict) {
		t.Fatal(err)
	}
	newG, err := s.Save("creation", "", choices)
	if err != nil || newG == g {
		t.Fatal("recreation reused old generation", err)
	}
	if _, err := s.Save("creation", g, choices); !errors.Is(err, ErrDraftConflict) {
		t.Fatal(err)
	}
}

func TestDraftStoreConcurrentWindowsHaveOneWinner(t *testing.T) {
	a, _ := newTestDraftStore(t)
	b, err := NewDraftStore()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	g, err := a.Save("import", "", testDraftChoices{Source: "first"})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, s := range []*DraftStore{a, b} {
		wg.Add(1)
		go func(s *DraftStore) {
			defer wg.Done()
			<-start
			_, err := s.Save("import", g, testDraftChoices{Source: "changed"})
			results <- err
		}(s)
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
			continue
		}
		if !errors.Is(err, ErrDraftBusy) && !errors.Is(err, ErrDraftConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("got %d winners", winners)
	}
}

func TestDraftStoreRejectsUntrustedJSONWithoutChangingDestination(t *testing.T) {
	s, dir := newTestDraftStore(t)
	generation := strings.Repeat("1", 32)
	envelope := `{"apiVersion":"virmill/v1","kind":"TUIDraft","generation":"` + generation + `","document":`
	tests := map[string]string{
		"unknown envelope":   envelope + `{},"extra":true}`,
		"unknown document":   envelope + `{"secret":"never accepted"}}`,
		"duplicate envelope": envelope + `{},"kind":"TUIDraft"}`,
		"duplicate document": envelope + `{"source":"a","source":"b"}}`,
		"wrong version":      strings.Replace(envelope+`{}}`, "virmill/v1", "virmill/v99", 1),
		"null document":      envelope + `null}`,
		"wrong type":         envelope + `{"source":"replaced","advanced":{"agent":"bad"}}}`,
		"trailing value":     envelope + `{}} {}`,
		"empty":              "",
		"oversize":           strings.Repeat("x", draftDocumentLimit+1),
		"missing metadata":   `{"document":{}}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "import.json"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			dst := testDraftChoices{Source: "unchanged"}
			if _, err := s.Load("import", &dst); err == nil {
				t.Fatal("accepted invalid saved draft")
			}
			if dst.Source != "unchanged" {
				t.Fatal("partial decode changed destination")
			}
			if _, err := s.Save("import", generation, testDraftChoices{}); err == nil {
				t.Fatal("overwrote invalid envelope")
			}
		})
	}
}

func TestDraftStoreRefusesUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "public", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			s, dir := newTestDraftStore(t)
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("source must remain"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "import.json")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(outside, path)
			case "hardlink":
				err = os.Link(outside, path)
			case "public":
				err = os.WriteFile(path, []byte("bad"), 0644)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			var dst testDraftChoices
			if _, err := s.Load("import", &dst); err == nil {
				t.Fatal("unsafe file accepted")
			}
			if _, err := s.Save("import", "", testDraftChoices{}); err == nil {
				t.Fatal("unsafe file overwritten")
			}
			data, _ := os.ReadFile(outside)
			if string(data) != "source must remain" {
				t.Fatal("outside source changed")
			}
		})
	}
}

func TestDraftStoreRefusesUnsafeDirectoriesAndLock(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ordinary-user-only frontend")
	}
	for _, kind := range []string{"relative", "dotdot", "parent symlink", "private symlink", "public private folder", "symlink lock", "hardlink lock"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("XDG_STATE_HOME", base)
			switch kind {
			case "relative":
				t.Setenv("XDG_STATE_HOME", "relative")
			case "dotdot":
				t.Setenv("XDG_STATE_HOME", base+"/../bad")
			case "parent symlink":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(base, link); err != nil {
					t.Fatal(err)
				}
				t.Setenv("XDG_STATE_HOME", link)
			case "private symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(base, "virmill")); err != nil {
					t.Fatal(err)
				}
			case "public private folder":
				if err := os.Mkdir(filepath.Join(base, "virmill"), 0755); err != nil {
					t.Fatal(err)
				}
			default:
				dir := filepath.Join(base, "virmill", "tui-drafts")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink lock" {
					if err := os.Symlink(outside, filepath.Join(dir, ".lock")); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Link(outside, filepath.Join(dir, ".lock")); err != nil {
						t.Fatal(err)
					}
				}
			}
			s, err := NewDraftStore()
			if err == nil {
				s.Close()
				t.Fatal("unsafe state directory accepted")
			}
		})
	}
}

func TestDraftStoreRejectsDynamicDTOsAndOversizeWithoutPublication(t *testing.T) {
	s, dir := newTestDraftStore(t)
	var nilChoices *testDraftChoices
	for _, doc := range []any{nil, nilChoices, map[string]string{"secret": "x"}, struct{ Dynamic any }{}, struct{ M map[string]string }{}, struct{ R json.RawMessage }{json.RawMessage(`{"extra":true}`)}, testDraftChoices{Source: strings.Repeat("x", draftDocumentLimit)}} {
		if _, err := s.Save("import", "", doc); err == nil {
			t.Fatalf("accepted unsupported DTO %T", doc)
		}
	}
	for _, name := range []string{"../escape", "/import", "other", "import.json"} {
		if _, err := s.Save(name, "", testDraftChoices{}); err == nil {
			t.Fatal("accepted unsafe name")
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".lock" {
		t.Fatalf("failed save left files: %v %v", entries, err)
	}
}

func TestDraftStoreLockContentionAndClosedStore(t *testing.T) {
	s, _ := newTestDraftStore(t)
	b, err := NewDraftStore()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := unix.Flock(s.lock, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Save("import", "", testDraftChoices{}); !errors.Is(err, ErrDraftBusy) {
		t.Fatal(err)
	}
	if err := unix.Flock(s.lock, unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("import", "", testDraftChoices{}); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
}

func TestDraftStoreRefusesReplacedLockAndLeavesDraftUntouched(t *testing.T) {
	s, dir := newTestDraftStore(t)
	g, err := s.Save("import", "", testDraftChoices{Source: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, ".lock"), filepath.Join(dir, ".original-lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("import", g); !errors.Is(err, ErrDraftConflict) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "import.json")); err != nil {
		t.Fatal("draft deleted through replaced lock", err)
	}
}

func TestDraftStoreDefaultXDGPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ordinary-user-only frontend")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	s, err := NewDraftStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Save("creation", "", testDraftChoices{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "state", "virmill", "tui-drafts", "creation.json")); err != nil {
		t.Fatal(err)
	}
}
