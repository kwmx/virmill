package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

// This fixture models the provider's full observed-VM fingerprint and a single
// autostart effect. It verifies shared service/durable-job behavior, not libvirt,
// actual host boot or guest power behavior (which need separate native evidence).
type autostartContractProvider struct {
	*configProvider
	actions []string
	inputs  []map[string]any
	reads   int
}

func (p *autostartContractProvider) Get(ctx context.Context, _, _ string) (domain.VM, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	if p.fail == "readback-error" && p.calls > 0 {
		return domain.VM{}, errors.New("synthetic autostart readback unavailable")
	}
	v := p.vm
	v.Fingerprint = ""
	raw, err := json.Marshal(v)
	if err != nil {
		return domain.VM{}, err
	}
	v.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(raw))
	return v, ctx.Err()
}

func (p *autostartContractProvider) Execute(ctx context.Context, uri, id, action string, input map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.actions = append(p.actions, action)
	p.inputs = append(p.inputs, maps.Clone(input))
	enabled, ok := input["enabled"].(bool)
	if action != "autostart" || !ok || uri != p.vm.Key.ConnectionID || id != p.vm.Key.UUID {
		return errors.New("unexpected synthetic autostart effect")
	}
	if p.fail != "unchanged-readback" {
		p.vm.Autostart = enabled
	}
	if p.fail == "lost-ack" {
		return errors.New("synthetic lost autostart acknowledgement")
	}
	return ctx.Err()
}

func autostartContractService(t *testing.T) (*Service, *autostartContractProvider) {
	t.Helper()
	s, base, _ := configService(t)
	base.vm.Key = domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: "12345678-1234-4234-8234-123456789abc"}
	p := &autostartContractProvider{configProvider: base}
	s.Provider = p
	return s, p
}

func autostartRequest(p *autostartContractProvider, enabled bool) Request {
	return Request{Connection: p.vm.Key.ConnectionID, ID: p.vm.Key.UUID, Action: "autostart", Input: map[string]any{"enabled": enabled}}
}

func TestAutostartContractRejectsNonBooleanAndExtraInputBeforePlan(t *testing.T) {
	for _, input := range []map[string]any{
		nil, {}, {"enabled": nil}, {"enabled": "true"}, {"enabled": "false"},
		{"enabled": float64(1)}, {"enabled": float64(0)}, {"enabled": []bool{true}},
		{"enabled": map[string]any{"value": true}}, {"enabled": true, "start": true},
	} {
		s, p := autostartContractService(t)
		r := autostartRequest(p, true)
		r.Input = input
		plan, err := s.planVM(context.Background(), 1000, r)
		var failure *domain.Error
		if !errors.As(err, &failure) || failure.Code != "INVALID_INPUT" || plan.ID != "" {
			t.Fatalf("invalid autostart input acquired a review: %#v, %v", input, err)
		}
		var count int
		if err := s.Engine.Store.DB.QueryRow("SELECT COUNT(*) FROM plans").Scan(&count); err != nil || count != 0 || p.calls != 0 {
			t.Fatal("invalid input was journaled or executed", count, p.calls, err)
		}
	}
}

func TestAutostartContractReviewApplyAndSameValuePreservePowerState(t *testing.T) {
	for _, state := range []string{"stopped", "running", "paused"} {
		for _, beforeEnabled := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%t-to-%t", state, beforeEnabled, enabled), func(t *testing.T) {
					s, p := autostartContractService(t)
					p.vm.State, p.vm.Autostart = state, beforeEnabled
					if state != "stopped" {
						p.vm.LiveXML = p.vm.PersistentXML
					}
					before, _ := p.Get(context.Background(), "", "")
					plan, err := s.planVM(context.Background(), 1000, autostartRequest(p, enabled))
					if err != nil {
						t.Fatal(err)
					}
					if p.calls != 0 || plan.Operation != "vm.autostart" || plan.Before[before.Key.String()] != before.Fingerprint || !reflect.DeepEqual(plan.ResourceIDs, []string{before.Key.String()}) || plan.Review["beforeAutostart"] != beforeEnabled || plan.Review["afterAutostart"] != enabled || plan.Review["persistentEdit"] != true || plan.Review["immediatePowerChange"] != false {
						t.Fatal("review lost exact target or before/after setting", plan)
					}
					_, raw, err := s.Engine.Store.Plan(plan.ID)
					if err != nil {
						t.Fatal(err)
					}
					var durable map[string]any
					if err := json.Unmarshal(raw, &durable); err != nil || durable["enabled"] != enabled || durable["vmID"] != before.Key.UUID {
						t.Fatal("durable recipe changed boolean or target", durable, err)
					}
					job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
					if job.State != "succeeded" {
						t.Fatal(job)
					}
					after, _ := p.Get(context.Background(), "", "")
					p.mu.Lock()
					calls, actions := p.calls, append([]string{}, p.actions...)
					p.mu.Unlock()
					if calls != 1 || !reflect.DeepEqual(actions, []string{"autostart"}) || after.Autostart != enabled || (beforeEnabled != enabled && before.Fingerprint == after.Fingerprint) || (beforeEnabled == enabled && before.Fingerprint != after.Fingerprint) {
						t.Fatal("wrong effect, readback, or fingerprint", calls, actions, before, after)
					}
					before.Autostart, after.Autostart = false, false
					before.Fingerprint, after.Fingerprint = "", ""
					if !reflect.DeepEqual(before, after) {
						t.Fatal("autostart changed power, XML, identity or other observed configuration", before, after)
					}
				})
			}
		}
	}
}

