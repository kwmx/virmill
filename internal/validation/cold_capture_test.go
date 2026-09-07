package validation

import (
	"encoding/json"
	"os"
	"testing"
)

func TestColdRecoveryPointSchemaIsOfflineStrictAndExplicit(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/protection/capture-manifest/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := Schema("cold-recovery-point", data); err != nil {
		t.Fatal("bundled fixture no longer matches its schema", err)
	}
	for _, key := range []string{"apiVersion", "kind", "version", "hasManagedSave", "secrets", "independentlyRecoverable", "auxiliaryInventoryMember"} {
		for _, replaceWithNull := range []bool{false, true} {
			var value map[string]any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if replaceWithNull {
				value[key] = nil
			} else {
				delete(value, key)
			}
			invalid, _ := json.Marshal(value)
			if err := Schema("cold-recovery-point", invalid); err == nil {
				t.Fatal("required declaration became an inferred zero value", key, replaceWithNull)
			}
		}
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	value["source"].(map[string]any)["disks"].([]any)[0].(map[string]any)["unknownSecret"] = "must not be accepted"
	invalid, _ := json.Marshal(value)
	if err := Schema("cold-recovery-point", invalid); err == nil {
		t.Fatal("unknown nested capture fields accepted")
	}
}
