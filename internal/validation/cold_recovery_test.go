package validation

import (
	"encoding/json"
	"strings"
	"testing"
)

// Input shape validation is offline. These tests do not establish source-root
// ownership, source freshness, capture completeness or native restore support.
func TestColdRecoveryInputSchemas(t *testing.T) {
	const pool = "a349c6aa-42fa-4931-99ab-091c315a7c6e"
	for _, tc := range []struct{ schema, raw string }{
		{"cold-capture-input", `{"sourceRoot":"/home/test/source images"}`},
		{"cold-capture-input", `{"sourceRoot":"/home/test/images","auxiliaryRootID":"aux-state_1.0"}`},
		{"cold-restore-input", `{"name":"restored-guest_1.0","poolID":"` + pool + `"}`},
		{"cold-restore-input", `{"name":"Restored ضيف","poolID":"` + pool + `"}`},
		{"cold-capture-input", `{"sourceRoot":"/home/test/صور ضيف"}`},
	} {
		if err := Schema(tc.schema, []byte(tc.raw)); err != nil {
			t.Fatal("valid input refused", tc.schema, err)
		}
	}
	for _, tc := range []struct{ name, schema, raw string }{
		{"missing-root", "cold-capture-input", `{}`},
		{"null-root", "cold-capture-input", `{"sourceRoot":null}`},
		{"root-type", "cold-capture-input", `{"sourceRoot":[]}`},
		{"unknown-root-field", "cold-capture-input", `{"sourceRoot":"/images","sourceMutation":"allow"}`},
		{"duplicate-root", "cold-capture-input", `{"sourceRoot":"/images","sourceRoot":"/other"}`},
		{"root-case-alias", "cold-capture-input", `{"SourceRoot":"/images"}`},
		{"auxiliary-traversal", "cold-capture-input", `{"sourceRoot":"/images","auxiliaryRootID":"../aux"}`},
		{"auxiliary-empty", "cold-capture-input", `{"sourceRoot":"/images","auxiliaryRootID":""}`},
		{"missing-pool", "cold-restore-input", `{"name":"new-vm"}`},
		{"missing-name", "cold-restore-input", `{"poolID":"` + pool + `"}`},
		{"empty-name", "cold-restore-input", `{"name":"","poolID":"` + pool + `"}`},
		{"null-name", "cold-restore-input", `{"name":null,"poolID":"` + pool + `"}`},
		{"unknown-restore-field", "cold-restore-input", `{"name":"new-vm","poolID":"` + pool + `","disconnectAllNICs":false}`},
		{"duplicate-pool", "cold-restore-input", `{"name":"new-vm","poolID":"` + pool + `","poolID":"` + pool + `"}`},
		{"pool-case-alias", "cold-restore-input", `{"name":"new-vm","PoolID":"` + pool + `"}`},
		{"multiple-documents", "cold-restore-input", `{"name":"new-vm","poolID":"` + pool + `"}{}`},
		{"array", "cold-restore-input", `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Schema(tc.schema, []byte(tc.raw)); err == nil {
				t.Fatal("invalid input accepted", tc.raw)
			}
		})
	}
}

func TestColdRecoveryCaptureRootCanonicalShape(t *testing.T) {
	for _, root := range []string{"", "/", "images", "./images", "/images/", "/images//disk", "/images/./disk", "/images/../disk", " /images", "/images ", "/images\x00disk", "/images\ndisk", "/images\tdisk", "/images\x7fdisk", "/images\u202edisk", "/images\\disk", "/" + strings.Repeat("x", 4096)} {
		t.Run(root, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"sourceRoot": root})
			if err := Schema("cold-capture-input", raw); err == nil {
				t.Fatalf("noncanonical source root accepted: %q", root)
			}
		})
	}
}

func TestColdRecoveryRestorePoolCanonicalIdentity(t *testing.T) {
	for _, id := range []string{"", "pool-name", "../pool", "00000000-0000-0000-0000-000000000000", "A349c6aa-42fa-4931-99ab-091c315a7c6e", "a349c6aa42fa493199ab091c315a7c6e", "a349c6aa-42fa-4931-99ab-091c315a7c6e\n", " a349c6aa-42fa-4931-99ab-091c315a7c6e"} {
		t.Run(id, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"name": "new-vm", "poolID": id})
			if err := Schema("cold-restore-input", raw); err == nil {
				t.Fatalf("invalid pool UUID accepted: %q", id)
			}
		})
	}
}

func TestColdRecoveryRestoreNameRejectsAmbiguousContent(t *testing.T) {
	for _, name := range []string{"", "   ", "\t", "new/guest", "new\\guest", "new\x00guest", "new\nguest", "new\x7fguest", "new\u202eguest", strings.Repeat("x", 129)} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"name": name, "poolID": "a349c6aa-42fa-4931-99ab-091c315a7c6e"})
			if err := Schema("cold-restore-input", raw); err == nil {
				t.Fatalf("invalid restore name accepted: %q", name)
			}
		})
	}
}
