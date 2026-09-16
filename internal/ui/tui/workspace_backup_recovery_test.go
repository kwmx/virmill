package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
)

func openRecoveryWorkspace(t *testing.T, receipts []domain.BackupReceipt) Workspace {
	t.Helper()
	m := fixtureWorkspace()
	m.Section = 6
	client := m.Client.(*workspaceClient)
	client.response = app.Response{Data: receipts}
	cmd := m.openAction(ui.Action{Command: "backup restore", Method: "backup.restore", Mutation: "restore"})
	if cmd == nil || m.BackupRecovery == nil || !m.Busy || !m.BackupRecoveryLoading || m.ActionForm != nil {
		t.Fatal("recovery did not start read-only history selection")
	}
	next, follow := m.Update(cmd())
	m = next.(Workspace)
	if follow != nil || m.Busy || m.BackupRecoveryLoading || len(client.calls) != 1 || client.calls[0] != "backup.receipts.list" {
		t.Fatal("receipt load submitted unexpected work", m.Error, client.calls)
	}
	return m
}

func TestWorkspaceBackupRecoveryReadsHistoryThenPreviewsExactSelectors(t *testing.T) {
	receipt := recoveryFormReceipt()
	m := openRecoveryWorkspace(t, []domain.BackupReceipt{receipt})
	if m.BackupRecovery == nil || !strings.Contains(m.View(), "Recover backup files") || strings.Contains(m.View(), "Input: path/ID") {
		t.Fatal("recovery is not a guided form", m.View())
	}
	m.BackupRecovery.Fields[2].Value = "/private/recovery-password"
	m.BackupRecovery.Focus = 3
	before := *m.BackupRecovery
	p := testWorkspacePlan(t)
	p.Operation = "backup.local-restore-v1"
	p.Acknowledgements = []string{"local-storage-mutation", "recovered-set-publication"}
	p.Digest, _ = operations.PlanDigest(p)
	client := m.Client.(*workspaceClient)
	client.response = app.Response{Data: p}
	m, cmd := wk(m, "enter")
	if cmd == nil || !m.Busy || m.Plan != nil {
		t.Fatal("explicit preview did not request a plan")
	}
	next, follow := m.Update(cmd())
	m = next.(Workspace)
	if follow != nil || m.Plan == nil || m.BackupRecovery == nil || m.PlanDetails || m.Busy || len(client.calls) != 2 || client.calls[1] != "backup.restore" {
		t.Fatal("plan did not retain recovery form", m.Error, client.calls)
	}
	request := client.requests[1]
	if request.ID != receipt.SnapshotID || request.Apply != nil || request.Input["captureID"] != receipt.CaptureID || request.Input["manifestSHA256"] != receipt.ManifestSHA256 || request.Input["passwordFile"] != "/private/recovery-password" {
		t.Fatal("preview changed selectors or applied", request)
	}
	m, cmd = wk(m, "esc")
	if cmd != nil || m.Plan != nil || !reflect.DeepEqual(*m.BackupRecovery, before) || len(client.calls) != 2 {
		t.Fatal("plan Back discarded form or executed work")
	}
}

func TestWorkspaceBackupRecoveryEmptyEnterImportsSavedReceiptThroughService(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	path := filepath.Join(root, "saved-backup.json")
	if err := os.WriteFile(path, []byte("metadata picker fixture; service validates contents"), 0600); err != nil {
		t.Fatal(err)
	}
	m := openRecoveryWorkspace(t, []domain.BackupReceipt{})
	m, cmd := wk(m, "enter")
	if cmd == nil || m.Picker == nil || !m.PickerBackupReceipt || m.Busy {
		t.Fatal("empty state did not open receipt browser")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal("file selection missing")
	}
	next, cmd = m.Update(cmd())
	m = next.(Workspace)
	if cmd == nil || m.Picker != nil || !m.Busy || m.BackupRecovery == nil {
		t.Fatal("receipt selection did not call shared read")
	}
	receipt := recoveryFormReceipt()
	client := m.Client.(*workspaceClient)
	client.response = app.Response{Data: receipt}
	next, follow := m.Update(cmd())
	m = next.(Workspace)
	if follow != nil || m.Busy || m.Plan != nil || m.BackupRecovery.Selected != 0 || m.BackupRecovery.Focus != 1 || len(m.BackupRecovery.Receipts) != 1 || client.calls[len(client.calls)-1] != "backup.receipt.read" || client.requests[len(client.requests)-1].Path != path {
		t.Fatal("imported receipt did not populate guided recovery", m.Error, client.calls)
	}
}

