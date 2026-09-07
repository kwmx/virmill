//go:build linux && amd64

package creating

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"virmill.local/core/internal/validation"
)

func TestCreationInputCompatibilityAndExplicitIdentityBoundary(t *testing.T) {
	b, err := os.ReadFile("../../examples/creation/prepared-ova.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = validation.Schema("vm-creation-input", b); err != nil {
		t.Fatal("documented example is not valid", err)
	}
	for _, field := range []string{"unknown", "uuid", "mac", "missing-disks"} {
		var value map[string]any
		if err = json.Unmarshal(b, &value); err != nil {
			t.Fatal(err)
		}
		hw := value["hardware"].(map[string]any)
		switch field {
		case "unknown":
			value["futureOption"] = true
		case "uuid":
			hw["uuid"] = "00000000-0000-4000-8000-000000000099"
		case "mac":
			hw["nics"].([]any)[0].(map[string]any)["mac"] = "02:00:00:00:00:01"
		case "missing-disks":
			delete(hw, "disks")
		}
		changed, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = validation.Schema("vm-creation-input", changed); err == nil {
			t.Fatal("unsupported input silently accepted", field)
		}
	}
}

func TestCleanupInputSchemaCompatibility(t *testing.T) {
	for _, input := range []string{`{"disposition":"retain"}`, `{"disposition":"delete"}`} {
		if err := validation.Schema("vm-creation-cleanup-input", []byte(input)); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{`{}`, `{"disposition":"automatic"}`, `{"disposition":"delete","overwrite":true}`, `{"disposition":"retain","disposition":"delete"}`, `{"disposition":null}`} {
		if err := validation.Schema("vm-creation-cleanup-input", []byte(input)); err == nil {
			t.Fatal("ambiguous/future input accepted", input)
		}
	}
}

func TestCreationReceiptRoundTripAndFutureVersionRefusal(t *testing.T) {
	s, _, request, _ := creationFixture(t)
	p, err := s.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	old, err := s.load(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Receipt
	if err = json.Unmarshal(encoded, &roundTrip); err != nil || !same(old, roundTrip) {
		t.Fatal("version 1 receipt changed", err)
	}
	for _, future := range []bool{false, true} {
		var changed map[string]any
		if err = json.Unmarshal(encoded, &changed); err != nil {
			t.Fatal(err)
		}
		if future {
			changed["schemaVersion"] = 2
		} else {
			changed["futureMeaning"] = true
		}
		if err = s.Store.Put("vm-creation", p.ID, changed); err != nil {
			t.Fatal(err)
		}
		if _, err = s.load(p.ID); err == nil {
			t.Fatal("future receipt silently accepted")
		}
	}
}
