//go:build linux && amd64

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/creating"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

const creationNVRAMInput = `{"identityMode":"clone","hardware":{"name":"NVRAM declaration UI fixture","poolID":"11111111-1111-4111-8111-111111111111","architecture":"x86_64","machine":"pc-q35-10.2","vcpus":1,"memoryMiB":512,"cpu":{"mode":"host-passthrough"},"firmware":{"mode":"uefi","code":"/fixture/OVMF_CODE.fd","template":"/fixture/OVMF_VARS.fd","format":"raw","secureBoot":false,"tpm":false},"clock":"utc","graphics":"none","disks":[{"sourceID":"boot","bus":"virtio","bootOrder":1}],"nics":[]}}`

type creationNVRAMBackend struct {
	*creationEstimateBackend
	inspections int
}

func (b *creationNVRAMBackend) PreflightCreation(ctx context.Context, uri string, spec domain.CreationSpec) (domain.CreationTarget, error) {
	target, err := b.creationEstimateBackend.PreflightCreation(ctx, uri, spec)
	target.FirmwareDigest = strings.Repeat("d", 64)
	return target, err
}

func (b *creationNVRAMBackend) InspectColdState(context.Context, string, string) (domain.ColdStateInspection, error) {
	b.inspections++
	return domain.ColdStateInspection{}, errors.New("UI observation must not run native declaration inspection")
}

// Use the real shared service and SQLite store, injecting only observations.
// Result fixtures below seed journal records; they do not execute creation or
// certify that a native declaration/file was bound, initialized or captured.
func creationNVRAMFixture(t *testing.T) (*creationEstimateClient, *creationNVRAMBackend) {
	t.Helper()
	c := creationEstimateFixture(t)
	loader := c.service.Engine.Handlers["vm.create"].(*creating.Service).LoadSource
	b := &creationNVRAMBackend{creationEstimateBackend: c.backend}
	c.service = app.New(b, c.service.Engine)
	creating.Register(c.service, "")
	c.service.Engine.Handlers["vm.create"].(*creating.Service).LoadSource = loader
	return c, b
}

type creationNVRAMResult struct {
	Operation             domain.Job `json:"operation"`
	Complete              bool       `json:"complete"`
	DeclarationBound      bool       `json:"nvramDeclarationBound"`
	InitializationChecked bool       `json:"nvramInitializationVerified"`
	Status                string     `json:"nvramDeclarationStatus"`
	GuestBootVerified     bool       `json:"guestBootVerified"`
	SetupVerified         bool       `json:"setupVerified"`
	ConnectivityVerified  bool       `json:"connectivityVerified"`
	Declaration           *struct {
		PlanID              string              `json:"planID"`
		InputDigest         string              `json:"inputDigest"`
		OperationID         string              `json:"operationID"`
		Resource            domain.ResourceKey  `json:"resource"`
		ObservedFingerprint string              `json:"observedFingerprint"`
		Firmware            domain.ColdFirmware `json:"firmware"`
	} `json:"nvramDeclaration"`
}