func TestWorkspaceBackupRecoveryPickersAndExportPreserveForm(t *testing.T) {
	m := openRecoveryWorkspace(t, []domain.BackupReceipt{recoveryFormReceipt()})
	for _, test := range []struct {
		focus int
		kind  string
	}{{1, "directory"}, {2, "file"}} {
		m.BackupRecovery.Focus = test.focus
		before := *m.BackupRecovery
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
		m = next.(Workspace)
		if cmd == nil || m.Picker == nil || !m.PickerBackupRecovery || m.PickerBackupReceipt || m.Picker.kind != test.kind {
			t.Fatal("backup path picker misrouted", test)
		}
		m, cmd = wk(m, "esc")
		if cmd != nil || m.Picker != nil || !reflect.DeepEqual(*m.BackupRecovery, before) {
			t.Fatal("picker cancel changed form")
		}
	}
	m.BackupRecovery.Focus = 4
	m, cmd := wk(m, "enter")
	if cmd != nil || m.ExportForm == nil || m.Plan != nil || m.Busy {
		t.Fatal("Save receipt did not open explicit destination form")
	}
	m, cmd = wk(m, "esc")
	if cmd != nil || m.ExportForm != nil || m.BackupRecovery == nil || m.BackupRecovery.Focus != 4 {
		t.Fatal("export cancel did not return to recovery")
	}
}

func TestWorkspaceBackupRecoveryLateRepliesAndNavigationAreIgnored(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 6
	cmd := m.openBackupRecovery()
	token := m.Pending["backup-receipts"]
	if cmd == nil {
		t.Fatal("no pending read")
	}
	m, _ = wk(m, "esc")
	next, _ := m.Update(workspaceReply{Kind: "backup-receipts", Token: token, Response: app.Response{Data: []domain.BackupReceipt{recoveryFormReceipt()}}})
	m = next.(Workspace)
	if m.BackupRecovery != nil || m.Busy {
		t.Fatal("late history reply resurrected dismissed form")
	}
	m = openRecoveryWorkspace(t, []domain.BackupReceipt{recoveryFormReceipt()})
	m.Busy = true
	m.sequence++
	m.Pending["backup-receipt-read"] = m.sequence
	token = m.sequence
	m.page(1)
	next, _ = m.Update(workspaceReply{Kind: "backup-receipt-read", Token: token, Response: app.Response{Data: recoveryFormReceipt()}})
	m = next.(Workspace)
	if m.BackupRecovery != nil || m.Section != 1 || m.Busy {
		t.Fatal("navigation accepted stale receipt")
	}
}

func TestWorkspaceBackupRecoveryReadAndPlanErrorsKeepInputs(t *testing.T) {
	m := openRecoveryWorkspace(t, []domain.BackupReceipt{recoveryFormReceipt()})
	m.BackupRecovery.Fields[2].Value = "/private/password"
	m.BackupRecovery.Focus = 3
	client := m.Client.(*workspaceClient)
	client.err = fmt.Errorf("repository unavailable; reconnect the backup disk")
	m, cmd := wk(m, "enter")
	if cmd == nil {
		t.Fatal("plan request missing")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Busy || m.Plan != nil || m.BackupRecovery.Fields[2].Value != "/private/password" || !strings.Contains(m.BackupRecovery.Error, "reconnect") {
		t.Fatal("plan failure lost guidance or input", m.Error)
	}
	m.BackupRecovery.Focus = 0
	m.Busy = true
	m.sequence++
	m.Pending["backup-receipt-read"] = m.sequence
	next, _ = m.Update(workspaceReply{Kind: "backup-receipt-read", Token: m.sequence, Response: app.Response{Error: domain.Fail("INVALID_INPUT", "Not a saved receipt")}})
	m = next.(Workspace)
	if m.Busy || m.BackupRecovery == nil || !strings.Contains(m.BackupRecovery.Error, "Not a saved receipt") {
		t.Fatal("receipt error not editable")
	}
}

func TestWorkspaceBackupRecoveryApplyAndNarrowCancelClearDraft(t *testing.T) {
	m := openRecoveryWorkspace(t, []domain.BackupReceipt{recoveryFormReceipt()})
	m.sequence++
	m.Pending["apply"] = m.sequence
	next, _ := m.Update(workspaceReply{Kind: "apply", Token: m.sequence, Response: app.Response{Data: domain.Job{ID: protectionCapture, State: "queued"}}})
	m = next.(Workspace)
	if m.BackupRecovery != nil || m.Section != 8 {
		t.Fatal("accepted recovery left an active submission draft")
	}
	m = openRecoveryWorkspace(t, []domain.BackupReceipt{recoveryFormReceipt()})
	next, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	m = next.(Workspace)
	m, _ = wk(m, "esc")
	if m.BackupRecovery != nil || m.Pending["backup-receipts"] != 0 || m.Pending["backup-receipt-read"] != 0 {
		t.Fatal("narrow cancel retained stale draft requests")
	}
}
