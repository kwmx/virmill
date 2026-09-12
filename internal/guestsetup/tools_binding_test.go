package guestsetup

import (
	"context"
	"strings"
	"testing"
	"virmill.local/core/internal/backend/guestssh"
	"virmill.local/core/internal/domain"
)

type connectingToolsTransport struct {
	Transport
	provider *recipeProvider
	apply    string
}

func (t connectingToolsTransport) Run(ctx context.Context, target guestssh.Target, script guestssh.Script) (guestssh.Result, error) {
	r, err := t.Transport.Run(ctx, target, script)
	if err == nil && string(script.Content) == t.apply {
		t.provider.vm.LiveXML = strings.Replace(t.provider.vm.LiveXML, "state='disconnected'", "state='connected'", 1)
		t.provider.vm.Fingerprint = strings.Repeat("f", 64)
	}
	return r, err
}

func TestToolsConnectionTransitionCompletesDurableVerification(t *testing.T) {
	h := newRecipeHarness(t)
	h.transport.check = 3
	recipe, _ := builtinToolsRecipe("linux-auto", false)
	h.service.Transport = connectingToolsTransport{&toolsFixtureTransport{base: h.transport, recipe: recipe}, h.provider, recipe.Spec.Apply}
	r := h.app.Call(context.Background(), 1000, "guest.tools.install", toolsRequest(h))
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	p := r.Data.(domain.Plan)
	j := h.apply(t, p)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	if strings.Join(h.transport.calls, ",") != "readiness,check,apply,verify" {
		t.Fatal(h.transport.calls)
	}
}

func TestToolsBindingRetainsV1AndAllOtherVMChanges(t *testing.T) {
	h := newRecipeHarness(t)
	r := h.app.Call(context.Background(), 1000, "guest.tools.install", toolsRequest(h))
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	p := r.Data.(domain.Plan)
	_, raw, err := h.db.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	in, err := decodeInput(p, raw)
	if err != nil || in.Version != 2 {
		t.Fatal(in, err)
	}
	old := in
	old.Version = 1
	old.ToolsFingerprint = ""
	legacy, err := h.service.Engine.Plan(context.Background(), 1000, p.ConnectionID, operation, p.ResourceIDs, p.Before, old, p.Steps, p.Acknowledgements, p.Risks)
	if err != nil {
		t.Fatal(err)
	}
	_, oldRaw, err := h.db.Plan(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.service.Validate(context.Background(), legacy, oldRaw); err != nil {
		t.Fatal(err)
	}
	original := h.provider.vm
	h.provider.vm.LiveXML = strings.Replace(original.LiveXML, "disconnected", "connected", 1)
	h.provider.vm.Fingerprint = strings.Repeat("e", 64)
	if err := h.service.Validate(context.Background(), p, raw); err != nil {
		t.Fatal(err)
	}
	if err := h.service.Validate(context.Background(), legacy, oldRaw); err == nil {
		t.Fatal("legacy plan silently gained new comparison semantics")
	}
	for name, change := range map[string]func(*domain.VM){
		"runtime ID":      func(v *domain.VM) { v.LiveXML = strings.Replace(v.LiveXML, "id='7'", "id='8'", 1) },
		"persistent XML":  func(v *domain.VM) { v.PersistentXML = "<domain changed='yes'/>" },
		"autostart":       func(v *domain.VM) { v.Autostart = true },
		"name":            func(v *domain.VM) { v.Name = "another" },
		"missing channel": func(v *domain.VM) { v.LiveXML = "<domain id='7'><devices/></domain>" },
	} {
		t.Run(name, func(t *testing.T) {
			h.provider.vm = original
			change(&h.provider.vm)
			if err := h.service.Validate(context.Background(), p, raw); err == nil {
				t.Fatal("unreviewed change accepted")
			}
		})
	}
}