// The fieldless legacy recipe is submitted to the public engine as a distinct
// historical fixture. No existing plan or digest is rewritten/upgraded.
func creationNVRAMJournal(t *testing.T, c *creationEstimateClient, p domain.Plan, scenario string) (domain.Plan, domain.Job, string) {
	t.Helper()
	db := c.service.Engine.Store
	_, encoded, err := db.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(scenario, "legacy-") {
		var recipe map[string]any
		if err = json.Unmarshal(encoded, &recipe); err != nil {
			t.Fatal(err)
		}
		delete(recipe, "nvramDeclarationVersion")
		p, err = c.service.Engine.Plan(context.Background(), 1000, p.ConnectionID, p.Operation, append([]string{}, p.ResourceIDs...), p.Before, recipe, p.Steps, append([]string{}, p.Acknowledgements...), p.Risks)
		if err != nil || p.Review["nvramDeclarationVersion"] != 0 || p.Review["nvramInitializationVerified"] != false {
			t.Fatal("legacy recipe did not retain unbound review", p, err)
		}
		_, encoded, err = db.Plan(p.ID)
		if err != nil || strings.Contains(string(encoded), "nvramDeclarationVersion") {
			t.Fatal("fieldless historical input was silently upgraded", err)
		}
	}
	var recipe struct {
		Target domain.CreationTarget `json:"target"`
	}
	if err = json.Unmarshal(encoded, &recipe); err != nil {
		t.Fatal(err)
	}
	j, err := db.Accept(p, domain.ID(), "synthetic UI journal fixture")
	if err != nil {
		t.Fatal(err)
	}
	j.State = "succeeded"
	if scenario == "pending" || scenario == "legacy-pending" {
		j.State = "queued"
	} else if scenario == "pending-partial" || scenario == "bound-running" {
		j.State = "verifying"
	} else if scenario == "failed-bound" {
		j.State = "recovery-required"
	}
	if err = db.Update(j, "synthetic UI result state; no native execution"); err != nil {
		t.Fatal(err)
	}
	binding, err := operations.Digest([]string{p.ID, p.InputDigest})
	if err != nil {
		t.Fatal(err)
	}
	path := "/fixture/nvram/" + recipe.Target.Spec.UUID + "_VARS.fd"
	if scenario == "pending" || scenario == "legacy-pending" {
		return p, j, path
	}
	receipt := creating.Receipt{Version: 1, PlanID: p.ID, OperationID: j.ID, Binding: binding, VMID: recipe.Target.Spec.UUID, Connection: p.ConnectionID, VolumesVerified: true, Defined: j.State == "succeeded"}
	if err = db.Put("vm-creation", p.ID, receipt); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(scenario, "legacy-") || scenario == "missing-binding" || scenario == "pending-partial" {
		return p, j, path
	}
	fw := recipe.Target.Spec.Firmware
	declaration := map[string]any{
		"schemaVersion": 1, "planID": p.ID, "inputDigest": p.InputDigest, "operationID": j.ID,
		"resource":        domain.ResourceKey{ProviderID: "libvirt", ConnectionID: p.ConnectionID, Kind: "vm", UUID: receipt.VMID},
		"creationBinding": binding, "firmwareDigest": recipe.Target.FirmwareDigest,
		"firmware":            domain.ColdFirmware{Loader: fw.Code, LoaderType: "pflash", LoaderReadOnly: "yes", LoaderSecure: "no", LoaderFormat: fw.Format, LoaderStateless: "", NVRAM: &domain.ColdNVRAM{Path: path, Format: fw.Format, Template: fw.Template, TemplateFormat: fw.Format}},
		"observedFingerprint": strings.Repeat("e", 64),
	}
	if scenario == "conflicting-binding" {
		declaration["inputDigest"] = strings.Repeat("f", 64)
	}
	if err = db.Put("creation-nvram-declaration", p.ID, declaration); err != nil {
		t.Fatal(err)
	}
	return p, j, path
}

func creationNVRAMReadResult(t *testing.T, response app.Response) creationNVRAMResult {
	t.Helper()
	encoded, err := json.Marshal(response.Data)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"complete", "nvramDeclarationBound", "nvramInitializationVerified", "guestBootVerified", "setupVerified", "connectivityVerified"} {
		if _, ok := fields[field].(bool); !ok {
			t.Fatal("result omitted an explicit stage flag", field, string(encoded))
		}
	}
	var result creationNVRAMResult
	if err = json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func creationNVRAMUnchanged(t *testing.T, c *creationEstimateClient, b *creationNVRAMBackend, p domain.Plan, j domain.Job, declaration, receipt []byte) {
	t.Helper()
	after, err := c.service.Engine.Store.Job(j.ID)
	if err != nil || after != j || b.mutations != 0 || b.inspections != 0 {
		t.Fatal("UI observation changed native state or journal", err, after)
	}
	for kind, want := range map[string][]byte{"creation-nvram-declaration": declaration, "vm-creation": receipt} {
		got, err := c.service.Engine.Store.MetadataBytes(kind, p.ID)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("UI result changed durable evidence", kind, err)
		}
	}
	// Store.Update released successful-job locks while constructing the fixture;
	// active and uncertain jobs retain them before and after the UI read.
	wantOwners := []string{j.ID}
	if j.State == "succeeded" {
		wantOwners = []string{}
	}
	for _, resource := range p.ResourceIDs {
		owners, err := c.service.Engine.Store.ResourceJobs(resource)
		if err != nil || !reflect.DeepEqual(owners, wantOwners) {
			t.Fatal("UI observation changed synthetic fixture locks", owners, err)
		}
	}
}

func creationNVRAMPages(t *testing.T, m Model) string {
	t.Helper()
	pages := ""
	m.Offset = 0
	for m.Offset <= len(wrap(m.Output, m.Width)) {
		pages += m.View()
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = next.(Model)
		if cmd != nil {
			t.Fatal("paging dispatched a request")
		}
	}
	return strings.ReplaceAll(pages, "\n", "")
}

func creationNVRAMTUIResponse(t *testing.T, m Model) app.Response {
	t.Helper()
	var response app.Response
	if err := json.Unmarshal([]byte(m.Output), &response); err != nil {
		t.Fatal("TUI omitted shared result envelope", err, m.Output)
	}
	return response
}

