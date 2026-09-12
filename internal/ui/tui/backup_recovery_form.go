package tui

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// BackupRecoveryForm selects immutable backup coordinates and collects only
// local path references. Recovery still requires the shared service's reviewed
// plan, repository authentication and full member verification.
type BackupRecoveryForm struct {
	Receipts []domain.BackupReceipt
	Fields   []GuidedField
	Selected int
	Focus    int
	Error    string
}

func NewBackupRecoveryForm(receipts []domain.BackupReceipt) BackupRecoveryForm {
	f := BackupRecoveryForm{Receipts: slices.Clone(receipts), Fields: []GuidedField{
		{Name: "receipt", Label: "Saved backup", Hint: "Left/Right chooses a saved receipt; it identifies the exact backup to recover."},
		{Name: "repository", Label: "Backup folder", Hint: "Ctrl+O chooses the local encrypted repository. Change this if its mount location moved.", Limit: 4096},
		{Name: "passwordFile", Label: "Password file", Hint: "Ctrl+O chooses an existing private password file outside the repository. No password text.", Limit: 4096},
	}}
	if len(receipts) > 0 {
		f.Fields[1].Value = receipts[0].Repository
		f.Fields[1].Cursor = len([]rune(receipts[0].Repository))
	}
	return f
}

func (f BackupRecoveryForm) Update(key tea.KeyMsg) (BackupRecoveryForm, bool, bool) {
	f.Fields = slices.Clone(f.Fields)
	if key.Type == tea.KeyEsc {
		return f, false, true
	}
	if len(f.Fields) != 3 {
		f.Error = "Reopen backup recovery; this form is incomplete."
		return f, false, false
	}
	f.Focus = max(0, min(f.Focus, len(f.Fields)+1))
	switch key.Type {
	case tea.KeyTab, tea.KeyDown:
		f.Focus = (f.Focus + 1) % (len(f.Fields) + 2)
	case tea.KeyShiftTab, tea.KeyUp:
		f.Focus = (f.Focus + len(f.Fields) + 1) % (len(f.Fields) + 2)
	case tea.KeyEnter:
		if f.Focus == len(f.Fields)+1 {
			return f, false, false
		} // Workspace owns receipt export.
		if f.Focus < len(f.Fields) {
			f.Focus++
			f.Error = ""
			break
		}
		connection := "qemu:///system"
		if f.Selected >= 0 && f.Selected < len(f.Receipts) {
			connection = f.Receipts[f.Selected].Connection
		}
		_, _, err := f.Request(connection)
		if err != nil {
			f.Error = err.Error()
			break
		}
		f.Error = ""
		return f, true, false
	default:
		if f.Focus >= len(f.Fields) {
			if key.Type == tea.KeySpace {
				return f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			}
			break
		}
		if f.Focus == 0 {
			if len(f.Receipts) == 0 {
				break
			}
			if key.Type == tea.KeyLeft || key.Type == tea.KeyRight || key.Type == tea.KeySpace {
				delta := 1
				if key.Type == tea.KeyLeft {
					delta = -1
				}
				previous := f.Selected
				f.Selected = (max(0, min(f.Selected, len(f.Receipts)-1)) + delta + len(f.Receipts)) % len(f.Receipts)
				if previous == f.Selected {
					break
				}
				f.Fields[1].Value = f.Receipts[f.Selected].Repository
				f.Fields[1].Cursor = len([]rune(f.Fields[1].Value))
				// A credential selected for another repository is never carried silently.
				f.Fields[2].Value = ""
				f.Fields[2].Cursor = 0
				f.Error = ""
			}
			break
		}
		editor := GuidedForm{Fields: f.Fields, Focus: f.Focus, Error: f.Error}
		editor, _, _ = editor.Update(key)
		f.Fields, f.Error = editor.Fields, editor.Error
	}
	return f, false, false
}

