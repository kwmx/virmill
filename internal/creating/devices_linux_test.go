//go:build linux && amd64

package creating

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

func TestDevicePolicyIsReviewedAndRequiresAcknowledgement(t *testing.T) {
	s, backend, request, _ := creationFixture(t)
	p, err := s.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	if p.Operation != "vm.create.devices-v1" || !slices.Contains(p.Acknowledgements, "creation-device-policy") {
		t.Fatal("policy missing from authorization", p)
	}
	target := p.Review["target"].(domain.CreationTarget)
	// The balloon is libvirt's own default and is what lets memory change while
	// the VM runs (ADR 0068); the rest stays off unless it is asked for.
	if target.Spec.DevicePolicy == nil || target.Spec.DevicePolicy.WatchdogAction != "none" || target.Spec.DevicePolicy.USBController != "none" || target.Spec.DevicePolicy.MemoryBalloon != "virtio" {
		t.Fatal("implicit unsafe defaults", target)
	}
	acks := slices.DeleteFunc(slices.Clone(p.Acknowledgements), func(v string) bool { return v == "creation-device-policy" })
	_, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: acks})
	if err == nil || backend.allocated != 0 {
		t.Fatal("unacknowledged policy executed", err)
	}
	request.Input["hardware"].(map[string]any)["devicePolicy"] = map[string]any{"version": 1, "chipset": "q35", "pciPlacement": "libvirt-auto", "usbController": "qemu-xhci", "memoryBalloon": "virtio", "watchdogAction": "reset", "input": "ps2", "audio": "none", "serial": "isa-serial"}
	p, err = s.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(p.Acknowledgements, "watchdog-reset") {
		t.Fatal("forceful reset lacks acknowledgement")
	}
	delete(s.Engine.Handlers, "vm.create.devices-v1")
	_, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
	if err == nil || backend.allocated != 0 {
		t.Fatal("older handler registry executed future operation")
	}
}

func TestLegacyCreationRecipeStaysExecutableWithoutNewPolicy(t *testing.T) {
	s, backend, request, _ := creationFixture(t)
	p, err := s.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := s.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var in input
	if err = wire.Decode(data, &in); err != nil {
		t.Fatal(err)
	}
	in.Target.Spec.DevicePolicy = nil
	steps := slices.Clone(p.Steps)
	steps[0].Action = "vm.create"
	legacy, err := s.Engine.Plan(context.Background(), 1000, p.ConnectionID, "vm.create", p.ResourceIDs, p.Before, in, steps, []string{"host-mutation", "copy-managed-volumes", "new-vm-identity", "network-attachment"}, p.Risks)
	if err != nil {
		t.Fatal(err)
	}
	backend.fail = "ack"
	job := awaitCreation(t, s, applyCreation(t, s, legacy).ID)
	if job.State != "recovery-required" {
		t.Fatal(job)
	}
	completed, err := s.Engine.Reconcile(context.Background(), job.ID)
	if err != nil || completed.State != "succeeded" {
		t.Fatal(completed, err)
	}
	if backend.target.Spec.DevicePolicy != nil || backend.definitions != 1 {
		t.Fatal("legacy recipe mutated or replayed")
	}
	oldJSON, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip input
	if err = wire.Decode(oldJSON, &roundTrip); err != nil || roundTrip.Target.Spec.DevicePolicy != nil {
		t.Fatal("legacy wire semantics changed", err)
	}
	for _, mismatch := range []struct {
		op     string
		policy *domain.CreationDevicePolicy
	}{{"vm.create.devices-v1", nil}, {"vm.create", p.Review["target"].(domain.CreationTarget).Spec.DevicePolicy}} {
		in.Target.Spec.DevicePolicy = mismatch.policy
		if err = creationRecipeVersion(domain.Plan{Operation: mismatch.op}, in); err == nil {
			t.Fatal("operation-policy mismatch accepted")
		}
	}
}

func TestDevicePolicyRejectsUnknownAndIncompleteInputs(t *testing.T) {
	for _, policy := range []map[string]any{
		{}, {"version": 2}, {"version": 1, "chipset": "q35", "pciPlacement": "libvirt-auto", "usbController": "none", "memoryBalloon": "none", "watchdogAction": "none", "input": "ps2", "audio": "none", "serial": "isa-serial", "hostdev": "/dev/bus/usb"},
		{"version": 1, "chipset": "i440fx", "pciPlacement": "libvirt-auto", "usbController": "none", "memoryBalloon": "none", "watchdogAction": "none", "input": "ps2", "audio": "none", "serial": "isa-serial"},
	} {
		s, backend, r, _ := creationFixture(t)
		r.Input["hardware"].(map[string]any)["devicePolicy"] = policy
		if _, err := s.Plan(context.Background(), 1000, r); err == nil || backend.allocated != 0 {
			t.Fatal("unreviewable policy accepted", policy, err)
		}
	}
}
