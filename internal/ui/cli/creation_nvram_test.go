//go:build linux && amd64

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

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

func TestCreationNVRAMCLIReviewAndAuthorization(t *testing.T) {
	for _, format := range []string{"table", "json", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			c, b := creationNVRAMFixture(t)
			response, output, err := creationEstimateCLI(t, c, context.Background(), "vm", "create", "prepared-fixture", "--input", creationNVRAMInput, "--output", format)
			if err != nil {
				t.Fatal(err)
			}
			p := creationEstimatePlan(t, response)
			if p.Review["nvramDeclarationVersion"] != float64(1) || p.Review["nvramInitializationVerified"] != false || !strings.Contains(output, "historical first assignment") || !strings.Contains(output, "fresh auxiliary-state initialization remain unverified") {
				t.Fatal("review omitted declaration version or initialization limitation", output)
			}
			if format == "ndjson" && strings.Count(output, "\n") != 1 {
				t.Fatal("NDJSON review is not a single clean record")
			}
			response, _, err = creationEstimateCLI(t, c, context.Background(), "plan", "show", p.ID, "--output", format)
			shown := creationEstimatePlan(t, response)
			if err != nil || !reflect.DeepEqual(shown, p) {
				t.Fatal("plan show changed reviewed declaration or authorization", err)
			}
			for _, invalid := range []string{"digest", "firmware-ack", "preflight"} {
				args := []string{"plan", "apply", shown.ID, "--digest", shown.Digest, "--idempotency-key", "nvram-ui-" + invalid, "--output", format}
				wantCode := "UNSUPPORTED_CAPABILITY"
				if invalid == "digest" {
					args[4], wantCode = strings.Repeat("0", 64), "STALE_PLAN"
				} else if invalid == "firmware-ack" {
					wantCode = "PERMISSION_REQUIRED"
				}
				for _, ack := range shown.Acknowledgements {
					if invalid != "firmware-ack" || ack != "new-firmware-state" {
						args = append(args, "--ack", ack)
					}
				}
				c.backend.err = domain.Fail("UNSUPPORTED_CAPABILITY", "fixture refuses apply before mutation")
				response, _, err = creationEstimateCLI(t, c, context.Background(), args...)
				if err == nil || response.Error == nil || response.Error.Code != wantCode {
					t.Fatal("authorization failure was lost", invalid, response, err)
				}
			}
			apply := c.requests[len(c.requests)-1].Apply
			acks := append([]string{}, shown.Acknowledgements...)
			sort.Strings(acks)
			if apply == nil || apply.PlanID != p.ID || apply.PlanDigest != p.Digest || !reflect.DeepEqual(apply.Acknowledgements, acks) || apply.IdempotencyKey != "nvram-ui-preflight" || !strings.Contains(strings.Join(acks, ","), "new-firmware-state") {
				t.Fatal("CLI did not reuse exact UEFI review authorization", apply)
			}
			jobs, err := c.service.Engine.Store.Jobs()
			if err != nil || len(jobs) != 0 || b.mutations != 0 || b.inspections != 0 || !reflect.DeepEqual(c.methods, []string{"vm.create", "plan.show", "operation.apply", "operation.apply", "operation.apply"}) {
				t.Fatal("preview/failed authorization performed creation", err, c.methods)
			}
		})
	}
}

