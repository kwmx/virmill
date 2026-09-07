package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/domain"
)

func TestProtectionInspectionAndManifestForms(t *testing.T) {
	for _, tc := range []struct{ command, input, method string }{
		{"vm recovery inspect", "vm-id", "vm.recovery.inspect"},
		{"backup verify-manifest", `{"path":"/tmp/manifest.json","input":{"root":"/tmp/recovery"}}`, "backup.verify-manifest"},
	} {
		r := &recorder{}
		m := New(r, "qemu:///system")
		for i, s := range sections {
			if s == "Protection" {
				m.Section = i
			}
		}
		found := false
		for i, a := range m.actions() {
			if a.Command == tc.command {
				m.Selected = i
				found = true
			}
		}
		if !found {
			t.Fatal("protection action missing")
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Model)
		if !m.Editing || cmd != nil {
			t.Fatal("form missing")
		}
		m.Input = tc.input
		_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("form did not dispatch")
		}
		cmd()
		if r.method != tc.method || r.request.Action != "" || r.request.Apply != nil {
			t.Fatal("TUI read became mutation", r)
		}
		if tc.method == "backup.verify-manifest" && (r.request.Path != "/tmp/manifest.json" || r.request.Input["root"] != "/tmp/recovery") {
			t.Fatal("manifest form input lost", r)
		}
	}
}

type recoveryTUIServiceClient struct {
	service        app.Service
	calls          int
	method         string
	request        app.Request
	transportError error
	cancelCall     bool
}

func (c *recoveryTUIServiceClient) Call(ctx context.Context, method string, request app.Request) (app.Response, error) {
	c.calls++
	c.method, c.request = method, request
	if c.transportError != nil {
		return app.Response{}, c.transportError
	}
	if c.cancelCall {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		ctx = canceled
	}
	return c.service.Call(ctx, 1000, method, request), nil
}

