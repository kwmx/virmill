package validation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"virmill.local/core/internal/app/lab"
	"virmill.local/core/internal/validation"
)

// These inputs come from the preserved specification, independently of the
// implementation's examples. The existing example glob misses these two files.
func TestSpecificationPreviouslyUnvisitedExamples(t *testing.T) {
	t.Run("root_backup_policy", func(t *testing.T) {
		value, _, err := validation.Document(specificationExample(t, "backup-policy.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if value["kind"] != "BackupPolicy" {
			t.Fatal("fixture no longer represents a backup policy")
		}
		// This is schema validation, not schedule validation or backup execution.
	})
	t.Run("plugin_manifest", func(t *testing.T) {
		if err := validation.Schema("plugin-manifest", specificationExample(t, "plugins/vm-summary/manifest.json")); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSpecificationLabSchemaRejectsUnknownField(t *testing.T) {
	v, _ := specificationLab(t)
	v["spec"].(map[string]any)["unexpected"] = true
	if _, _, err := validation.Document(specificationJSON(t, v)); err == nil {
		t.Fatal("accepted normative unknown-field negative")
	}
}

// Schema validation deliberately does not resolve references or graph
// dependencies. Exercise the public pure semantic validator used immediately
// after Document by document.validate/lab.validate, without a provider or store.
func TestSpecificationLabSemanticNegatives(t *testing.T) {
	_, raw := specificationLab(t)
	if report, err := lab.Validate(raw); err != nil || !report.Valid || report.HostPreflight != "not-run" {
		t.Fatalf("valid specification lab: report=%+v error=%v", report, err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"unresolved_network", func(spec map[string]any) {
			machines := spec["machines"].([]any)
			machines[0].(map[string]any)["spec"].(map[string]any)["nics"].([]any)[1].(map[string]any)["networkRef"] = "absent"
		}},
		{"dependency_cycle", func(spec map[string]any) {
			machines := spec["machines"].([]any)
			machines[0].(map[string]any)["dependsOn"] = []any{map[string]any{"machine": "target-server", "ready": "running", "timeoutSeconds": 10}}
		}},
		{"duplicate_machine_identity", func(spec map[string]any) {
			machines := spec["machines"].([]any)
			machines[1].(map[string]any)["id"] = "workstation"
		}},
		{"invalid_cidr", func(spec map[string]any) {
			network := spec["networks"].([]any)[1].(map[string]any)
			network["spec"].(map[string]any)["ipv4"].(map[string]any)["cidr"] = "not-a-cidr"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, _ := specificationLab(t)
			tc.mutate(v["spec"].(map[string]any))
			_, raw, err := validation.Document(specificationJSON(t, v))
			if err != nil {
				t.Fatalf("semantic fixture failed before its semantic boundary: %v", err)
			}
			if report, err := lab.Validate(raw); err == nil || report.Valid {
				t.Fatalf("accepted normative semantic negative: report=%+v error=%v", report, err)
			}
		})
	}
}

func TestSpecificationPluginManifestNegatives(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"entrypoint_traversal", func(v map[string]any) {
			v["entrypoints"].(map[string]any)["linux/amd64"].(map[string]any)["path"] = "../escape"
		}},
		{"unimplemented_protocol_version", func(v map[string]any) {
			v["protocol"].(map[string]any)["maxVersion"] = "2.0"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var v map[string]any
			if err := json.Unmarshal(specificationExample(t, "plugins/vm-summary/manifest.json"), &v); err != nil {
				t.Fatal(err)
			}
			tc.mutate(v)
			if err := validation.Schema("plugin-manifest", specificationJSON(t, v)); err == nil {
				t.Fatal("accepted normative manifest negative")
			}
		})
	}
}

func TestSpecificationRPCEnvelopeAlternatives(t *testing.T) {
	// Null results and a null error ID are valid envelope shapes. They must not
	// be confused with missing fields when refusing the two normative negatives.
	for _, valid := range []string{
		`{"jsonrpc":"2.0","id":"h-1","method":"ping"}`,
		`{"jsonrpc":"2.0","id":"p-1","result":null}`,
		`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}`,
		`{"jsonrpc":"2.0","method":"progress","params":{}}`,
	} {
		if err := validation.Schema("plugin-rpc-envelope", []byte(valid)); err != nil {
			t.Fatalf("valid RPC shape %s: %v", valid, err)
		}
	}
	for _, tc := range []struct{ name, raw string }{
		{"both_result_and_error", `{"jsonrpc":"2.0","id":"h-1","result":{},"error":{"code":-32010,"message":"x"}}`},
		{"numeric_identifier", `{"jsonrpc":"2.0","id":1,"method":"ping"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validation.Schema("plugin-rpc-envelope", []byte(tc.raw)); err == nil {
				t.Fatal("accepted normative RPC negative")
			}
		})
	}
}

func TestSpecificationPlanDateTimeFormats(t *testing.T) {
	// validate_package.py explicitly enables FormatChecker. A schema-level
	// result must enforce date-time even before any typed domain.Plan decoder.
	var plan map[string]any
	if err := json.Unmarshal([]byte(`{
		"apiVersion":"virmill/v1","planID":"format-parity",
		"createdAt":"2026-09-08T01:02:03Z","expiresAt":"2026-09-08T01:07:03Z",
		"actorUID":1000,"connectionID":"qemu:///system","operation":"vm.start",
		"resourceIDs":[],"beforeFingerprints":{},"requiredGrants":[],
		"acknowledgements":[],"risks":[],
		"steps":[{"id":"start","action":"vm.start","preconditions":[],
		"idempotency":"reconcile-before-retry","compensation":"none",
		"reconciliation":"observe","completionPredicate":"running"}],
		"estimates":{"additionalBytes":0,"requiresDowntime":false}
	}`), &plan); err != nil {
		t.Fatal(err)
	}
	plan["inputDigest"], plan["planDigest"] = strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, valid := range []string{"2026-09-08T01:02:03Z", "2026-09-08T04:02:03+03:00", "2026-09-07T19:32:03-05:30", "2026-09-08T01:02:03.123Z"} {
		plan["createdAt"], plan["expiresAt"] = valid, valid
		if err := validation.Schema("operation-plan", specificationJSON(t, plan)); err != nil {
			t.Fatalf("valid timestamp %s: %v", valid, err)
		}
	}
	for _, field := range []string{"createdAt", "expiresAt"} {
		for _, invalid := range []string{"not-a-date", "2026-02-30T01:02:03Z", "2026-09-08T25:02:03Z", "2026-09-08T01:02:03", "2026-09-08T01:02:03+24:00"} {
			t.Run(field+"/"+invalid, func(t *testing.T) {
				previous := plan[field]
				plan[field] = invalid
				defer func() { plan[field] = previous }()
				if err := validation.Schema("operation-plan", specificationJSON(t, plan)); err == nil {
					t.Fatal("accepted invalid date-time required by normative FormatChecker")
				}
			})
		}
	}
}

func specificationExample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../../virmill-v1-spec/examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func specificationLab(t *testing.T) (map[string]any, []byte) {
	t.Helper()
	v, raw, err := validation.Document(specificationExample(t, "labs/multi-network-lab.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return v, raw
}

func specificationJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
