package app

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
)

func inlineNetworkDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := validation.Document(raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func normalizedInlineNetworkPlan(t *testing.T, s *Service, p domain.Plan) (string, string) {
	t.Helper()
	stored, raw, err := s.Engine.Store.Plan(p.ID)
	storedJSON, _ := json.Marshal(stored)
	originalJSON, _ := json.Marshal(p)
	var storedValue, originalValue any
	json.Unmarshal(storedJSON, &storedValue)
	json.Unmarshal(originalJSON, &originalValue)
	if err != nil || !reflect.DeepEqual(storedValue, originalValue) {
		t.Fatal("plan readback changed", err)
	}
	if digest, err := operations.PlanDigest(p); err != nil || digest != p.Digest {
		t.Fatal("plan digest does not bind the review", err)
	}
	recipe, err := parseNetworkRecipe(p, raw)
	if err != nil {
		t.Fatal(err)
	}
	xml, err := networkxml.Render(recipe.Definition)
	if err != nil || p.Review["networkXML"] != xml {
		t.Fatal("review XML differs from durable definition", err)
	}
	normalized := recipe.Definition
	normalized.UUID = "12345678-1234-4234-8234-123456789abc"
	normalized.Name, normalized.Bridge = "virmill-"+normalized.UUID, "vm123456781234"
	normalizedXML, err := networkxml.Render(normalized)
	if err != nil {
		t.Fatal(err)
	}
	p.Review = maps.Clone(p.Review)
	p.Review["networkXML"] = normalizedXML
	// Only generated identity and timestamps differ. All policy, metadata,
	// allocation observations, XML, grants and durable steps stay comparable.
	replace := strings.NewReplacer(recipe.Definition.Name, "NETWORK_NAME", recipe.Definition.UUID, "NETWORK_UUID", recipe.Definition.Bridge, "BRIDGE")
	p.ID, p.Digest, p.InputDigest = "", "", ""
	p.CreatedAt, p.ExpiresAt = time.Time{}, time.Time{}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return replace.Replace(string(encoded)), replace.Replace(string(raw))
}

// NET-01/03/04/06 and UX-01/02: this verifies declaration/planning parity only.
// Native packets, routes, DHCP/DNS and isolation require separate live evidence.
func TestInlineNetworkMatchesFileReviewAndDurableRecipe(t *testing.T) {
	for _, profile := range []string{"legacy-nat", "legacy-lab", "nat", "lab", "guest-only", "addressless", "auto"} {
		t.Run(profile, func(t *testing.T) {
			s, provider, _ := networkService(t)
			var path string
			switch profile {
			case "legacy-nat", "legacy-lab":
				path = networkDocument(t, strings.TrimPrefix(profile, "legacy-"), "allow")
			case "addressless":
				path = protectedNetworkDocument(t, "guest-only", false, false)
			case "auto":
				path = autoNetworkDocument(t, "lab")
			default:
				path = protectedNetworkDocument(t, profile, profile != "guest-only", true)
			}
			// Empty input and absent input have identical wire semantics; a
			// file request must continue accepting either representation.
			request := Request{Connection: "qemu:///system", Action: "create", Path: path, Input: map[string]any{}}
			filePlan, err := s.planNetworkCreation(context.Background(), 1000, request)
			if err != nil {
				t.Fatal(err)
			}
			request.Path, request.Input = "", map[string]any{"document": inlineNetworkDocument(t, path)}
			inlinePlan, err := s.planNetworkCreation(context.Background(), 1000, request)
			if err != nil {
				t.Fatal("inline declaration refused", err)
			}
			fileReview, fileRecipe := normalizedInlineNetworkPlan(t, s, filePlan)
			inlineReview, inlineRecipe := normalizedInlineNetworkPlan(t, s, inlinePlan)
			if fileReview != inlineReview || fileRecipe != inlineRecipe {
				t.Fatalf("inline changed semantic review or recipe\nfile: %s\ninline: %s\nfile recipe: %s\ninline recipe: %s", fileReview, inlineReview, fileRecipe, inlineRecipe)
			}
			jobs, err := s.Engine.Store.Jobs()
			if err != nil || len(jobs) != 0 || provider.definitions != 0 || provider.activations != 0 || s.NetworkFirewall.(*networkFirewallFixture).applications != 0 {
				t.Fatal("preview executed a mutation", jobs, err)
			}
			records, err := s.Engine.Store.MetadataRecords()
			if err != nil || len(records) != 0 {
				t.Fatal("preview reserved resources", records, err)
			}
		})
	}
}

type inlineNetworkObservationCounter struct {
	*networkCreationFixture
	reads, checks int
}

func (p *inlineNetworkObservationCounter) ListNetworks(ctx context.Context, uri string) ([]domain.VirtualNetwork, error) {
	p.reads++
	return p.networkCreationFixture.ListNetworks(ctx, uri)
}
func (p *inlineNetworkObservationCounter) CheckNetworkCreation(ctx context.Context, uri string, definition domain.NetworkDefinition) error {
	p.checks++
	return p.networkCreationFixture.CheckNetworkCreation(ctx, uri, definition)
}

func TestInlineNetworkRejectsInvalidInputBeforeProviderEffects(t *testing.T) {
	for _, fault := range []string{"missing", "empty-input", "empty-document", "null-document", "string-document", "array-document", "mixed", "unknown-input", "wrong-kind", "wrong-version", "missing-spec", "unknown-spec", "extension", "invalid-policy", "session", "unexpected-id", "apply"} {
		t.Run(fault, func(t *testing.T) {
			s, native, _ := networkService(t)
			provider := &inlineNetworkObservationCounter{networkCreationFixture: native}
			s.Provider = provider
			hostReads := 0
			s.HostPrefixes = func(context.Context) (domain.HostNetworkPrefixes, error) {
				hostReads++
				return domain.HostNetworkPrefixes{}, nil
			}
			path := protectedNetworkDocument(t, "lab", true, true)
			doc := inlineNetworkDocument(t, path)
			r := Request{Connection: "qemu:///system", Action: "create", Input: map[string]any{"document": doc}}
			switch fault {
			case "missing":
				r.Input = nil
			case "empty-input":
				r.Input = map[string]any{}
			case "empty-document":
				r.Input["document"] = map[string]any{}
			case "null-document":
				r.Input["document"] = nil
			case "string-document":
				r.Input["document"] = path
			case "array-document":
				r.Input["document"] = []any{doc}
			case "mixed":
				r.Path = path
			case "unknown-input":
				r.Input["hostAccess"] = "allow"
			case "wrong-kind":
				doc["kind"] = "VM"
			case "wrong-version":
				doc["apiVersion"] = "virmill/v2"
			case "missing-spec":
				delete(doc, "spec")
			case "unknown-spec":
				doc["spec"].(map[string]any)["unknown"] = true
			case "extension":
				doc["spec"].(map[string]any)["extensions"] = map[string]any{"example.test/policy": map[string]any{"enabled": true}}
			case "invalid-policy":
				doc["spec"].(map[string]any)["egress"] = "any"
			case "session":
				r.Connection = "qemu:///session"
			case "unexpected-id":
				r.ID = domain.ID()
			case "apply":
				r.Apply = &operations.ApplyRequest{}
			}
			response := s.Call(context.Background(), 1000, "network.create", r)
			p, _ := response.Data.(domain.Plan)
			if response.Error == nil || p.ID != "" {
				t.Fatal("invalid input acquired a plan", response)
			}
			jobs, err := s.Engine.Store.Jobs()
			if err != nil || len(jobs) != 0 || provider.reads != 0 || provider.checks != 0 || hostReads != 0 || native.definitions != 0 || native.activations != 0 || s.NetworkFirewall.(*networkFirewallFixture).applications != 0 {
				t.Fatal("invalid input reached provider or durable effects", jobs, err, provider.reads, provider.checks, hostReads)
			}
			records, err := s.Engine.Store.MetadataRecords()
			if err != nil || len(records) != 0 {
				t.Fatal("invalid input left reservations", records, err)
			}
		})
	}
}
