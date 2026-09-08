package guestsetup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/guestssh"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

type recipeProvider struct {
	domain.ComputeProvider
	vm domain.VM
}

func (p *recipeProvider) Get(ctx context.Context, uri, id string) (domain.VM, error) {
	if uri != p.vm.Key.ConnectionID || id != p.vm.Key.UUID {
		return domain.VM{}, errors.New("unexpected guest")
	}
	return p.vm, ctx.Err()
}

type recipeTransport struct {
	mu       sync.Mutex
	db       *store.Store
	identity guestssh.Identity
	files    guestssh.TargetIdentity
	calls    []string
	check    int
	fail     string
}

func (t *recipeTransport) Identity(ctx context.Context) (guestssh.Identity, error) {
	return t.identity, ctx.Err()
}
func (t *recipeTransport) InspectTarget(ctx context.Context, _ guestssh.Target) (guestssh.TargetIdentity, error) {
	return t.files, ctx.Err()
}
func (t *recipeTransport) Run(ctx context.Context, target guestssh.Target, script guestssh.Script) (guestssh.Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	name := string(script.Content)
	if name == readiness {
		name = "readiness"
	}
	job := operations.OperationID(ctx)
	if job == "" {
		return guestssh.Result{}, errors.New("missing job")
	}
	var record stageRecord
	if err := t.db.Get(stageKind, job+":"+name, &record); err != nil || record.Receipt != nil || !record.Intent.RunPlanned {
		return guestssh.Result{}, errors.New("stage executed without exclusive durable intent")
	}
	if script.SSHSHA256 != t.identity.SHA256 {
		return guestssh.Result{}, errors.New("SSH executable not bound")
	}
	if target.IdentitySHA256 != t.files.IdentitySHA256 || target.KnownHostsSHA256 != t.files.KnownHostsSHA256 {
		return guestssh.Result{}, errors.New("target not bound")
	}
	t.calls = append(t.calls, name)
	if t.fail == name+"-lost" {
		return guestssh.Result{}, errors.New("sensitive partial output must be withheld")
	}
	if t.fail == name+"-cancel" {
		<-ctx.Done()
		return guestssh.Result{}, ctx.Err()
	}
	r := guestssh.Result{Stdout: []byte("sensitive guest stdout"), Stderr: []byte("sensitive guest stderr")}
	if name == "readiness" {
		r.Stdout = []byte("1000\n")
		r.Stderr = nil
		if t.fail == "root" {
			r.Stdout = []byte("0\n")
		}
	}
	if name == "check" {
		r.ExitCode = t.check
	}
	if t.fail == name+"-exit" {
		r.ExitCode = 7
	}
	return r, ctx.Err()
}

type recipeHarness struct {
	app       *app.Service
	service   *Service
	provider  *recipeProvider
	transport *recipeTransport
	request   app.Request
	db        *store.Store
}