func TestCreationNVRAMCLIResultStages(t *testing.T) {
	for _, format := range []string{"table", "json", "ndjson"} {
		for _, scenario := range []string{"bound", "bound-running", "pending", "pending-partial", "legacy-succeeded", "legacy-pending", "missing-binding", "conflicting-binding", "failed-bound"} {
			t.Run(format+"/"+scenario, func(t *testing.T) {
				c, b := creationNVRAMFixture(t)
				response, _, err := creationEstimateCLI(t, c, context.Background(), "vm", "create", "prepared-fixture", "--input", creationNVRAMInput, "--output", format)
				if err != nil {
					t.Fatal(err)
				}
				p, j, path := creationNVRAMJournal(t, c, creationEstimatePlan(t, response), scenario)
				declaration, err := c.service.Engine.Store.MetadataBytes("creation-nvram-declaration", p.ID)
				if err != nil {
					t.Fatal(err)
				}
				receipt, err := c.service.Engine.Store.MetadataBytes("vm-creation", p.ID)
				if err != nil {
					t.Fatal(err)
				}
				response, output, err := creationEstimateCLI(t, c, context.Background(), "vm", "creation", "result", j.ID, "--output", format)
				failure := scenario == "missing-binding" || scenario == "conflicting-binding" || scenario == "failed-bound"
				if (err != nil) != failure || (response.Error != nil) != failure || (failure && (response.Error.Code != "RECOVERY_REQUIRED" || domain.ExitCode(err) == 0)) {
					t.Fatal("CLI did not preserve the shared result error", response, err)
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
					t.Fatal("UI conflated definition/declaration/initialization or guest stages", output)
				}
				if (result.Declaration != nil) != bound {
					t.Fatal("UI omitted or fabricated declaration", output)
				}
				if bound && (result.Declaration.Firmware.NVRAM == nil || result.Declaration.Firmware.NVRAM.Path != path || result.Declaration.PlanID != p.ID || result.Declaration.InputDigest != p.InputDigest || result.Declaration.OperationID != j.ID || result.Declaration.Resource != (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: p.ConnectionID, Kind: "vm", UUID: strings.TrimSuffix(strings.TrimPrefix(path, "/fixture/nvram/"), "_VARS.fd")}) || result.Declaration.ObservedFingerprint != strings.Repeat("e", 64)) {
					t.Fatal("UI changed bound declaration identity or exact canonical path", output)
				}
				if format == "ndjson" && strings.Count(output, "\n") != 1 {
					t.Fatal("NDJSON result is not one complete envelope")
				}
				creationNVRAMUnchanged(t, c, b, p, j, declaration, receipt)
				if !reflect.DeepEqual(c.methods, []string{"vm.create", "vm.creation.result"}) {
					t.Fatal("result action submitted unintended apply", c.methods)
				}
			})
		}
	}
}

func TestCreationNVRAMCLIFailedObservationDiscardsPriorSuccess(t *testing.T) {
	for _, failure := range []string{"cancellation", "transport", "missing-job"} {
		t.Run(failure, func(t *testing.T) {
			c, b := creationNVRAMFixture(t)
			response, _, err := creationEstimateCLI(t, c, context.Background(), "vm", "create", "prepared-fixture", "--input", creationNVRAMInput, "--output", "json")
			if err != nil {
				t.Fatal(err)
			}
			p, j, path := creationNVRAMJournal(t, c, creationEstimatePlan(t, response), "bound")
			response, _, err = creationEstimateCLI(t, c, context.Background(), "vm", "creation", "result", j.ID, "--output", "json")
			if err != nil || !creationNVRAMReadResult(t, response).DeclarationBound {
				t.Fatal("missing prior successful observation", err)
			}
			declaration, _ := c.service.Engine.Store.MetadataBytes("creation-nvram-declaration", p.ID)
			receipt, _ := c.service.Engine.Store.MetadataBytes("vm-creation", p.ID)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id := j.ID
			if failure == "cancellation" {
				cancel()
			} else if failure == "transport" {
				c.transport = true // Returns prior successful payload with an error.
			} else {
				id = "missing-job"
			}
			response, output, err := creationEstimateCLI(t, c, ctx, "vm", "creation", "result", id, "--output", "json")
			if err == nil || response.Error == nil || domain.ExitCode(err) == 0 || response.Data != nil || strings.Contains(output, path) || strings.Contains(output, "declaration-bound") {
				t.Fatal("failed observation leaked stale declaration success", response, err)
			}
			creationNVRAMUnchanged(t, c, b, p, j, declaration, receipt)
		})
	}
}

func TestCreationNVRAMCLINewUEFIRequiresInspectionCapability(t *testing.T) {
	c := creationEstimateFixture(t) // Deliberately lacks ColdStateInspector.
	response, output, err := creationEstimateCLI(t, c, context.Background(), "vm", "create", "prepared-fixture", "--input", creationNVRAMInput, "--output", "json")
	if err == nil || response.Error == nil || response.Error.Code != "UNSUPPORTED_CAPABILITY" || strings.Contains(output, `"nvramDeclarationVersion":1`) {
		t.Fatal("unsupported new UEFI preview appeared bound or applicable", response, err)
	}
	jobs, err := c.service.Engine.Store.Jobs()
	if err != nil || len(jobs) != 0 || c.backend.mutations != 0 || !reflect.DeepEqual(c.methods, []string{"vm.create"}) {
		t.Fatal("unsupported UEFI preview submitted creation", err, c.methods)
	}
}
