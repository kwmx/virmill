package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func pickerRun(t *testing.T, p FilePicker, cmd tea.Cmd) (FilePicker, PickerResult) {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing directory command")
	}
	p, _, result := p.Update(cmd())
	return p, result
}
func pickerKey(p FilePicker, key tea.KeyType) (FilePicker, tea.Cmd, PickerResult) {
	return p.Update(tea.KeyMsg{Type: key})
}
func pickerText(p FilePicker, text string) (FilePicker, tea.Cmd, PickerResult) {
	return p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}
func pickerFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("source remains unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestFilePickerBrowseSelectAndCancel(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "images")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "disk.qcow2")
	pickerFile(t, file)
	p, cmd := NewFilePicker(root, "file")
	p, _ = pickerRun(t, p, cmd)
	if p.directory != root || len(p.entries) != 1 {
		t.Fatalf("initial listing: %+v", p)
	}
	p, cmd, _ = pickerKey(p, tea.KeyEnter)
	p, _ = pickerRun(t, p, cmd)
	if p.directory != folder {
		t.Fatal("folder did not open")
	}
	p, cmd, _ = pickerKey(p, tea.KeyBackspace)
	p, _ = pickerRun(t, p, cmd)
	if p.directory != root {
		t.Fatal("parent did not open")
	}
	p, cmd, _ = pickerKey(p, tea.KeyEnter)
	p, _ = pickerRun(t, p, cmd)
	p, cmd, _ = pickerKey(p, tea.KeyEnter)
	p, result := pickerRun(t, p, cmd)
	if result.Path != file || result.Cancel {
		t.Fatalf("selection: %+v", result)
	}
	if body, _ := os.ReadFile(file); string(body) != "source remains unchanged" {
		t.Fatal("source changed")
	}
	p, cmd = NewFilePicker(root, "file")
	p, _ = pickerRun(t, p, cmd)
	p, _, result = pickerKey(p, tea.KeyEsc)
	if !result.Cancel {
		t.Fatal("cancel missing")
	}
	_, _, result = p.Update(cmd())
	if result.Path != "" {
		t.Fatal("closed picker accepted reply")
	}
}

func TestFilePickerFilteringHiddenAndDimensions(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha.ova", "beta.iso", ".hidden", "bad\x1bname"} {
		pickerFile(t, filepath.Join(root, name))
	}
	p, cmd := NewFilePicker(root, "file")
	p, _ = pickerRun(t, p, cmd)
	if len(p.entries) != 3 || len(p.visible()) != 2 {
		t.Fatalf("unsafe or hidden entry exposed: %+v", p.entries)
	}
	p, _, _ = pickerText(p, "/")
	p, _, _ = pickerText(p, "beta")
	if len(p.visible()) != 1 {
		t.Fatal("live filter failed")
	}
	p, _, _ = pickerKey(p, tea.KeyEnter)
	if p.mode != "" || p.filter != "beta" {
		t.Fatal("filter focus did not close")
	}
	p, _, r := pickerKey(p, tea.KeyEsc)
	if r.Cancel || p.filter != "" {
		t.Fatal("filter escape should preserve picker")
	}
	p, _, _ = pickerText(p, ".")
	if len(p.visible()) != 3 {
		t.Fatal("hidden toggle failed")
	}
	for _, size := range [][2]int{{80, 24}, {120, 36}, {1, 1}, {20, 4}, {0, 4}, {4, 0}} {
		view := p.View(size[0], size[1])
		if size[0] == 0 || size[1] == 0 {
			if view != "" {
				t.Fatal("zero dimension rendered")
			}
			continue
		}
		lines := strings.Split(view, "\n")
		if len(lines) > size[1] {
			t.Fatalf("height overflow: %v", size)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] || strings.ContainsRune(line, '\x1b') {
				t.Fatalf("unsafe or wide line %q at%v", line, size)
			}
		}
	}
}