func newRecipeHarness(t *testing.T) *recipeHarness {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := operations.New(db)
	p := &recipeProvider{vm: domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: "11111111-2222-4333-8444-555555555555"}, State: "running", PersistentXML: "<domain/>", Fingerprint: strings.Repeat("a", 64)}}
	transport := &recipeTransport{db: db, identity: guestssh.Identity{Path: "/usr/bin/ssh", SHA256: strings.Repeat("b", 64), Version: "OpenSSH_fixture"}, files: guestssh.TargetIdentity{IdentitySHA256: strings.Repeat("c", 64), KnownHostsSHA256: strings.Repeat("d", 64)}}
	a := app.New(p, e)
	s := &Service{Engine: e, Provider: p, Transport: transport}
	s.Register(a)
	t.Cleanup(func() { e.Close(); db.Close() })
	raw, err := os.ReadFile("../../examples/guest-recipes/posix-readiness.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := DecodeRecipe(raw)
	if err != nil {
		t.Fatal(err)
	}
	r.Spec.Check = "check"
	r.Spec.Apply = "apply"
	r.Spec.Verify = "verify"
	r.Spec.TimeoutSeconds = 1
	raw, _ = json.Marshal(r)
	path := filepath.Join(root, "recipe.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	q := app.Request{Connection: "qemu:///system", ID: p.vm.Key.UUID, Path: path, Action: "run", Input: map[string]any{"address": "192.0.2.42", "port": uint16(2222), "user": "guest", "identityFile": "/private/identity", "knownHostsFile": "/private/known-hosts", "arguments": []string{"literal $(touch unwanted)", "apostrophe'argument"}}}
	return &recipeHarness{a, s, p, transport, q, db}
}
func (h *recipeHarness) plan(t *testing.T) domain.Plan {
	t.Helper()
	r := h.app.Call(context.Background(), 1000, operation, h.request)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	return r.Data.(domain.Plan)
}
func (h *recipeHarness) apply(t *testing.T, p domain.Plan) domain.Job {
	t.Helper()
	j, err := h.app.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err = h.db.Job(j.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch j.State {
		case "succeeded", "failed", "recovery-required", "interrupted", "canceled", "partial":
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish", j)
	return j
}
func TestGuestRecipeStageWorkflowAndNoRawOutputPersistence(t *testing.T) {
	for _, check := range []int{0, 3} {
		t.Run(string(rune('0'+check)), func(t *testing.T) {
			h := newRecipeHarness(t)
			h.transport.check = check
			p := h.plan(t)
			if len(h.transport.calls) != 0 {
				t.Fatal("planning connected to guest")
			}
			// Editing the source recipe cannot change the frozen approved program.
			if err := os.WriteFile(h.request.Path, []byte("invalid changed recipe"), 0600); err != nil {
				t.Fatal(err)
			}
			j := h.apply(t, p)
			if j.State != "succeeded" {
				t.Fatal(j)
			}
			want := []string{"readiness", "check", "verify"}
			if check == 3 {
				want = []string{"readiness", "check", "apply", "verify"}
			}
			if !reflect.DeepEqual(h.transport.calls, want) {
				t.Fatal(h.transport.calls)
			}
			wrong := h.app.Call(context.Background(), 1000, "guest.recipe.result", app.Request{Connection: "qemu:///session", ID: j.ID})
			if wrong.Error == nil || wrong.Error.Code != "PERMISSION_DENIED" {
				t.Fatalf("cross-connection result: %+v", wrong)
			}
			r := h.app.Call(context.Background(), 1000, "guest.recipe.result", app.Request{Connection: "qemu:///system", ID: j.ID})
			if r.Error != nil {
				t.Fatal(r.Error)
			}
			result := r.Data.(Result)
			if !result.Complete || result.RawOutputRetained || result.NativeAddressBindingVerified {
				t.Fatal(result)
			}
			records, err := h.db.Metadata(stageKind)
			if err != nil {
				t.Fatal(err)
			}
			for _, raw := range records {
				if strings.Contains(string(raw), "sensitive guest") || strings.Contains(string(raw), "literal $(") {
					t.Fatal("raw output/arguments leaked into stage receipt")
				}
			}
		})
	}
}
func TestGuestRecipeFailureAndLostResponseNeverReplays(t *testing.T) {
	for _, fault := range []string{"root", "readiness-lost", "check-exit", "apply-exit", "apply-lost", "verify-exit", "verify-cancel"} {
		t.Run(fault, func(t *testing.T) {
			h := newRecipeHarness(t)
			h.transport.check = 3
			h.transport.fail = fault
			p := h.plan(t)
			j := h.apply(t, p)
			if j.State == "succeeded" {
				t.Fatal("failed stage became complete")
			}
			before := append([]string(nil), h.transport.calls...)
			_, _ = h.app.Engine.Reconcile(context.Background(), j.ID)
			if !reflect.DeepEqual(h.transport.calls, before) {
				t.Fatal("uncertain guest command replayed")
			}
			r := h.app.Call(context.Background(), 1000, "guest.recipe.result", app.Request{Connection: "qemu:///system", ID: j.ID})
			if r.Error == nil && r.Data.(Result).Complete {
				t.Fatal("failure result claims completion")
			}
			if fault == "root" && len(before) != 1 {
				t.Fatal("recipe ran as root")
			}
		})
	}
}
func TestGuestRecipeStaleNativeOrCredentialRefusedBeforeExecution(t *testing.T) {
	for _, fault := range []string{"native", "key", "tool"} {
		t.Run(fault, func(t *testing.T) {
			h := newRecipeHarness(t)
			p := h.plan(t)
			switch fault {
			case "native":
				h.provider.vm.Fingerprint = strings.Repeat("e", 64)
			case "key":
				h.transport.files.IdentitySHA256 = strings.Repeat("e", 64)
			case "tool":
				h.transport.identity.SHA256 = strings.Repeat("e", 64)
			}
			_, err := h.app.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
			if err == nil || len(h.transport.calls) != 0 {
				t.Fatal("stale authority reached SSH", err)
			}
		})
	}
}
