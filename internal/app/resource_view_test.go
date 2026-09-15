package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

func resourceViewFixture(t *testing.T) (*Service, *configProvider, Request) {
	s, p, _ := configService(t)
	p.vm.Key = domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: "570c4866-5b5e-4583-817c-602538d19b9c"}
	return s, p, Request{Connection: p.vm.Key.ConnectionID, ID: p.vm.Key.UUID}
}
func TestResourcesViewCurrentValuesAndReadOnlyContract(t *testing.T) {
	s, p, r := resourceViewFixture(t)
	out := s.Call(context.Background(), 1000, "vm.resources.show", r)
	if out.Error != nil {
		t.Fatal(out.Error)
	}
	v := out.Data.(domain.VMResourceView)
	if v.Resource != p.vm.Key || !v.CanEditCPU || !v.CanEditMemory || v.Live != nil || *v.Persistent.VCPUs != 2 || *v.Persistent.MemoryBytes != 256<<20 || v.RequiresShutdown || len(v.ApplyModes) != 1 || v.ApplyModes[0] != "next-boot" || p.calls != 0 {
		t.Fatal(v)
	}
	b, _ := json.Marshal(v)
	if err := validation.Schema("vm-resources-view", b); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(r)
	if err := validation.Schema("vm-resources-show-request", b); err != nil {
		t.Fatal(err)
	}
	p.vm.State = "running"
	p.vm.LiveXML = strings.ReplaceAll(p.vm.PersistentXML, "262144", "131072")
	p.vm.LiveXML = strings.Replace(p.vm.LiveXML, "<vcpu>2</vcpu>", "<vcpu>1</vcpu>", 1)
	out = s.Call(context.Background(), 1000, "vm.resources.show", r)
	if out.Error != nil {
		t.Fatal(out.Error)
	}
	v = out.Data.(domain.VMResourceView)
	// ADR 0061: a running VM's next-boot values are editable; they apply after shutdown.
	if !v.CanEditCPU || !v.CanEditMemory || !v.RequiresShutdown || *v.Live.VCPUs != 1 || *v.Live.MemoryBytes != 128<<20 || *v.Persistent.VCPUs != 2 {
		t.Fatal(v)
	}
	p.vm.HasManagedSave = true
	out = s.Call(context.Background(), 1000, "vm.resources.show", r)
	v = out.Data.(domain.VMResourceView)
	if v.RequiresShutdown || !strings.Contains(v.CPUReason, "Restore") || p.calls != 0 {
		t.Fatal(v)
	}
}

func TestResourcesViewPreservesIndependentFieldSupport(t *testing.T) {
	s, p, r := resourceViewFixture(t)
	p.vm.PersistentXML = strings.Replace(p.vm.PersistentXML, "</domain>", "<cpu><topology sockets='1' cores='2' threads='1'/></cpu></domain>", 1)
	v, err := s.resourceView(context.Background(), r)
	if err != nil || v.CanEditCPU || !v.CanEditMemory || *v.Persistent.VCPUs != 2 || v.CPUReason == "" {
		t.Fatal(v, err)
	}
	p.vm.PersistentXML = strings.Replace(p.vm.PersistentXML, "<currentMemory unit='KiB'>262144</currentMemory>", "<currentMemory unit='KiB'>131072</currentMemory>", 1)
	v, err = s.resourceView(context.Background(), r)
	if err != nil || v.CanEditMemory || *v.Persistent.MemoryBytes != 128<<20 || *v.Persistent.MaximumMemoryBytes != 256<<20 || v.MemoryReason == "" {
		t.Fatal(v, err)
	}
}

func TestResourcesViewRejectsWrongRequestIdentityAndCancellation(t *testing.T) {
	s, p, r := resourceViewFixture(t)
	for _, change := range []func(*Request){func(q *Request) { q.ID = "name" }, func(q *Request) { q.Connection = "qemu+ssh://elsewhere/system" }, func(q *Request) { q.Input = map[string]any{"vcpus": float64(9)} }, func(q *Request) { q.Action = "set" }, func(q *Request) { q.Path = "file" }, func(q *Request) { q.After = 1 }} {
		q := r
		change(&q)
		if _, err := s.resourceView(context.Background(), q); err == nil {
			t.Fatal("invalid request accepted", q)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.resourceView(ctx, r); err == nil {
		t.Fatal("canceled read accepted")
	}
	p.vm.Key.UUID = "11111111-2222-4333-8444-555555555555"
	if _, err := s.resourceView(context.Background(), r); err == nil {
		t.Fatal("wrong backend identity accepted")
	}
	if p.calls != 0 {
		t.Fatal("read mutated VM")
	}
}

func TestResourcesViewAllowsExplicitTargetsOutsideCurrentValueRange(t *testing.T) {
	s, p, r := resourceViewFixture(t)
	for _, raw := range []string{
		"<domain><vcpu>1024</vcpu><memory unit='MiB'>2097152</memory></domain>",
		"<domain><vcpu>2</vcpu><memory unit='b'>1000000</memory></domain>",
		"<domain><vcpu>2</vcpu><memory unit='MB'>100</memory></domain>",
	} {
		p.vm.PersistentXML = raw
		v, err := s.resourceView(context.Background(), r)
		if err != nil || !v.CanEditCPU || !v.CanEditMemory {
			t.Fatal(v, err)
		}
		if p.vm.PersistentXML != raw || p.calls != 0 {
			t.Fatal("capability probe mutated configuration")
		}
	}
}

func TestResourcesPlanReviewIncludesObservedBeforeAndRequestedAfter(t *testing.T) {
	s, p, r := resourceViewFixture(t)
	r.Action = "set"
	r.Input = map[string]any{"vcpus": float64(4), "memoryMiB": float64(1024), "applyMode": "next-boot"}
	response := s.Call(context.Background(), 1000, "vm.plan", r)
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	plan := response.Data.(domain.Plan)
	before, ok := plan.Review["beforeResources"].(domain.ResourceValues)
	if !ok {
		t.Fatal(plan.Review)
	}
	after, ok := plan.Review["afterResources"].(domain.ResourceValues)
	if !ok {
		t.Fatal(plan.Review)
	}
	if *before.VCPUs != 2 || *before.MemoryBytes != 256<<20 || *after.VCPUs != 4 || *after.MemoryBytes != 1024<<20 || p.calls != 0 {
		t.Fatal(before, after)
	}
	_, raw, err := s.Engine.Store.Plan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.vm.Fingerprint = "changed"
	h := &vmHandler{s: s, action: "set"}
	if _, err := h.Review(context.Background(), plan, raw); err == nil {
		t.Fatal("stale observed values were used for a fresh review")
	}
}