func TestFilePickerRefusesSymlinksSpecialFilesAndChangedSelection(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "disk")
	pickerFile(t, file)
	link := filepath.Join(root, "linked")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "folder")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(folder, filepath.Join(root, "folder-link")); err != nil {
		t.Fatal(err)
	}
	p, cmd := NewFilePicker(root, "file")
	p, _ = pickerRun(t, p, cmd)
	if len(p.entries) != 2 {
		t.Fatalf("symlink listed: %+v", p.entries)
	}
	for _, path := range []string{link, filepath.Join(root, "folder-link"), "/dev/null", filepath.Join(root, "missing")} {
		q, command := p.load(path, true)
		q, r := pickerRun(t, q, command)
		if r.Path != "" || q.message == "" || q.directory != root {
			t.Fatalf("unsafe path accepted or navigation lost: %q", path)
		}
	}
	// Swap an observed ordinary file for a symlink before confirmation.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(folder, file); err != nil {
		t.Fatal(err)
	}
	p.selected = 1
	p, cmd, _ = pickerKey(p, tea.KeyEnter)
	p, r := pickerRun(t, p, cmd)
	if r.Path != "" || p.message == "" {
		t.Fatal("changed selection was accepted")
	}
}

func TestFilePickerStaleRepliesAndDirectorySelection(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "child")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	pickerFile(t, filepath.Join(root, "file"))
	first, old := NewFilePicker(root, "directory")
	second, latest := NewFilePicker(folder, "directory")
	second, _, _ = second.Update(old())
	if second.directory != "" || !second.loading {
		t.Fatal("foreign picker reply accepted")
	}
	second, _ = pickerRun(t, second, latest)
	first, _ = pickerRun(t, first, old)
	first, newer := first.load(folder, false)
	first, _, _ = first.Update(old())
	if !first.loading || first.directory != root {
		t.Fatal("stale request accepted")
	}
	first, _ = pickerRun(t, first, newer)
	first, cmd, _ := pickerKey(first, tea.KeyCtrlS)
	_, result := pickerRun(t, first, cmd)
	if result.Path != folder {
		t.Fatalf("directory selection: %+v", result)
	}
	if second.directory != folder {
		t.Fatal("independent picker changed")
	}
}

func TestFilePickerTypedPathAndDefaultHome(t *testing.T) {
	root := t.TempDir()
	images := filepath.Join(root, "images")
	if err := os.Mkdir(images, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(images, "sample.ova")
	pickerFile(t, file)
	t.Setenv("HOME", root)
	p, cmd := NewFilePicker("", "file")
	p, _ = pickerRun(t, p, cmd)
	if p.directory != images {
		t.Fatal("did not prefer home images")
	}
	p, cmd, _ = pickerKey(p, tea.KeyCtrlH)
	p, _ = pickerRun(t, p, cmd)
	if p.directory != root {
		t.Fatal("home shortcut failed")
	}
	p, _, _ = pickerKey(p, tea.KeyCtrlL)
	p, _, _ = pickerKey(p, tea.KeyCtrlU)
	p, _, _ = pickerText(p, "relative")
	p, cmd, _ = pickerKey(p, tea.KeyEnter)
	if cmd != nil || p.message == "" || p.mode != "path" {
		t.Fatal("relative path did not stay editable")
	}
	p, _, _ = pickerKey(p, tea.KeyCtrlU)
	p, _, _ = pickerText(p, "~/images/sample.ova")
	p, cmd, _ = pickerKey(p, tea.KeyEnter)
	_, result := pickerRun(t, p, cmd)
	if result.Path != file {
		t.Fatalf("typed file failed: %+v", result)
	}
}

func TestFilePickerBoundedListing(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < pickerEntryLimit+1; i++ {
		file, err := os.CreateTemp(root, "sample-")
		if err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	p, cmd := NewFilePicker(root, "file")
	p, _ = pickerRun(t, p, cmd)
	if !p.limited || len(p.entries) != pickerEntryLimit || !strings.Contains(p.View(80, 24), "4096") {
		t.Fatal("directory bound or notice missing")
	}
}

func TestFilePickerPermissionErrorStaysOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions require an unprivileged test process")
	}
	root := t.TempDir()
	denied := filepath.Join(root, "private")
	if err := os.Mkdir(denied, 0700); err != nil {
		t.Fatal(err)
	}
	p, cmd := NewFilePicker(root, "file")
	p, _ = pickerRun(t, p, cmd)
	if err := os.Chmod(denied, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(denied, 0700)
	p, cmd, _ = pickerKey(p, tea.KeyEnter)
	p, result := pickerRun(t, p, cmd)
	if result.Path != "" || result.Cancel || p.directory != root || p.message == "" || p.closed {
		t.Fatal("permission failure escaped picker")
	}
	p, _, result = pickerKey(p, tea.KeyEsc)
	if !result.Cancel {
		t.Fatal("cannot cancel after permission failure")
	}
}
