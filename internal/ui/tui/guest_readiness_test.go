package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

type readinessTUIObserver struct {
	domain.ComputeProvider
	result         domain.GuestReadiness
	err            error
	connection, id string
	calls          int
}

func (o *readinessTUIObserver) InspectGuestReadiness(ctx context.Context, connection, id string) (domain.GuestReadiness, error) {
	o.calls++
	o.connection, o.id = connection, id
	if err := ctx.Err(); err != nil {
		return domain.GuestReadiness{}, err
	}
	if o.err != nil {
		return domain.GuestReadiness{}, o.err
	}
	return o.result, nil
}

func readinessTUIOpen(t *testing.T, m Model) Model {
	t.Helper()
	found := false
	for section, name := range sections {
		if name != "VMs" {
			continue
		}
		m.Section = section
		for index, action := range m.actions() {
			if action.Command == "vm readiness show" {
				m.Selected = index
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("readiness view inaccessible")
	}
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.Editing || m.Busy {
		t.Fatal("readiness navigation submitted work")
	}
	return m
}

// The model and shared service are real; observations are deterministic fixture
// data, not proof that a guest agent or an application is ready.
func TestGuestReadinessTUIExactObservationAndNoApplicationClaim(t *testing.T) {
	for _, connection := range []string{"qemu:///session", "qemu:///system"} {
		for _, state := range []string{"responsive", "unresponsive", "disconnected", "absent", "unknown"} {
			t.Run(connection+"/"+state, func(t *testing.T) {
				id := "12345678-1234-1234-1234-123456789abc"
				o := &readinessTUIObserver{result: domain.GuestReadiness{Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: connection, Kind: "vm", UUID: id}, State: state, AgentConnected: state == "responsive" || state == "unresponsive", AgentResponsive: state == "responsive", Evidence: "fixture-observation-" + state, ObservedAt: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}}
				c := &recoveryTUIServiceClient{service: app.Service{Provider: o}}
				m := readinessTUIOpen(t, New(c, connection))
				m = coldTUIType(t, m, id)
				m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEsc})
				if c.calls != 0 || o.calls != 0 || m.Editing {
					t.Fatal("cancel submitted observation")
				}
				m = readinessTUIOpen(t, m)
				m = coldTUIType(t, m, id)
				m = coldTUISubmit(t, m)
				want, _ := json.MarshalIndent(app.Response{APIVersion: domain.APIVersion, Data: o.result, Warnings: []string{}}, "", "  ")
				if m.Output != string(want) || m.Plan != nil || m.Confirm {
					t.Fatal("readiness changed shared evidence or gained approval", m.Output)
				}
				if c.method != "vm.readiness.show" || c.request.ID != id || c.request.Connection != connection || c.request.Action != "" || c.request.Apply != nil || len(c.request.Input) != 0 || o.id != id || o.connection != connection {
					t.Fatal("readiness changed selection", c.request)
				}
				pages := ""
				for i := 0; m.Offset <= len(wrap(m.Output, m.Width)); i++ {
					if i > 100 {
						t.Fatal("unbounded readiness view")
					}
					pages += terminalBounds(t, m)
					m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
				}
				for _, text := range []string{id, state, "agentConnected", "agentResponsive", "observedAt", "fixture-observation-" + state} {
					if !strings.Contains(strings.ReplaceAll(pages, "\n", ""), text) {
						t.Fatal("readiness evidence inaccessible at 80x24", text)
					}
				}
				for _, invented := range []string{"applicationReady", "guestBootVerified", "operationID", "planDigest", "ipAddress"} {
					if strings.Contains(m.Output, invented) {
						t.Fatal("invented readiness claim", invented)
					}
				}
				m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
				if m.Confirm || c.calls != 1 || o.calls != 1 {
					t.Fatal("readiness allowed hidden job authority")
				}
			})
		}
	}
}

func TestGuestReadinessTUIErrorsClearObservationAndApproval(t *testing.T) {
	for _, fault := range []string{"native", "canceled", "transport"} {
		t.Run(fault, func(t *testing.T) {
			o := &readinessTUIObserver{}
			c := &recoveryTUIServiceClient{service: app.Service{Provider: o}}
			want := "OPERATION_FAILED"
			switch fault {
			case "native":
				want = "SOURCE_CHANGED"
				o.err = domain.Fail(want, "VM runtime identity changed")
			case "canceled":
				c.cancelCall = true
			case "transport":
				want = "readiness transport canceled"
				c.transportError = errors.New(want)
			}
			m := New(c, "qemu:///session")
			p := coldTUIPlan(t, "create")
			m.Plan = &p
			m.Output = "old observation " + p.Digest
			m = readinessTUIOpen(t, m)
			m = coldTUIType(t, m, "12345678-1234-1234-1234-123456789abc")
			m = coldTUISubmit(t, m)
			if !strings.Contains(m.Output, want) || strings.Contains(m.Output, p.Digest) || m.Plan != nil || m.Confirm || m.Busy {
				t.Fatal("readiness failure hid cause or retained approval", m.Output)
			}
			if fault != "transport" {
				var r app.Response
				if err := json.Unmarshal([]byte(m.Output), &r); err != nil || r.Error == nil {
					t.Fatal("invalid error envelope", err)
				}
				if r.Data != nil {
					allocationTUIEmptyErrorData[domain.GuestReadiness](t, r.Data)
				}
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			if m.Confirm || c.calls != 1 || c.request.Apply != nil || c.request.Action != "" {
				t.Fatal("readiness failure acquired authority")
			}
		})
	}
}
