package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// ProtectionForm collects ordinary protection options, never password contents
// or authorizations. The shared service verifies the capture and storage again.
type ProtectionForm struct {
	Kind, ID string
	Fields   []GuidedField
	Pools    []domain.StoragePool
	Focus    int
	Error    string
}

func NewProtectionForm(kind, id string) (ProtectionForm, error) {
	f := ProtectionForm{Kind: kind, ID: id}
	if !protectionUUID(id) {
		return f, fmt.Errorf("Select a recovery point in Protection first.")
	}
	field := func(name, label, hint string, limit int) {
		f.Fields = append(f.Fields, GuidedField{Name: name, Label: label, Hint: hint, Limit: limit})
	}
	switch kind {
	case "backup-create":
		field("repository", "Backup folder", "Choose an initialized local encrypted repository. Create one in Protection if needed.", 4096)
		field("passwordFile", "Password file", "Choose an existing private password file outside the repository. Never paste its contents.", 4096)
	case "snapshot-restore":
		field("name", "New VM name", "Give the restored VM a new name. Existing VMs and this recovery point are kept.", 128)
		field("poolID", "Storage pool", "Left/Right chooses an active directory pool for the restored disks.", 36)
	default:
		return f, fmt.Errorf("Choose backup or restore from a recovery point.")
	}
	return f, nil
}

