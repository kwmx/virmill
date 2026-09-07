package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
)

func TestVersionedCaptureTUIIncludesAuxiliaryVerificationAndClearsSuccess(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../tests/fixtures/protection/capture-manifest")); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]any{"path": filepath.Join(root, "manifest.json"), "input": map[string]string{"root": root}})
	client := &recoveryTUIServiceClient{}
	for _, missing := range []bool{false, true} {
		if missing {
			if err := os.Remove(filepath.Join(root, "tpm.fixture")); err != nil {
				t.Fatal(err)
			}
		}
		m := recoveryTUIForm(t, client, "backup verify-manifest")
		m.Input = string(input)
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Model)
		if cmd == nil {
			t.Fatal("TUI did not dispatch verification")
		}
		next, _ = m.Update(cmd())
		m = next.(Model)
		var response app.Response
		if err := json.Unmarshal([]byte(m.Output), &response); err != nil {
			t.Fatal("TUI lost shared response", err, m.Output)
		}
		if missing || runtime.GOOS != "linux" {
			if response.Error == nil || response.Data != nil {
				t.Fatal("TUI retained verification with missing TPM state", response)
			}
		} else {
			report, ok := response.Data.(map[string]any)
			if response.Error != nil || !ok || report["manifestKind"] != "ColdRecoveryPoint" || report["membersChecked"] != true || report["completeCaptureVerified"] != false || report["independentRecoveryVerified"] != false || report["bootTested"] != false {
				t.Fatal("TUI changed integrity into recovery proof", response)
			}
		}
		if m.Plan != nil || m.Confirm || m.Busy || client.method != "backup.verify-manifest" || client.request.Action != "" || client.request.Apply != nil {
			t.Fatal("verification acquired mutation or approval state")
		}
	}
}
