//go:build linux && amd64

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/creating"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/importing"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/wire"
)

// This fixture injects only source/backend observations. Plan construction,
// estimation, persistence and application-service dispatch are production code.
type creationEstimateBackend struct {
	domain.ComputeProvider
	domain.CreationBackend
	domain.ResourceInventory
	err       error
	mutations int
}

func (b *creationEstimateBackend) PreflightCreation(ctx context.Context, _ string, s domain.CreationSpec) (domain.CreationTarget, error) {
	if err := ctx.Err(); err != nil {
		return domain.CreationTarget{}, err
	}
	if b.err != nil {
		return domain.CreationTarget{}, b.err
	}
	return domain.CreationTarget{Spec: s, PoolName: "fixture", PoolFingerprint: "fixture", CapabilitiesDigest: "fixture", Networks: []domain.VirtualNetwork{}}, nil
}
func (b *creationEstimateBackend) GetStoragePool(context.Context, string, string) (domain.StoragePool, error) {
	free := uint64(1 << 30)
	return domain.StoragePool{Active: true, State: "running", AvailableBytes: &free}, nil
}
func (b *creationEstimateBackend) VolumeAbsent(context.Context, string, domain.VolumeIntent) error {
	return nil
}
func (b *creationEstimateBackend) AllocateVolume(context.Context, string, domain.VolumeIntent) (domain.CreatedVolume, error) {
	b.mutations++
	return domain.CreatedVolume{}, errors.New("unexpected fixture allocation")
}
func (b *creationEstimateBackend) PopulateVolume(context.Context, string, domain.CreatedVolume, io.Reader) error {
	b.mutations++
	return errors.New("unexpected fixture upload")
}
func (b *creationEstimateBackend) DefineCreatedVM(context.Context, string, domain.CreationTarget, []domain.CreatedVolume, string) (domain.VM, error) {
	b.mutations++
	return domain.VM{}, errors.New("unexpected fixture definition")
}

type creationEstimateClient struct {
	service   *app.Service
	backend   *creationEstimateBackend
	methods   []string
	requests  []app.Request
	transport bool
	canceled  bool
	last      app.Response
}

func (c *creationEstimateClient) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	c.methods, c.requests = append(c.methods, method), append(c.requests, r)
	if c.transport {
		return c.last, errors.New("fixture transport interrupted")
	}
	if c.canceled {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		ctx = canceled
	}
	c.last = c.service.Call(ctx, 1000, method, r)
	return c.last, nil
}

const creationEstimateInput = `{"identityMode":"clone","hardware":{"name":"Creation estimate UI fixture","poolID":"11111111-1111-4111-8111-111111111111","architecture":"x86_64","machine":"pc-q35-10.2","vcpus":1,"memoryMiB":512,"cpu":{"mode":"host-passthrough"},"firmware":{"mode":"bios","secureBoot":false,"tpm":false},"clock":"utc","graphics":"none","disks":[{"sourceID":"boot","bus":"virtio","bootOrder":1}],"nics":[]}}`

func creationEstimateFixture(t *testing.T) *creationEstimateClient {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "state", "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	backend := &creationEstimateBackend{}
	service := app.New(backend, engine)
	creating.Register(service, "")
	dir := t.TempDir()
	service.Engine.Handlers["vm.create"].(*creating.Service).LoadSource = func(ctx context.Context, _ *store.Store, _ uint32, _ string) (importing.Artifact, string, error) {
		return importing.Artifact{APIVersion: domain.APIVersion, Kind: "PreparedDiskSet", InputDigest: strings.Repeat("a", 64), SourceSHA256: strings.Repeat("b", 64), Disks: []importing.PreparedDisk{{SourceID: "boot", Path: "boot.qcow2", Format: "qcow2", VirtualBytes: 32 << 20, FileBytes: 65536, SHA256: strings.Repeat("c", 64)}}}, dir, ctx.Err()
	}
	t.Cleanup(func() { engine.Close(); db.Close() })
	return &creationEstimateClient{service: service, backend: backend}
}