func protectionUUID(id string) bool {
	return guidedUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

// SetPools retains the selection when it remains available. The caller supplies
// the observed local connection's pool inventory; Request checks its identity.
func (f *ProtectionForm) SetPools(pools []domain.StoragePool) {
	f.Pools = nil
	for _, p := range pools {
		if p.Active && p.State == "running" && p.Type == "dir" && p.Key.ProviderID == "libvirt" && p.Key.Kind == "storage-pool" && guidedLocal(p.Key.ConnectionID) && protectionUUID(p.Key.UUID) {
			if slices.ContainsFunc(f.Pools, func(other domain.StoragePool) bool { return other.Key.UUID == p.Key.UUID }) {
				continue
			}
			f.Pools = append(f.Pools, p)
		}
	}
	for i := range f.Fields {
		if f.Fields[i].Name != "poolID" {
			continue
		}
		field := &f.Fields[i]
		field.Choices = nil
		for _, p := range f.Pools {
			field.Choices = append(field.Choices, p.Key.UUID)
		}
		if !slices.Contains(field.Choices, field.Value) {
			field.Value = ""
			if len(field.Choices) > 0 {
				field.Value = field.Choices[0]
			}
		}
	}
}

func (f ProtectionForm) Update(key tea.KeyMsg) (ProtectionForm, bool, bool) {
	f.Fields = slices.Clone(f.Fields)
	if key.Type == tea.KeyEsc {
		return f, false, true
	}
	f.Focus = max(0, min(f.Focus, len(f.Fields)))
	switch key.Type {
	case tea.KeyTab, tea.KeyDown:
		f.Focus = (f.Focus + 1) % (len(f.Fields) + 1)
	case tea.KeyShiftTab, tea.KeyUp:
		f.Focus = (f.Focus + len(f.Fields)) % (len(f.Fields) + 1)
	case tea.KeyEnter:
		if f.Focus < len(f.Fields) {
			f.Focus++
			break
		}
		f.Error = ""
		// The workspace verifies the actual connection before dispatching.
		connection := "qemu:///system"
		for _, p := range f.Pools {
			if p.Key.UUID == f.value("poolID") {
				connection = p.Key.ConnectionID
			}
		}
		if _, _, err := f.Request(connection); err != nil {
			f.Error = err.Error()
			break
		}
		return f, true, false
	default:
		if f.Focus == len(f.Fields) {
			if key.Type == tea.KeySpace {
				return f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			}
			break
		}
		if f.Fields[f.Focus].Name == "poolID" && len(f.Fields[f.Focus].Choices) == 0 {
			f.Error = "No active directory pool is available. Open Storage to check pools."
			break
		}
		editor := GuidedForm{Fields: f.Fields, Focus: f.Focus, Error: f.Error}
		editor, _, _ = editor.Update(key)
		f.Fields, f.Error = editor.Fields, editor.Error
	}
	return f, false, false
}

func (f ProtectionForm) value(name string) string {
	for _, field := range f.Fields {
		if field.Name == name {
			return field.Value
		}
	}
	return ""
}

func (f ProtectionForm) Request(connection string) (string, app.Request, error) {
	fail := func(message string) (string, app.Request, error) {
		return "", app.Request{}, domain.Fail("INVALID_INPUT", message)
	}
	if !guidedLocal(connection) || !protectionUUID(f.ID) {
		return fail("Select a recovery point on a local connection.")
	}
	r := app.Request{Connection: connection, ID: f.ID, Input: map[string]any{}}
	switch f.Kind {
	case "backup-create":
		repo, password := f.value("repository"), f.value("passwordFile")
		if !guidedPath(repo) {
			return fail("Choose an absolute local backup folder path.")
		}
		if !guidedPath(password) {
			return fail("Choose an absolute path to the private password file.")
		}
		if password == repo || strings.HasPrefix(password, strings.TrimSuffix(repo, "/")+"/") {
			return fail("Keep the password file outside the backup folder so it remains available for recovery.")
		}
		r.Action = "create"
		r.Input["repository"], r.Input["passwordFile"] = repo, password
		return "backup.create", r, nil
	case "snapshot-restore":
		name, err := validation.DisplayName(f.value("name"))
		if err != nil || strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 128 {
			return fail("Enter a valid new VM name: 1–128 characters, without slashes or control characters.")
		}
		pool := f.value("poolID")
		if !slices.ContainsFunc(f.Pools, func(p domain.StoragePool) bool {
			return p.Key.ConnectionID == connection && p.Key.ProviderID == "libvirt" && p.Key.Kind == "storage-pool" && p.Key.UUID == pool && protectionUUID(pool) && p.Active && p.State == "running" && p.Type == "dir"
		}) {
			return fail("Choose an active directory pool on this connection. Refresh Storage if none is available.")
		}
		r.Action = "restore"
		r.Input["name"], r.Input["poolID"] = name, pool
		return "snapshot.restore", r, nil
	default:
		return fail("Choose a supported protection action.")
	}
}

func (f ProtectionForm) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	clean := func(s string) string {
		return ansi.Truncate(strings.NewReplacer("\n", " ", "\t", " ").Replace(validation.SafeText(s)), width, "…")
	}
	if width < 40 || height < 10 {
		return strings.Join([]string{clean("Resize to edit protection options."), clean("Your choices are retained.")}[:min(2, height)], "\n")
	}
	title, note, preview := "Back up recovery point", "Encrypt a complete recovery point in your backup folder.", "Preview backup"
	if f.Kind == "snapshot-restore" {
		title, note, preview = "Restore as a new VM", "Create a stopped VM with networking disconnected.", "Preview restore"
	}
	lines := []string{title, note, "Recovery point: " + f.ID, ""}
	rows := []string{}
	for _, field := range f.Fields {
		value := "[ " + field.Value + " ]"
		if field.Name == "poolID" {
			value = "< No active directory pool >"
			for _, p := range f.Pools {
				if p.Key.UUID == field.Value {
					value = "< " + p.Name + " >"
				}
			}
		}
		rows = append(rows, field.Label+": "+value)
	}
	rows = append(rows, "[ "+preview+" ]")
	focus := max(0, min(f.Focus, len(rows)-1))
	count := max(1, height-12)
	start := max(0, focus-count+1)
	for i := start; i < min(len(rows), start+count); i++ {
		mark := "  "
		if i == focus {
			mark = "> "
		}
		lines = append(lines, mark+rows[i])
	}
	lines = append(lines, "")
	hint := "Review the plan before applying changes."
	if focus < len(f.Fields) {
		hint = f.Fields[focus].Hint
	}
	lines = append(lines, wrap(validation.SafeText(hint), width)...)
	if f.Kind == "snapshot-restore" {
		lines = append(lines, "Firmware/TPM restore is unavailable; affected points are refused.")
	}
	if f.Error != "" {
		lines = append(lines, wrap("Issue: "+validation.SafeText(f.Error), width)...)
	}
	if len(lines) >= height {
		lines = lines[:height-1]
	}
	lines = append(lines, "Tab/Arrows Select   Enter Next   Ctrl+O Browse   Esc Back")
	for i := range lines {
		lines[i] = clean(lines[i])
	}
	return strings.Join(lines, "\n")
}
