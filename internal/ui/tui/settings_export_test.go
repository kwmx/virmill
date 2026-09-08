//go:build linux

package tui

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func TestExportImportSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "appliance.json")
	input := map[string]any{
		"destination": "/var/lib/virmill/images/example",
		"systemID":    "guest-system",
		"disks": []any{
			map[string]any{"id": "boot", "format": "qcow2", "maximumVirtualBytes": float64(1073741824)},
			map[string]any{"id": "data", "format": "raw", "maximumVirtualBytes": float64(2147483648)},
		},
	}
	if err := exportImportSettings(path, input); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, input) {
		t.Fatalf("settings object changed or wrapped: %#v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("settings permissions = %o, want 0600", info.Mode().Perm())
	}
	assertNoSettingsExportTemporaryFiles(t, dir)
}

func TestExportImportSettingsPreservesExistingTargets(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "dangling-symlink", "fifo", "directory", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			original := filepath.Join(dir, "original")
			if err := os.WriteFile(original, []byte("keep this"), 0640); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "file":
				err = os.WriteFile(path, []byte("keep this"), 0640)
			case "symlink":
				err = os.Symlink(original, path)
			case "dangling-symlink":
				err = os.Symlink(filepath.Join(dir, "missing"), path)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "hardlink":
				err = os.Link(original, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := exportImportSettings(path, map[string]any{"systemID": "new"}); err == nil || !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("existing %s was not clearly refused: %v", kind, err)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatalf("existing %s changed: %v", kind, err)
			}
			data, err := os.ReadFile(original)
			if err != nil || string(data) != "keep this" {
				t.Fatalf("original changed: %q, %v", data, err)
			}
			if kind == "file" {
				data, err = os.ReadFile(path)
				if err != nil || string(data) != "keep this" {
					t.Fatalf("existing file changed: %q, %v", data, err)
				}
			}
			assertNoSettingsExportTemporaryFiles(t, dir)
		})
	}
}

func TestExportImportSettingsRejectsUnsafeParents(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(filepath.Join(real, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(link, "settings.json"),
		filepath.Join(link, "child", "settings.json"),
		filepath.Join(fifo, "settings.json"),
		filepath.Join(dir, "missing", "settings.json"),
		"settings.json", "~/settings.json", "/", dir + "/./settings.json", dir + "/child/../settings.json", dir + "/settings.json/", dir + "/bad\x00name",
	} {
		if err := exportImportSettings(path, map[string]any{"systemID": "new"}); err == nil {
			t.Errorf("unsafe path accepted: %q", path)
		}
	}
	if _, err := os.Stat(filepath.Join(real, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("wrote through symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(real, "child", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("wrote through ancestor symlink: %v", err)
	}
	assertNoSettingsExportTemporaryFiles(t, dir)
	assertNoSettingsExportTemporaryFiles(t, real)
}

func TestExportImportSettingsEncodingFailuresLeaveNoFile(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, tc := range []struct {
		name  string
		input map[string]any
	}{
		{"nil", nil},
		{"too-large", map[string]any{"systemID": strings.Repeat("a", importSettingsExportLimit)}},
		{"cycle", cycle},
		{"nonfinite", map[string]any{"size": math.Inf(1)}},
		{"unsupported", map[string]any{"callback": func() {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			if err := exportImportSettings(path, tc.input); err == nil {
				t.Fatal("invalid settings accepted")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed export left files: %v, %v", entries, err)
			}
		})
	}
}

func TestExportImportSettingsConcurrentSaveHasOneWinner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	const count = 8
	var workers sync.WaitGroup
	results := make(chan error, count)
	for i := range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- exportImportSettings(path, map[string]any{"writer": i})
		}()
	}
	workers.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("unexpected concurrent export error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("successful exports = %d; want exactly one", winners)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct{ Writer int }
	if err := json.Unmarshal(data, &saved); err != nil || saved.Writer < 0 || saved.Writer >= count {
		t.Fatalf("saved result is incomplete: %q, %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("export left extra files: %v, %v", entries, err)
	}
}

func assertNoSettingsExportTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".virmill-settings-") {
			t.Fatalf("temporary export file left behind: %s", entry.Name())
		}
	}
}
