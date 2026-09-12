package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func recoveryFormReceipt() domain.BackupReceipt {
	return domain.BackupReceipt{APIVersion: domain.APIVersion, Kind: "BackupReceipt", OperationID: "4c24969a-5d50-43f5-9df2-7f8994c95984", Connection: "qemu:///system", Repository: "/media/backup/repository", CaptureID: protectionCapture, SnapshotID: strings.Repeat("a", 64), ManifestSHA256: strings.Repeat("b", 64), VerifiedAt: time.Date(2026, 9, 11, 14, 30, 0, 0, time.UTC)}
}
func recoveryFormReady() BackupRecoveryForm {
	f := NewBackupRecoveryForm([]domain.BackupReceipt{recoveryFormReceipt()})
	f.Fields[2].Value = "/home/operator/private/password"
	return f
}

func TestBackupRecoveryFormExactSharedRequestAndRelocatedRepository(t *testing.T) {
	f := recoveryFormReady()
	receipt := f.Receipts[0]
	f.Fields[1].Value = "/media/new-drive/encrypted-backup"
	method, r, err := f.Request("qemu:///session")
	want := app.Request{Connection: "qemu:///session", ID: receipt.SnapshotID, Action: "restore", Input: map[string]any{"repository": "/media/new-drive/encrypted-backup", "passwordFile": "/home/operator/private/password", "captureID": receipt.CaptureID, "manifestSHA256": receipt.ManifestSHA256}}
	if err != nil || method != "backup.restore" || !reflect.DeepEqual(r, want) {
		t.Fatalf("unexpected preview %s %+v %v", method, r, err)
	}
	if f.Receipts[0].Repository != receipt.Repository {
		t.Fatal("relocation edited immutable receipt")
	}
	if guidedBrowseKind(f.Fields[1].Name) != "directory" || guidedBrowseKind(f.Fields[2].Name) != "file" || guidedBrowseKind(f.Fields[0].Name) != "" {
		t.Fatal("file picker targets changed")
	}
}

func TestBackupRecoveryFormFieldNavigationNeverSubmitsEarly(t *testing.T) {
	f := recoveryFormReady()
	for _, focus := range []int{1, 2, 3} {
		var submit, back bool
		f, submit, back = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if submit || back || f.Focus != focus || f.Error != "" {
			t.Fatalf("field enter submitted or lost focus: %+v", f)
		}
	}
	before := f
	f, submit, back := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !submit || back || f.Error != "" {
		t.Fatal("preview unavailable", f.Error)
	}
	f, submit, back = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if submit || !back || !reflect.DeepEqual(f.Fields, before.Fields) || f.Selected != before.Selected {
		t.Fatal("Back discarded input")
	}
}

func TestBackupRecoveryFormReceiptSelectionNeverTypesHashes(t *testing.T) {
	first := recoveryFormReceipt()
	second := first
	second.SnapshotID = strings.Repeat("c", 64)
	second.Repository = "/media/second/repo"
	f := NewBackupRecoveryForm([]domain.BackupReceipt{first, second})
	f.Fields[2].Value = "/private/first-password"
	before := f
	f, submit, back := f.Update(tea.KeyMsg{Type: tea.KeyRight})
	if submit || back || f.Selected != 1 || f.Fields[1].Value != second.Repository || f.Fields[2].Value != "" {
		t.Fatal("selection retained previous repository credentials", f)
	}
	if before.Fields[2].Value != "/private/first-password" {
		t.Fatal("selection mutated previous form")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("d", 64))})
	if f.Selected != 1 || f.Receipts[1].SnapshotID != second.SnapshotID {
		t.Fatal("typed text changed receipt coordinates")
	}
	single := recoveryFormReady()
	single, _, _ = single.Update(tea.KeyMsg{Type: tea.KeyRight})
	if single.Fields[2].Value == "" {
		t.Fatal("cycling only receipt discarded credentials")
	}
}