func recoveryHash(s string) bool {
	if len(s) != 64 || s == strings.Repeat("0", 64) || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func validRecoveryReceipt(r domain.BackupReceipt) bool {
	return r.APIVersion == domain.APIVersion && r.Kind == "BackupReceipt" && protectionUUID(r.OperationID) && guidedLocal(r.Connection) && guidedPath(r.Repository) && protectionUUID(r.CaptureID) && recoveryHash(r.SnapshotID) && recoveryHash(r.ManifestSHA256) && !r.VerifiedAt.IsZero()
}

func (f BackupRecoveryForm) Request(connection string) (string, app.Request, error) {
	fail := func(message string) (string, app.Request, error) {
		return "", app.Request{}, domain.Fail("INVALID_INPUT", message)
	}
	if !guidedLocal(connection) {
		return fail("Select a local connection before recovering files.")
	}
	if f.Selected < 0 || f.Selected >= len(f.Receipts) {
		return fail("Choose a saved backup receipt to recover on a new installation.")
	}
	receipt := f.Receipts[f.Selected]
	if !validRecoveryReceipt(receipt) {
		return fail("This backup receipt is incomplete or unsupported. Choose a valid saved receipt.")
	}
	if len(f.Fields) != 3 || f.Fields[0].Name != "receipt" || f.Fields[1].Name != "repository" || f.Fields[2].Name != "passwordFile" {
		return fail("Reopen backup recovery; this form is incomplete.")
	}
	repository, password := f.Fields[1].Value, f.Fields[2].Value
	if !guidedPath(repository) {
		return fail("Choose the encrypted repository's absolute local folder path.")
	}
	if !guidedPath(password) {
		return fail("Choose an existing private password file using Ctrl+O; never paste a password.")
	}
	if password == repository || strings.HasPrefix(password, strings.TrimSuffix(repository, "/")+"/") {
		return fail("Keep the password file outside the encrypted repository.")
	}
	return "backup.restore", app.Request{Connection: connection, ID: receipt.SnapshotID, Action: "restore", Input: map[string]any{"repository": repository, "passwordFile": password, "captureID": receipt.CaptureID, "manifestSHA256": receipt.ManifestSHA256}}, nil
}

func (f BackupRecoveryForm) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	clean := func(s string) string {
		return ansi.Truncate(strings.NewReplacer("\n", " ", "\t", " ").Replace(validation.SafeText(s)), width, "…")
	}
	if width < 40 || height < 10 {
		return strings.Join([]string{clean("Resize to recover backup files."), clean("Your choices are retained.")}[:min(2, height)], "\n")
	}
	lines := []string{"Recover backup files", "Recover a verified local copy from an encrypted repository.", "Then use Protection to restore a new VM; no VM starts here.", ""}
	footer := []string{}
	if f.Error != "" {
		footer = append(footer, wrap("Issue: "+validation.SafeText(f.Error), width)...)
	}
	footer = append(footer, "Tab/Arrows Select   Enter Next/Choose   Ctrl+O Browse   Esc Back")
	if len(footer) > height/3 {
		footer = append(footer[:max(1, height/3-1)], footer[len(footer)-1])
	}
	if len(f.Receipts) == 0 {
		lines = append(lines, wrap("Choose a saved backup receipt to recover on a new installation.", width)...)
		lines = append(lines, wrap("A receipt records the backup identity; the password stays in a separate private file.", width)...)
		if len(lines) > height-len(footer) {
			lines = lines[:height-len(footer)]
		}
		lines = append(lines, footer...)
		for i := range lines {
			lines[i] = clean(lines[i])
		}
		return strings.Join(lines, "\n")
	}
	selected := max(0, min(f.Selected, len(f.Receipts)-1))
	receipt := f.Receipts[selected]
	date := "Unknown date"
	if !receipt.VerifiedAt.IsZero() {
		date = receipt.VerifiedAt.Local().Format("2006-01-02 15:04 MST")
	}
	capture := receipt.CaptureID
	if len(capture) > 8 {
		capture = capture[:8]
	}
	summary := fmt.Sprintf("%d/%d · %s · capture %s", selected+1, len(f.Receipts), date, capture)
	fields := f.Fields
	rows := []string{"Saved backup: < " + summary + " >"}
	for i := 1; i < min(len(fields), 3); i++ {
		field := fields[i]
		value := validation.SafeText(field.Value)
		room := max(4, width-ansi.StringWidth(field.Label)-7)
		if f.Focus == i {
			runes := []rune(value)
			cursor := max(0, min(field.Cursor, len(runes)))
			before := string(runes[:cursor])
			cells := ansi.StringWidth(before)
			if cells >= room {
				before = "…" + ansi.Cut(before, cells-room+2, cells)
			}
			value = before + "|" + string(runes[cursor:])
		}
		rows = append(rows, field.Label+": ["+ansi.Truncate(value, room, "…")+"]")
	}
	rows = append(rows, "[ Preview recovery ]", "[ Save recovery receipt ]")
	focus := max(0, min(f.Focus, len(rows)-1))
	count := max(1, height-len(lines)-len(footer)-3)
	first := max(0, focus-count+1)
	for i := first; i < min(len(rows), first+count); i++ {
		prefix := "  "
		if i == focus {
			prefix = "> "
		}
		lines = append(lines, prefix+rows[i])
	}
	hint := "Existing local recovery points are kept. Review before recovering files."
	if focus < len(fields) {
		hint = fields[focus].Hint
	} else if focus == 4 {
		hint = "Save a separate recovery receipt for use without this installation. Passwords are never included."
	}
	lines = append(lines, wrap(validation.SafeText(hint), width)...)
	if len(lines) > height-len(footer) {
		lines = lines[:height-len(footer)]
	}
	lines = append(lines, footer...)
	for i := range lines {
		lines[i] = clean(lines[i])
	}
	return strings.Join(lines, "\n")
}
