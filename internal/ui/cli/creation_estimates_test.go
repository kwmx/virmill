//go:build linux && amd64

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

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
	last      app.Response
}

func (c *creationEstimateClient) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	c.methods, c.requests = append(c.methods, method), append(c.requests, r)
	if c.transport {
		return c.last, errors.New("fixture transport interrupted")
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

func creationEstimateCLI(t *testing.T, c *creationEstimateClient, ctx context.Context, args ...string) (app.Response, string, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	command := New(c, &out, &stderr)
	command.SetArgs(append(args, "--connection", "qemu:///session", "--non-interactive"))
	err := command.ExecuteContext(ctx)
	var response app.Response
	if e := wire.Decode(out.Bytes(), &response); e != nil {
		t.Fatalf("CLI response is not a single clean JSON envelope: %v; output=%q stderr=%q", e, out.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatal("machine output leaked diagnostics", stderr.String())
	}
	return response, out.String(), err
}

func creationEstimatePlan(t *testing.T, response app.Response) domain.Plan {
	t.Helper()
	b, err := json.Marshal(response.Data)
	if err != nil {
		t.Fatal(err)
	}
	var p domain.Plan
	if err := wire.Decode(b, &p); err != nil || response.Error != nil || p.ID == "" || p.Digest == "" {
		t.Fatalf("missing successful shared plan: %v %+v", err, response)
	}
	return p
}

func TestCreationEstimateCLIShowsAndReusesPersistedReview(t *testing.T) {
	for _, output := range []string{"table", "json", "ndjson"} {
		t.Run(output, func(t *testing.T) {
			client := creationEstimateFixture(t)
			response, text, err := creationEstimateCLI(t, client, context.Background(), "vm", "create", "prepared-fixture", "--input", creationEstimateInput, "--output", output)
			if err != nil {
				t.Fatal(err)
			}
			p := creationEstimatePlan(t, response)
			if p.Estimates.AdditionalBytes != 125829120 || p.Review["requiredFreeBytes"] != float64(125829120) || !strings.Contains(text, p.Estimates.Notes) || !strings.Contains(text, "Copied volume payload: 65536 bytes") || !strings.Contains(text, "Initial physical allocation") {
				t.Fatal("CLI omitted shared pool budget or its interpretation", text)
			}
			if output == "ndjson" && strings.Count(text, "\n") != 1 {
				t.Fatal("NDJSON plan is not one complete record")
			}
			response, _, err = creationEstimateCLI(t, client, context.Background(), "plan", "show", p.ID, "--output", output)
			if err != nil {
				t.Fatal(err)
			}
			shown := creationEstimatePlan(t, response)
			stored, _, err := client.service.Engine.Store.Plan(p.ID)
			if err != nil || shown.Digest != p.Digest || stored.Digest != p.Digest || shown.Estimates != p.Estimates || stored.Estimates != p.Estimates {
				t.Fatal("plan show changed the reviewed estimate or digest", err)
			}
			// Correct authorization reaches shared validation; the fixture refuses
			// preflight so no allocation or background job can occur.
			client.backend.err = domain.Fail("UNSUPPORTED_CAPABILITY", "fixture apply preflight refused")
			args := []string{"plan", "apply", shown.ID, "--digest", shown.Digest, "--idempotency-key", "estimate-ui-apply", "--output", output}
			for _, ack := range shown.Acknowledgements {
				args = append(args, "--ack", ack)
			}
			response, _, err = creationEstimateCLI(t, client, context.Background(), args...)
			apply := client.requests[len(client.requests)-1].Apply
			wantAcks := append([]string{}, stored.Acknowledgements...)
			sort.Strings(wantAcks)
			if err == nil || response.Error == nil || response.Error.Code != "UNSUPPORTED_CAPABILITY" || apply == nil || apply.PlanID != stored.ID || apply.PlanDigest != stored.Digest || !reflect.DeepEqual(apply.Acknowledgements, wantAcks) {
				t.Fatal("CLI did not submit the exact reviewed digest and acknowledgements", response, apply, err)
			}
			jobs, err := client.service.Engine.Store.Jobs()
			if err != nil || len(jobs) != 0 || client.backend.mutations != 0 || !reflect.DeepEqual(client.methods, []string{"vm.create", "plan.show", "operation.apply"}) {
				t.Fatal("preview/error path authorized a mutation", err, client.methods)
			}
		})
	}
}

func TestCreationEstimateCLIFailuresDoNotPrintSuccessfulEstimate(t *testing.T) {
	for _, mode := range []string{"preflight-error", "canceled-creation", "canceled-plan-show", "transport-error", "missing-plan"} {
		t.Run(mode, func(t *testing.T) {
			client := creationEstimateFixture(t)
			response, _, err := creationEstimateCLI(t, client, context.Background(), "vm", "create", "prepared-fixture", "--input", creationEstimateInput, "--output", "json")
			if err != nil {
				t.Fatal(err)
			}
			p := creationEstimatePlan(t, response)
			args := []string{"vm", "create", "prepared-fixture", "--input", creationEstimateInput, "--output", "json"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "preflight-error":
				client.backend.err = domain.Fail("UNSUPPORTED_CAPABILITY", "fixture hardware unavailable")
			case "canceled-creation":
				cancel()
			case "canceled-plan-show":
				cancel()
				args = []string{"plan", "show", p.ID, "--output", "json"}
			case "transport-error":
				client.transport = true
			case "missing-plan":
				args = []string{"plan", "show", "absent-plan", "--output", "json"}
			}
			response, text, err := creationEstimateCLI(t, client, ctx, args...)
			if err == nil || response.Error == nil || domain.ExitCode(err) == 0 || strings.Contains(text, "125829120") || strings.Contains(text, p.Estimates.Notes) {
				t.Fatal("failure retained successful estimates", response, err)
			}
			if mode == "canceled-plan-show" && response.Data != nil {
				t.Fatal("canceled plan read retained a success payload")
			}
			for _, method := range client.methods {
				if method == "operation.apply" {
					t.Fatal("failed preview submitted apply")
				}
			}
			jobs, err := client.service.Engine.Store.Jobs()
			if err != nil || len(jobs) != 0 || client.backend.mutations != 0 {
				t.Fatal("failed view created a mutation", err)
			}
		})
	}
}