func recoveryTUIForm(t *testing.T, client *recoveryTUIServiceClient, command string) Model {
	t.Helper()
	m := New(client, "qemu:///session")
	m.Height = 48
	for i, name := range sections {
		if name == "Protection" {
			m.Section = i
		}
	}
	found := false
	for i, a := range m.actions() {
		if a.Command == command {
			m.Selected, found = i, true
		}
	}
	if !found {
		t.Fatal("recovery action is not reachable in Protection", command)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.Editing || cmd != nil {
		t.Fatal("recovery action did not open a read-only input form")
	}
	return m
}

func recoveryTUIManifest(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	m := protection.Manifest{APIVersion: domain.APIVersion, ID: "tui-backup", VMID: "tui-vm", Mode: "cold", Consistency: "cold-complete", IndependentlyRecoverable: true}
	for _, kind := range []string{"disk", "persistent-xml"} {
		body := []byte("TUI declared " + kind)
		hash := sha256.Sum256(body)
		if err := os.WriteFile(filepath.Join(dir, kind), body, 0600); err != nil {
			t.Fatal(err)
		}
		m.Required = append(m.Required, kind)
		m.Members = append(m.Members, protection.Member{ID: kind, Kind: kind, Path: kind, Size: int64(len(body)), SHA256: hex.EncodeToString(hash[:])})
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(filename, b, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, filename
}

func TestRecoveryTUIManifestRootReachesActualMemberVerification(t *testing.T) {
	dir, filename := recoveryTUIManifest(t)
	client := &recoveryTUIServiceClient{}
	for _, phase := range []string{"declaration", "members", "corrupt-member"} {
		if phase == "corrupt-member" {
			if err := os.WriteFile(filepath.Join(dir, "disk"), []byte("corrupt"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		form := map[string]any{"path": filename}
		if phase != "declaration" {
			form["input"] = map[string]any{"root": dir}
		}
		b, _ := json.Marshal(form)
		m := recoveryTUIForm(t, client, "backup verify-manifest")
		m.Input = string(b)
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Model)
		if cmd == nil {
			t.Fatal("manifest form did not dispatch")
		}
		next, _ = m.Update(cmd())
		m = next.(Model)
		var response app.Response
		if err := json.Unmarshal([]byte(m.Output), &response); err != nil {
			t.Fatal("TUI lost shared-service result envelope", err, m.Output)
		}
		if runtime.GOOS != "linux" {
			if response.Error == nil || response.Error.Code != "UNSUPPORTED_CAPABILITY" || response.Data != nil {
				t.Fatal("unsupported TUI platform returned verification", response)
			}
		} else if phase == "corrupt-member" {
			if response.Error == nil || response.Data != nil {
				t.Fatal("TUI root member verification did not detect corruption", response)
			}
		} else {
			data, ok := response.Data.(map[string]any)
			if response.Error != nil || !ok || data["membersChecked"] != (phase == "members") || data["completeCaptureVerified"] != false || data["independentRecoveryVerified"] != false || data["bootTested"] != false {
				t.Fatal("TUI changed declared verification into recovery proof", response)
			}
		}
		if client.method != "backup.verify-manifest" || client.request.Connection != "qemu:///session" || client.request.Action != "" || client.request.Apply != nil || m.Plan != nil || m.Confirm || m.Busy {
			t.Fatal("TUI recovery read acquired a mutation/approval state")
		}
	}
}

func TestRecoveryTUIFailuresClearStaleApprovalAndRemainVisible(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transport error
		cancel    bool
	}{
		{"unsupported-native", nil, false}, {"transport-error", errors.New("fixture recovery connection failed"), false}, {"canceled-observation", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &recoveryTUIServiceClient{transportError: tc.transport, cancelCall: tc.cancel}
			m := recoveryTUIForm(t, client, "vm recovery inspect")
			m.Input = "vm-id"
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(Model)
			if cmd == nil {
				t.Fatal("inspection form did not dispatch")
			}
			m.Plan = &domain.Plan{ID: "earlier-plan", Digest: "earlier-digest"}
			m.Confirm = true
			m.Offset = 999
			next, _ = m.Update(cmd())
			m = next.(Model)
			if m.Plan != nil || m.Confirm || m.Busy || m.Offset != 0 || strings.Contains(m.View(), "Plan is a preview") || strings.Contains(m.Output, "manifest-checked") {
				t.Fatal("failed recovery read retained stale approval/success state", m.Output)
			}
			if tc.transport != nil {
				if !strings.Contains(m.View(), "fixture recovery connection failed") {
					t.Fatal("transport failure is not visible")
				}
			} else {
				var response app.Response
				if err := json.Unmarshal([]byte(m.Output), &response); err != nil || response.Error == nil || response.Data != nil {
					t.Fatal("TUI error retained partial native data", response, err)
				}
				if !strings.Contains(m.View(), response.Error.Code) {
					t.Fatal("native/cancellation error is not visible")
				}
			}
		})
	}
}

func TestRecoveryTUIStrictFormsPathsAndEscapeNeverDispatchUnsafeInput(t *testing.T) {
	for _, raw := range []string{
		`{"path":"/tmp/a","path":"/tmp/b"}`, `{"path":"/tmp/a","unexpected":true}`,
	} {
		client := &recoveryTUIServiceClient{}
		m := recoveryTUIForm(t, client, "backup verify-manifest")
		m.Input = raw
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Model)
		if cmd != nil || client.calls != 0 || !m.Editing || m.Busy {
			t.Fatal("invalid recovery JSON reached shared service")
		}
	}
	for _, raw := range []string{
		`{"path":"/tmp/alias/../manifest.json"}`,
		`{"path":"/tmp/manifest.json","input":{"root":"/tmp/alias/../members"}}`,
	} {
		client := &recoveryTUIServiceClient{}
		m := recoveryTUIForm(t, client, "backup verify-manifest")
		m.Input = raw
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Model)
		if cmd == nil {
			t.Fatal("expected a normalization result command")
		}
		next, _ = m.Update(cmd())
		m = next.(Model)
		if client.calls != 0 || !strings.Contains(m.Output, "INVALID_INPUT") || m.Busy {
			t.Fatal("ambiguous path was cleaned into a dispatched request", m.Output)
		}
	}
	client := &recoveryTUIServiceClient{}
	m := recoveryTUIForm(t, client, "backup verify-manifest")
	m.Input = `{"path":"/tmp/manifest.json","input":{"root":"/tmp/members"}}`
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if cmd != nil || client.calls != 0 || m.Editing || m.Busy || m.Confirm {
		t.Fatal("Esc from recovery form dispatched an operation")
	}
	client = &recoveryTUIServiceClient{}
	m = recoveryTUIForm(t, client, "backup verify-manifest")
	m.Input = `{"path":"manifest.json","input":{"root":"members"}}`
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("canonical relative paths did not dispatch")
	}
	cmd()
	manifestPath, _ := filepath.Abs("manifest.json")
	memberRoot, _ := filepath.Abs("members")
	if client.request.Path != manifestPath || client.request.Input["root"] != memberRoot {
		t.Fatal("TUI relative paths were not resolved in client directory")
	}
}