func TestBackupRecoveryFormEmptyOrInvalidReceiptsCannotSubmit(t *testing.T) {
	empty := NewBackupRecoveryForm(nil)
	if _, _, err := empty.Request("qemu:///system"); err == nil || !strings.Contains(err.Error(), "Choose a saved backup receipt") {
		t.Fatal("empty recovery silently accepted", err)
	}
	if !strings.Contains(empty.View(80, 24), "new installation") {
		t.Fatal("empty recovery lacks next-step guidance")
	}
	mutations := []func(*domain.BackupReceipt){
		func(r *domain.BackupReceipt) { r.APIVersion = "virmill/v2" }, func(r *domain.BackupReceipt) { r.Kind = "Other" }, func(r *domain.BackupReceipt) { r.OperationID = "unknown" },
		func(r *domain.BackupReceipt) { r.Connection = "qemu+ssh://other/system" }, func(r *domain.BackupReceipt) { r.Repository = "relative" }, func(r *domain.BackupReceipt) { r.CaptureID = "00000000-0000-0000-0000-000000000000" },
		func(r *domain.BackupReceipt) { r.SnapshotID = strings.Repeat("0", 64) }, func(r *domain.BackupReceipt) { r.SnapshotID = strings.Repeat("A", 64) }, func(r *domain.BackupReceipt) { r.ManifestSHA256 = "short" }, func(r *domain.BackupReceipt) { r.VerifiedAt = time.Time{} },
	}
	for i, change := range mutations {
		f := recoveryFormReady()
		change(&f.Receipts[0])
		method, r, err := f.Request("qemu:///system")
		if err == nil || method != "" || !reflect.DeepEqual(r, app.Request{}) {
			t.Fatalf("invalid receipt %d accepted: %v", i, err)
		}
	}
}

func TestBackupRecoveryFormCredentialReferencesAreExternalAndCanonical(t *testing.T) {
	for _, path := range []string{"", "password text", "/media/backup/repository/password", "/media/backup/repository", "/private/../password", "/private/password\x1b[31m"} {
		f := recoveryFormReady()
		f.Fields[2].Value = path
		if _, _, err := f.Request("qemu:///system"); err == nil {
			t.Fatalf("accepted password reference %q", path)
		}
	}
	f := recoveryFormReady()
	if _, _, err := f.Request("qemu+ssh://other/system"); err == nil {
		t.Fatal("remote recovery accepted")
	}
	f.Fields = f.Fields[:2]
	if _, _, err := f.Request("qemu:///system"); err == nil {
		t.Fatal("missing credential field accepted")
	}
}

func TestBackupRecoveryFormRenderingAndHonestScope(t *testing.T) {
	f := recoveryFormReady()
	f.Fields[1].Value = "/media/long-" + strings.Repeat("repo", 40) + "\x1b[2J\nspoofed"
	for _, size := range [][2]int{{80, 18}, {120, 30}, {40, 10}, {20, 4}, {1, 1}} {
		for focus := 0; focus <= len(f.Fields); focus++ {
			f.Focus = focus
			view := f.View(size[0], size[1])
			lines := strings.Split(view, "\n")
			if len(lines) > size[1] {
				t.Fatal("form too tall", size)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] || strings.Contains(line, "\x1b") {
					t.Fatalf("unsafe/wide line %v %q", size, line)
				}
			}
		}
	}
	view := f.View(80, 24)
	for _, want := range []string{"Recover backup files", "no VM starts here", "Preview recovery", "2026-09-11", protectionCapture[:8]} {
		if !strings.Contains(view, want) {
			t.Fatal("missing meaningful recovery context", want, view)
		}
	}
	if strings.Contains(view, strings.Repeat("a", 64)) || strings.Contains(view, strings.Repeat("b", 64)) {
		t.Fatal("ordinary form exposes raw restore coordinates")
	}
}