func TestCreationNVRAMTUIReviewAndAuthorization(t *testing.T) {
	c, b := creationNVRAMFixture(t)
	m := creationEstimateTUIAction(t, New(c, "qemu:///session"), "vm create", `{"id":"prepared-fixture","input":`+creationNVRAMInput+`}`)
	p := creationEstimateTUIPlan(t, m)
	if p.Review["nvramDeclarationVersion"] != float64(1) || p.Review["nvramInitializationVerified"] != false {
		t.Fatal("TUI lost the declaration review version or limitation", m.Output)
	}
	pages := creationNVRAMPages(t, m)
	for _, text := range []string{`"nvramDeclarationVersion": 1`, `"nvramInitializationVerified": false`, "historical first assignment", "fresh auxiliary-state initialization remain unverified", "new-firmware-state"} {
		if !strings.Contains(pages, text) {
			t.Fatal("80x24 review hid a binding/initialization distinction", text)
		}
	}
	m = creationEstimateTUIAction(t, m, "plan show", p.ID)
	shown := creationEstimateTUIPlan(t, m)
	if !reflect.DeepEqual(shown, p) {
		t.Fatal("plan show changed UEFI review or digest")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if cmd != nil || !m.Confirm || !strings.Contains(m.View(), shown.Digest) || !strings.Contains(m.View(), "new-firmware-state") {
		t.Fatal("authorization omitted exact digest or firmware acknowledgement")
	}
	m.Input = "wrong-digest"
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || len(c.methods) != 2 {
		t.Fatal("wrong digest submitted creation")
	}
	// Esc abandons authorization without changing the persisted preview.
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if cmd != nil || m.Confirm || len(c.methods) != 2 {
		t.Fatal("canceled authorization dispatched apply")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	m.Input = shown.Digest
	c.backend.err = domain.Fail("UNSUPPORTED_CAPABILITY", "fixture refuses apply before mutation")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("exact digest did not dispatch shared apply")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	apply := c.requests[len(c.requests)-1].Apply
	acks := append([]string{}, shown.Acknowledgements...)
	sort.Strings(acks)
	response := creationNVRAMTUIResponse(t, m)
	if apply == nil || apply.PlanID != p.ID || apply.PlanDigest != p.Digest || !reflect.DeepEqual(apply.Acknowledgements, acks) || apply.IdempotencyKey == "" || response.Error == nil || response.Error.Code != "UNSUPPORTED_CAPABILITY" || m.Plan != nil || m.Confirm {
		t.Fatal("TUI lost exact review authorization or retained failed approval", m.Output, apply)
	}
	jobs, err := c.service.Engine.Store.Jobs()
	if err != nil || len(jobs) != 0 || b.mutations != 0 || b.inspections != 0 || !reflect.DeepEqual(c.methods, []string{"vm.create", "plan.show", "operation.apply"}) {
		t.Fatal("preview/failed authorization performed creation", err, c.methods)
	}
}

func TestCreationNVRAMTUIResultStages(t *testing.T) {
	for _, scenario := range []string{"bound", "bound-running", "pending", "pending-partial", "legacy-succeeded", "legacy-pending", "missing-binding", "conflicting-binding", "failed-bound"} {
		t.Run(scenario, func(t *testing.T) {
			c, b := creationNVRAMFixture(t)
			m := creationEstimateTUIAction(t, New(c, "qemu:///session"), "vm create", `{"id":"prepared-fixture","input":`+creationNVRAMInput+`}`)
			p, j, path := creationNVRAMJournal(t, c, creationEstimateTUIPlan(t, m), scenario)
			declaration, err := c.service.Engine.Store.MetadataBytes("creation-nvram-declaration", p.ID)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := c.service.Engine.Store.MetadataBytes("vm-creation", p.ID)
			if err != nil {
				t.Fatal(err)
			}
			m = creationEstimateTUIAction(t, m, "vm creation result", j.ID)
			response := creationNVRAMTUIResponse(t, m)
			failure := scenario == "missing-binding" || scenario == "conflicting-binding" || scenario == "failed-bound"
			if (response.Error != nil) != failure || (failure && response.Error.Code != "RECOVERY_REQUIRED") {
				t.Fatal("TUI omitted shared result failure", m.Output)
			}
			result := creationNVRAMReadResult(t, response)
			bound := scenario == "bound" || scenario == "bound-running" || scenario == "failed-bound"
			status := "pending"
			if bound {
				status = "declaration-bound"
			} else if strings.HasPrefix(scenario, "legacy-") {
				status = "legacy-unbound"
			}
			if result.Operation.ID != j.ID || result.Operation.State != j.State || result.Complete != (scenario == "bound" || scenario == "legacy-succeeded") || result.Status != status || result.DeclarationBound != bound || result.InitializationChecked || result.GuestBootVerified || result.SetupVerified || result.ConnectivityVerified {
				t.Fatal("TUI conflated definition/declaration/initialization or guest stages", m.Output)
			}
			if (result.Declaration != nil) != bound {
				t.Fatal("TUI omitted or fabricated declaration", m.Output)
			}
			if bound && (result.Declaration.Firmware.NVRAM == nil || result.Declaration.Firmware.NVRAM.Path != path || result.Declaration.PlanID != p.ID || result.Declaration.InputDigest != p.InputDigest || result.Declaration.OperationID != j.ID || result.Declaration.Resource != (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: p.ConnectionID, Kind: "vm", UUID: strings.TrimSuffix(strings.TrimPrefix(path, "/fixture/nvram/"), "_VARS.fd")}) || result.Declaration.ObservedFingerprint != strings.Repeat("e", 64)) {
				t.Fatal("TUI changed declaration identity or exact canonical path", m.Output)
			}
			pages := creationNVRAMPages(t, m)
			for _, text := range []string{`"nvramDeclarationStatus": "` + status + `"`, `"nvramInitializationVerified": false`} {
				if !strings.Contains(pages, text) {
					t.Fatal("80x24 result hid binding status or initialization limitation", text)
				}
			}
			if bound && !strings.Contains(pages, path) {
				t.Fatal("80x24 result hid exact bound path")
			}
			if m.Plan != nil || m.Confirm || m.Busy || m.Editing {
				t.Fatal("result retained stale creation approval")
			}
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			if cmd != nil || next.(Model).Confirm || !reflect.DeepEqual(c.methods, []string{"vm.create", "vm.creation.result"}) {
				t.Fatal("result action submitted unintended apply", c.methods)
			}
			creationNVRAMUnchanged(t, c, b, p, j, declaration, receipt)
		})
	}
}

func TestCreationNVRAMTUIFailedObservationDiscardsPriorSuccess(t *testing.T) {
	for _, failure := range []string{"cancellation", "transport", "missing-job"} {
		t.Run(failure, func(t *testing.T) {
			c, b := creationNVRAMFixture(t)
			m := creationEstimateTUIAction(t, New(c, "qemu:///session"), "vm create", `{"id":"prepared-fixture","input":`+creationNVRAMInput+`}`)
			p, j, path := creationNVRAMJournal(t, c, creationEstimateTUIPlan(t, m), "bound")
			m = creationEstimateTUIAction(t, m, "vm creation result", j.ID)
			if !creationNVRAMReadResult(t, creationNVRAMTUIResponse(t, m)).DeclarationBound {
				t.Fatal("missing prior successful result")
			}
			declaration, _ := c.service.Engine.Store.MetadataBytes("creation-nvram-declaration", p.ID)
			receipt, _ := c.service.Engine.Store.MetadataBytes("vm-creation", p.ID)
			id := j.ID
			if failure == "cancellation" {
				c.canceled = true
			} else if failure == "transport" {
				c.transport = true // Returns the stale successful payload and an error.
			} else {
				id = "missing-job"
			}
			m = creationEstimateTUIAction(t, m, "vm creation result", id)
			if m.Plan != nil || m.Confirm || strings.Contains(m.Output, path) || strings.Contains(m.Output, "declaration-bound") {
				t.Fatal("failed TUI observation retained stale success", m.Output)
			}
			if failure == "transport" {
				if !strings.Contains(m.Output, "fixture transport interrupted") {
					t.Fatal("TUI lost transport failure", m.Output)
				}
			} else if response := creationNVRAMTUIResponse(t, m); response.Error == nil || response.Data != nil {
				t.Fatal("failed TUI observation retained success payload", m.Output)
			}
			creationNVRAMUnchanged(t, c, b, p, j, declaration, receipt)
		})
	}
}

func TestCreationNVRAMTUINewUEFIRequiresInspectionCapability(t *testing.T) {
	c := creationEstimateFixture(t) // Deliberately lacks ColdStateInspector.
	m := creationEstimateTUIAction(t, New(c, "qemu:///session"), "vm create", `{"id":"prepared-fixture","input":`+creationNVRAMInput+`}`)
	response := creationNVRAMTUIResponse(t, m)
	if response.Error == nil || response.Error.Code != "UNSUPPORTED_CAPABILITY" || m.Plan != nil || m.Confirm || strings.Contains(m.Output, `"nvramDeclarationVersion": 1`) {
		t.Fatal("unsupported new UEFI preview retained approval", m.Output)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil || next.(Model).Confirm {
		t.Fatal("failed UEFI preview authorized apply")
	}
	jobs, err := c.service.Engine.Store.Jobs()
	if err != nil || len(jobs) != 0 || c.backend.mutations != 0 || !reflect.DeepEqual(c.methods, []string{"vm.create"}) {
		t.Fatal("unsupported UEFI preview submitted creation", err, c.methods)
	}
}
