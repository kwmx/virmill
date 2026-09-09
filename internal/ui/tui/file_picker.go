package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/validation"
)

const pickerEntryLimit = 4096

var pickerSequence atomic.Uint64

// PickerResult returns a chosen path or cancellation; neither authorizes an
// operation. The service must revalidate the path when it observes the source.
type PickerResult struct {
	Path   string
	Cancel bool
}
type pickerEntry struct {
	name      string
	directory bool
}
type pickerRead struct {
	token         uint64
	path          string
	entries       []pickerEntry
	selected      bool
	limited       bool
	err           error
	directoryInfo os.FileInfo
	createdName   string
}

// FilePicker observes names and file types only. It never opens file contents.
// Creating a child folder requires a separate named, explicit confirmation.
type FilePicker struct {
	kind, directory, home, filter, input, mode, message string
	entries                                             []pickerEntry
	selected                                            int
	hidden, loading, limited, closed                    bool
	token                                               uint64
	directoryInfo                                       os.FileInfo
	creating                                            bool
}

// NewFilePicker starts an asynchronous observation at an explicit field path,
// or the user's home directory when blank. kind is "file" or "directory".
func NewFilePicker(start, kind string) (FilePicker, tea.Cmd) {
	home, _ := os.UserHomeDir()
	p := FilePicker{kind: kind, home: home}
	if kind != "file" && kind != "directory" && kind != "source" {
		p.kind = "file"
	}
	if start == "" {
		start = home
	}
	if start == "" {
		start = "/"
	}
	return p.load(start, false)
}

func (p FilePicker) load(path string, choose bool) (FilePicker, tea.Cmd) {
	p.token = pickerSequence.Add(1)
	token, kind := p.token, p.kind
	p.loading, p.message = true, ""
	return p, func() tea.Msg {
		r := pickerRead{token: token, path: path}
		info, err := pickerInfo(path)
		if err != nil {
			r.err = err
			return r
		}
		if !info.IsDir() {
			if choose && (kind == "file" || kind == "source") && info.Mode().IsRegular() {
				r.selected = true
				return r
			}
			if !choose && info.Mode().IsRegular() {
				path = filepath.Dir(path)
				r.path = path
				info, err = pickerInfo(path)
			} else {
				r.err = fmt.Errorf("Choose a folder or an ordinary file.")
				return r
			}
		}
		if err != nil {
			r.err = err
			return r
		}
		if choose && (kind == "directory" || kind == "source") {
			r.selected = true
			return r
		}
		// OpenRoot opens only directories. Check identity before listing so a
		// replaced final component cannot turn this into a special-file open.
		root, err := os.OpenRoot(path)
		if err != nil {
			r.err = fmt.Errorf("Cannot open this folder. Check its permissions.")
			return r
		}
		defer root.Close()
		observed, err := root.Stat(".")
		if err != nil || !os.SameFile(info, observed) {
			r.err = fmt.Errorf("This folder changed. Try again.")
			return r
		}
		current, err := pickerInfo(path)
		if err != nil || !os.SameFile(info, current) {
			r.err = fmt.Errorf("This folder changed. Try again.")
			return r
		}
		folder, err := root.Open(".")
		if err != nil {
			r.err = fmt.Errorf("Cannot read this folder. Check its permissions.")
			return r
		}
		defer folder.Close()
		entries, err := folder.ReadDir(pickerEntryLimit + 1)
		if err != nil && err != io.EOF {
			r.err = fmt.Errorf("Cannot read this folder. Check its permissions.")
			return r
		}
		r.limited = len(entries) > pickerEntryLimit
		if r.limited {
			entries = entries[:pickerEntryLimit]
		}
		for _, e := range entries {
			if !pickerName(e.Name()) || e.Type()&os.ModeSymlink != 0 {
				continue
			}
			file, err := e.Info()
			if err != nil || (!file.IsDir() && !file.Mode().IsRegular()) {
				continue
			}
			if kind == "directory" && !file.IsDir() {
				continue
			}
			r.entries = append(r.entries, pickerEntry{name: e.Name(), directory: file.IsDir()})
		}
		current, err = pickerInfo(path)
		if err != nil || !os.SameFile(info, current) {
			r.err = fmt.Errorf("This folder changed. Try again.")
			r.entries = nil
			return r
		}
		sort.Slice(r.entries, func(i, j int) bool {
			a, b := r.entries[i], r.entries[j]
			if a.directory != b.directory {
				return a.directory
			}
			return strings.ToLower(a.name) < strings.ToLower(b.name)
		})
		r.directoryInfo = info
		return r
	}
}