func creationEstimateTUIAction(t *testing.T, m Model, command, input string) Model {
	t.Helper()
	found := false
	for section := range sections {
		m.Section = section
		for selected, action := range m.actions() {
			if action.Command == command {
				m.Selected, found = selected, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("TUI command is not reachable", command)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.Editing || cmd != nil {
		t.Fatal("action did not open input form")
	}
	m.Input = input
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || !m.Busy {
		t.Fatal("form did not dispatch")
	}
	next, _ = m.Update(cmd())
	return next.(Model)
}

func creationEstimateTUIPlan(t *testing.T, m Model) domain.Plan {
	t.Helper()
	var response struct {
		Data  domain.Plan   `json:"data"`
		Error *domain.Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(m.Output), &response); err != nil || response.Error != nil || m.Plan == nil || response.Data.Digest != m.Plan.Digest {
		t.Fatalf("TUI did not retain the shared review: %v %s", err, m.Output)
	}
	return response.Data
}

func TestCreationEstimateTUIShowsPagedSharedReviewAndReusesDigest(t *testing.T) {
	client := creationEstimateFixture(t)
	m := creationEstimateTUIAction(t, New(client, "qemu:///session"), "vm create", `{"id":"prepared-fixture","input":`+creationEstimateInput+`}`)
	p := creationEstimateTUIPlan(t, m)
	// A 64 MiB reserve plus the 64 KiB prepared disk and its 16 MiB headroom (ADR 0060).
	if p.Estimates.AdditionalBytes != 83951616 || p.Review["requiredFreeBytes"] != float64(83951616) || p.Estimates.RequiresDowntime {
		t.Fatal("TUI changed shared estimate", p.Estimates)
	}
	// At the default 80x24 size, every wrapped detail must be reachable using
	// actual page-down key updates, including the long explanatory notes.
	pages := ""
	for m.Offset <= len(wrap(m.Output, m.Width)) {
		pages += m.View()
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = next.(Model)
		if cmd != nil {
			t.Fatal("paging dispatched a request")
		}
	}
	for _, text := range []string{`"additionalBytes": 83951616`, `"requiredFreeBytes": 83951616`, "Target-pool disk/media free-space budget", "Copied volume payload: 65536 bytes", "Initial physical allocation", "elapsed time is not estimated"} {
		// Terminal wrapping can split a phrase across adjacent rows.
		if !strings.Contains(strings.ReplaceAll(pages, "\n", ""), text) {
			t.Fatalf("80x24 paging hid estimate detail %q", text)
		}
	}
	m.Plan, m.Confirm = nil, false
	m = creationEstimateTUIAction(t, m, "plan show", p.ID)
	shown := creationEstimateTUIPlan(t, m)
	stored, _, err := client.service.Engine.Store.Plan(p.ID)
	if err != nil || shown.Digest != p.Digest || stored.Digest != p.Digest || shown.Estimates != p.Estimates || stored.Estimates != p.Estimates {
		t.Fatal("TUI plan retrieval changed estimate or digest", err)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if cmd != nil || !m.Confirm || !strings.Contains(m.View(), shown.Digest) {
		t.Fatal("review did not require the shared digest")
	}
	m.Input = "wrong-digest"
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || len(client.methods) != 2 {
		t.Fatal("wrong digest submitted authorization")
	}
	client.backend.err = domain.Fail("UNSUPPORTED_CAPABILITY", "fixture apply preflight refused")
	m.Input = shown.Digest
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("exact digest did not submit reviewed request")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	apply := client.requests[len(client.requests)-1].Apply
	wantAcks := append([]string{}, stored.Acknowledgements...)
	sort.Strings(wantAcks)
	if apply == nil || apply.PlanID != stored.ID || apply.PlanDigest != stored.Digest || !reflect.DeepEqual(apply.Acknowledgements, wantAcks) || m.Plan != nil || m.Confirm || !strings.Contains(m.Output, "fixture apply preflight refused") {
		t.Fatal("TUI lost the reviewed authorization or retained approval after error")
	}
	jobs, err := client.service.Engine.Store.Jobs()
	if err != nil || len(jobs) != 0 || client.backend.mutations != 0 || !reflect.DeepEqual(client.methods, []string{"vm.create", "plan.show", "operation.apply"}) {
		t.Fatal("TUI preview/error caused mutation", err)
	}
}

func TestCreationEstimateTUIFailuresClearStaleBudgetAndApproval(t *testing.T) {
	for _, mode := range []string{"preflight-error", "canceled-creation", "canceled-plan-show", "transport-error", "missing-plan"} {
		t.Run(mode, func(t *testing.T) {
			client := creationEstimateFixture(t)
			m := creationEstimateTUIAction(t, New(client, "qemu:///session"), "vm create", `{"id":"prepared-fixture","input":`+creationEstimateInput+`}`)
			p := creationEstimateTUIPlan(t, m)
			command, input := "vm create", `{"id":"prepared-fixture","input":`+creationEstimateInput+`}`
			switch mode {
			case "preflight-error":
				client.backend.err = domain.Fail("UNSUPPORTED_CAPABILITY", "fixture hardware unavailable")
			case "canceled-creation":
				client.canceled = true
			case "canceled-plan-show":
				client.canceled = true
				command, input = "plan show", p.ID
			case "transport-error":
				client.transport = true
			case "missing-plan":
				command, input = "plan show", "absent-plan"
			}
			m.Offset = 50
			m = creationEstimateTUIAction(t, m, command, input)
			if m.Plan != nil || m.Confirm || m.Busy || m.Offset != 0 || strings.Contains(m.Output, "83951616") || strings.Contains(m.Output, p.Estimates.Notes) || strings.Contains(m.View(), "Plan is a preview") {
				t.Fatal("failed request retained successful estimate or approval", m.Output)
			}
			if mode != "transport-error" {
				var response app.Response
				if err := wire.Decode([]byte(m.Output), &response); err != nil || response.Error == nil {
					t.Fatal("service failure is not visible", err, m.Output)
				}
				if mode == "canceled-plan-show" && response.Data != nil {
					t.Fatal("canceled read retained successful plan")
				}
			}
			before := len(client.methods)
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			m = next.(Model)
			if cmd != nil || m.Confirm || len(client.methods) != before {
				t.Fatal("failed estimate still authorizes mutation")
			}
			jobs, err := client.service.Engine.Store.Jobs()
			if err != nil || len(jobs) != 0 || client.backend.mutations != 0 {
				t.Fatal("failed view created mutation", err)
			}
		})
	}
}

func TestCreationEstimateTUIEscapeCancelsAuthorizationWithoutSubmission(t *testing.T) {
	client := creationEstimateFixture(t)
	m := creationEstimateTUIAction(t, New(client, "qemu:///session"), "vm create", `{"id":"prepared-fixture","input":`+creationEstimateInput+`}`)
	creationEstimateTUIPlan(t, m)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if !m.Confirm {
		t.Fatal("confirmation not entered")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if cmd != nil || m.Confirm || m.Editing || len(client.methods) != 1 || client.backend.mutations != 0 {
		t.Fatal("Esc submitted creation authorization")
	}
}