func TestAutostartContractStaleReviewAndApplyRejectExternalChanges(t *testing.T) {
	for _, change := range []string{"autostart", "power", "configuration", "identity", "transient"} {
		t.Run(change, func(t *testing.T) {
			s, p := autostartContractService(t)
			plan, err := s.planVM(context.Background(), 1000, autostartRequest(p, true))
			if err != nil {
				t.Fatal(err)
			}
			_, raw, err := s.Engine.Store.Plan(plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			switch change {
			case "autostart":
				p.vm.Autostart = true
			case "power":
				p.vm.State = "running"
			case "configuration":
				p.vm.PersistentXML += "<!-- external edit -->"
			case "identity":
				p.vm.Key.UUID = domain.ID()
			case "transient":
				p.vm.PersistentXML = ""
			}
			p.mu.Unlock()
			_, err = (&vmHandler{s: s, action: "autostart"}).Review(context.Background(), plan, raw)
			var failure *domain.Error
			if !errors.As(err, &failure) || failure.Code != "STALE_PLAN" {
				t.Fatal("review rebound to changed observation", err)
			}
			_, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: domain.ID(), Acknowledgements: plan.Acknowledgements})
			if !errors.As(err, &failure) || failure.Code != "STALE_PLAN" || p.calls != 0 {
				t.Fatal("stale autostart plan executed", err, p.calls)
			}
			jobs, err := s.Engine.Store.Jobs()
			if err != nil || len(jobs) != 0 {
				t.Fatal("stale plan acquired a job", jobs, err)
			}
		})
	}
}

func TestAutostartContractTransientAndMalformedDurableInputRefused(t *testing.T) {
	s, p := autostartContractService(t)
	p.vm.PersistentXML = ""
	if _, err := s.planVM(context.Background(), 1000, autostartRequest(p, true)); err == nil || p.calls != 0 {
		t.Fatal("transient VM acquired an autostart plan", err)
	}
	p.vm.PersistentXML = "<domain/>"
	plan, err := s.planVM(context.Background(), 1000, autostartRequest(p, true))
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []any{nil, "false", float64(0)} {
		raw, _ := json.Marshal(map[string]any{"vmID": p.vm.Key.UUID, "enabled": enabled})
		err := (&vmHandler{s: s, action: "autostart"}).Validate(context.Background(), plan, raw)
		var failure *domain.Error
		if !errors.As(err, &failure) || failure.Code != "INVALID_INPUT" || p.calls != 0 {
			t.Fatal("malformed durable enabled value reached execution", enabled, err)
		}
	}
}

func TestAutostartContractReadbackFailureAndLostAckNeverReplay(t *testing.T) {
	for _, fault := range []string{"unchanged-readback", "readback-error", "lost-ack"} {
		t.Run(fault, func(t *testing.T) {
			s, p := autostartContractService(t)
			p.fail = fault
			plan, err := s.planVM(context.Background(), 1000, autostartRequest(p, true))
			if err != nil {
				t.Fatal(err)
			}
			job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
			if job.State != "recovery-required" {
				t.Fatal("unproven effect claimed success", job)
			}
			job, err = s.Engine.Reconcile(context.Background(), job.ID)
			if fault == "lost-ack" {
				if err != nil || job.State != "succeeded" {
					t.Fatal("observed original effect did not reconcile", job, err)
				}
			} else if err == nil || job.State != "recovery-required" {
				t.Fatal("unconfirmed readback released recovery", job, err)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.calls != 1 || !reflect.DeepEqual(p.actions, []string{"autostart"}) || p.vm.State != "stopped" {
				t.Fatal("recovery replayed autostart or changed power", p.calls, p.actions, p.vm.State)
			}
		})
	}
}