// pickerInfo rejects symlinks in every path component and special files. This
// is an observation, not a lock on a later service operation.
func pickerInfo(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !pickerName(path) {
		return nil, fmt.Errorf("Use an absolute path without control characters.")
	}
	var info os.FileInfo
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(path, current), string(filepath.Separator))
	if path == current {
		parts = []string{""}
	}
	for _, part := range parts {
		current = filepath.Join(current, part)
		var err error
		info, err = os.Lstat(current)
		if err != nil {
			return nil, fmt.Errorf("Cannot access this path. Check its name and permissions.")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("Symbolic links are not available here. Choose the original path.")
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("Choose an ordinary file or folder.")
		}
	}
	return info, nil
}
func pickerName(name string) bool {
	if len(name) == 0 || len(name) > 4096 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func pickerFolderName(name string) bool {
	return pickerName(name) && len(name) <= 255 && strings.TrimSpace(name) == name && name != "." && name != ".." && !strings.ContainsAny(name, "/\\")
}

func (p FilePicker) createFolder() (FilePicker, tea.Cmd) {
	name, directory, expected := p.input, p.directory, p.directoryInfo
	p, refresh := p.load(directory, false)
	p.creating = true
	token := p.token
	return p, func() tea.Msg {
		created, err := pickerMakeFolder(directory, name, expected)
		if err != nil {
			r := pickerRead{token: token, path: directory, err: err}
			if created {
				r.createdName = name
			}
			return r
		}
		r := refresh().(pickerRead)
		r.createdName = name
		if r.err != nil {
			r.err = fmt.Errorf("Folder created, but the view could not refresh: %w", r.err)
		}
		// The bounded listing may omit a newly added entry in a large folder.
		// Include the explicitly created child only after observing its type again.
		if r.err == nil && r.limited {
			found := false
			for _, entry := range r.entries {
				found = found || entry.name == name
			}
			if !found {
				info, err := pickerInfo(filepath.Join(directory, name))
				if err == nil && info.IsDir() {
					if len(r.entries) == pickerEntryLimit {
						r.entries = r.entries[:pickerEntryLimit-1]
					}
					r.entries = append(r.entries, pickerEntry{name: name, directory: true})
				}
			}
		}
		return r
	}
}
func (p FilePicker) visible() []pickerEntry {
	entries := make([]pickerEntry, 0, len(p.entries))
	for _, e := range p.entries {
		if (!p.hidden && strings.HasPrefix(e.name, ".")) || !strings.Contains(strings.ToLower(e.name), strings.ToLower(p.filter)) {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}
func (p FilePicker) expandPath(path string) (string, error) {
	if path == "~" {
		path = p.home
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(p.home, path[2:])
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("Use / for an absolute path, or ~/ for home.")
	}
	path = filepath.Clean(path)
	if !pickerName(path) {
		return "", fmt.Errorf("Remove unsupported characters from the path.")
	}
	return path, nil
}

func (p FilePicker) Update(msg tea.Msg) (FilePicker, tea.Cmd, PickerResult) {
	result := PickerResult{}
	if p.closed {
		return p, nil, result
	}
	if read, ok := msg.(pickerRead); ok {
		if read.token != p.token {
			return p, nil, result
		}
		p.loading = false
		p.creating = false
		if read.createdName != "" {
			p.mode, p.input = "", ""
		}
		if read.err != nil {
			p.message = read.err.Error()
			return p, nil, result
		}
		if read.selected {
			p.closed = true
			return p, nil, PickerResult{Path: read.path}
		}
		p.directory, p.entries, p.limited, p.selected, p.filter = read.path, read.entries, read.limited, 0, ""
		p.directoryInfo = read.directoryInfo
		if read.createdName != "" {
			if strings.HasPrefix(read.createdName, ".") {
				p.hidden = true
			}
			for i, entry := range p.visible() {
				if entry.name == read.createdName {
					p.selected = i
					break
				}
			}
			p.message = "Folder created. Enter opens it."
		}
		return p, nil, result
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil, result
	}
	if p.creating {
		return p, nil, result
	}
	if key.Type == tea.KeyEsc {
		if p.mode != "" {
			p.mode, p.input, p.message = "", "", ""
			return p, nil, result
		}
		if p.filter != "" {
			p.filter = ""
			p.selected = 0
			return p, nil, result
		}
		p.closed = true
		p.token = pickerSequence.Add(1)
		return p, nil, PickerResult{Cancel: true}
	}
	if p.mode != "" {
		switch key.Type {
		case tea.KeyEnter:
			if p.mode == "mkdir" {
				if !pickerFolderName(p.input) {
					p.message = "Enter one folder name, without slashes, dots alone or outside spaces."
					return p, nil, result
				}
				var cmd tea.Cmd
				p, cmd = p.createFolder()
				return p, cmd, result
			}
			if p.mode == "filter" {
				p.filter = p.input
				p.mode = ""
				p.selected = 0
				return p, nil, result
			}
			path, err := p.expandPath(p.input)
			if err != nil {
				p.message = err.Error()
				return p, nil, result
			}
			p.mode = ""
			var cmd tea.Cmd
			p, cmd = p.load(path, true)
			return p, cmd, result
		case tea.KeyBackspace, tea.KeyDelete:
			runes := []rune(p.input)
			if len(runes) > 0 {
				p.input = string(runes[:len(runes)-1])
			}
		case tea.KeyCtrlU:
			p.input = ""
		case tea.KeySpace:
			if len(p.input) < 4096 {
				p.input += " "
			}
		case tea.KeyRunes:
			if pickerName(string(key.Runes)) && len(p.input)+len(string(key.Runes)) <= 4096 {
				p.input += string(key.Runes)
			}
		}
		if p.mode == "filter" {
			p.filter = p.input
			p.selected = 0
		}
		return p, nil, result
	}
	var target string
	switch key.String() {
	case "/":
		p.mode, p.input, p.message = "filter", p.filter, ""
	case "ctrl+l":
		p.mode, p.input, p.message = "path", p.directory, ""
	case "ctrl+n":
		if !p.loading && p.directory != "" && p.directoryInfo != nil {
			p.mode, p.input, p.message = "mkdir", "", ""
		}
	case "ctrl+h":
		target = p.home
	case "backspace":
		target = filepath.Dir(p.directory)
	case ".":
		p.hidden = !p.hidden
		p.selected = 0
	case "up":
		if p.selected > 0 {
			p.selected--
		}
	case "down":
		if p.selected+1 < len(p.visible()) {
			p.selected++
		}
	case "home":
		p.selected = 0
	case "end":
		if n := len(p.visible()); n > 0 {
			p.selected = n - 1
		}
	case "ctrl+s":
		if (p.kind == "directory" || p.kind == "source") && p.directory != "" && !p.loading {
			var cmd tea.Cmd
			p, cmd = p.load(p.directory, true)
			return p, cmd, result
		}
	case "enter":
		if !p.loading {
			entries := p.visible()
			if p.selected < len(entries) {
				entry := entries[p.selected]
				target = filepath.Join(p.directory, entry.name)
				if !entry.directory {
					var cmd tea.Cmd
					p, cmd = p.load(target, true)
					return p, cmd, result
				}
			}
		}
	}
	if target != "" {
		var cmd tea.Cmd
		p, cmd = p.load(target, false)
		return p, cmd, result
	}
	return p, nil, result
}

func (p FilePicker) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	clean := func(s string) string {
		return ansi.Truncate(strings.NewReplacer("\n", " ", "\t", " ").Replace(validation.SafeText(s)), width, "")
	}
	title := "Choose a file"
	if p.kind == "directory" {
		title = "Choose a folder"
	}
	lines := []string{title, clean(p.directory)}
	if p.mode == "mkdir" {
		lines = []string{clean("New folder"), clean("Create in: " + p.directory), "", clean("Name: [" + importTail(validation.SafeText(p.input), max(1, width-10)) + "|]")}
		if p.message != "" {
			lines = append(lines, clean(p.message))
		}
		lines = append(lines, "", clean("[ Enter Create folder ]  Esc Cancel"))
		if p.creating {
			lines[len(lines)-1] = clean("Creating folder...")
		}
		return strings.Join(lines[:min(height, len(lines))], "\n")
	}
	if p.mode == "path" {
		lines = append(lines, clean("Path: "+p.input))
	} else if p.filter != "" || p.mode == "filter" {
		lines = append(lines, clean("Find: "+p.filter))
	}
	if p.message != "" {
		lines = append(lines, clean(p.message))
	}
	footer := "Enter open/select | / find | Esc back"
	if p.kind == "directory" || p.kind == "source" {
		footer = "Enter open/select | Ctrl+S choose folder | Esc back"
	}
	tips := "Ctrl+N New folder | Backspace parent | Ctrl+H home | Ctrl+L path | . hidden"
	rows := height - len(lines) - 2
	if rows < 0 {
		rows = 0
	}
	entries := p.visible()
	if p.loading {
		entries = nil
		if rows > 0 {
			lines = append(lines, "Loading...")
			rows--
		}
	} else if len(entries) == 0 && rows > 0 {
		lines = append(lines, "No matching files or folders.")
		rows--
	}
	start := 0
	if p.selected >= rows && rows > 0 {
		start = p.selected - rows + 1
	}
	for i := start; i < len(entries) && rows > 0; i++ {
		entry := entries[i]
		marker := "  "
		if i == p.selected {
			marker = "> "
		}
		name := entry.name
		if entry.directory {
			name += "/"
		}
		lines = append(lines, clean(marker+name))
		rows--
	}
	for rows > 0 {
		lines = append(lines, "")
		rows--
	}
	if p.limited {
		tips = "4096 entries shown | Ctrl+L exact path | Ctrl+N New folder"
	}
	if p.mode == "path" {
		footer = "Enter open | Ctrl+U clear | Esc back"
	}
	if p.mode == "filter" {
		footer = "Type a name | Enter done | Esc back"
	}
	lines = append(lines, clean(tips), clean(footer))
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = clean(lines[i])
	}
	return strings.Join(lines, "\n")
}
