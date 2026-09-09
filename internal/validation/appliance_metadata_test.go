package validation

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/app/importer"
)

// Reuse the archived receipt from the generated multi-disk fixture. This is
// strictly offline schema evidence, not another execution of the native probe.
func applianceReceiptFixture(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("../../docs/evidence/logs/multidisk-native-import-tui-001.log")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var record struct {
			ImportResult struct {
				Artifact map[string]any `json:"artifact"`
			} `json:"importResult"`
		}
		if json.Unmarshal(line, &record) == nil && record.ImportResult.Artifact["kind"] == "PreparedImport" {
			return record.ImportResult.Artifact
		}
	}
	t.Fatal("archived multi-disk receipt missing")
	return nil
}

func applianceReceiptJSON(t *testing.T, receipt map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestApplianceMetadataReceiptCompatibility(t *testing.T) {
	receipt := applianceReceiptFixture(t)
	system := receipt["system"].(map[string]any)
	for _, field := range []string{"os", "ovfOS", "osSource", "firmware", "devices"} {
		if _, found := system[field]; found {
			t.Fatalf("old receipt unexpectedly has %s", field)
		}
	}
	if err := Schema("prepared-import", applianceReceiptJSON(t, receipt)); err != nil {
		t.Fatal("old receipt must remain valid", err)
	}

	system["os"] = "Windows11_64"
	system["ovfOS"] = "Windows10_64"
	system["osSource"] = "virtualbox"
	system["firmware"] = "uefi"
	system["devices"] = []any{
		map[string]any{"kind": "audio", "model": "HDA", "enabled": false},
		map[string]any{"kind": "usb", "model": "OHCI"},
	}
	item := system["hardware"].([]any)[0].(map[string]any)
	item["resourceSubType"] = "AHCI"
	item["description"] = "SATA controller"
	if err := Schema("prepared-import", applianceReceiptJSON(t, receipt)); err != nil {
		t.Fatal("enriched receipt must remain valid", err)
	}

	// Round-trip through the real importer DTO, including false versus unknown.
	data, err := json.Marshal(system)
	if err != nil {
		t.Fatal(err)
	}
	var decoded importer.System
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OS != "Windows11_64" || decoded.OVFOS != "Windows10_64" || decoded.OSSource != "virtualbox" || decoded.Firmware != "uefi" || len(decoded.Devices) != 2 || decoded.Devices[0].Enabled == nil || *decoded.Devices[0].Enabled || decoded.Devices[1].Enabled != nil || decoded.Items[0].ResourceSubType != "AHCI" || decoded.Items[0].Description != "SATA controller" {
		t.Fatalf("metadata lost in DTO: %+v", decoded)
	}
	data, err = json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]any
	if err = json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(system, roundTrip) {
		t.Fatal("system metadata changed during round-trip")
	}
	receipt["system"] = roundTrip
	if err := Schema("prepared-import", applianceReceiptJSON(t, receipt)); err != nil {
		t.Fatal("round-trip receipt must remain valid", err)
	}
}

func TestApplianceMetadataReceiptRejectsInvalidProfile(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"unknown firmware", func(s map[string]any) { s["firmware"] = "EFI" }},
		{"empty firmware", func(s map[string]any) { s["firmware"] = "" }},
		{"too many devices", func(s map[string]any) {
			devices := make([]any, 65)
			for i := range devices {
				devices[i] = map[string]any{"kind": "usb"}
			}
			s["devices"] = devices
		}},
		{"long device model", func(s map[string]any) {
			s["devices"] = []any{map[string]any{"kind": "usb", "model": strings.Repeat("a", 2049)}}
		}},
		{"nonboolean enabled", func(s map[string]any) { s["devices"] = []any{map[string]any{"kind": "audio", "enabled": "false"}} }},
		{"missing device kind", func(s map[string]any) { s["devices"] = []any{map[string]any{"model": "HDA"}} }},
		{"unknown device field", func(s map[string]any) { s["devices"] = []any{map[string]any{"kind": "usb", "supported": true}} }},
		{"long subtype", func(s map[string]any) {
			s["hardware"].([]any)[0].(map[string]any)["resourceSubType"] = strings.Repeat("a", 2049)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receipt := applianceReceiptFixture(t)
			tc.edit(receipt["system"].(map[string]any))
			if err := Schema("prepared-import", applianceReceiptJSON(t, receipt)); err == nil {
				t.Fatal("invalid profile accepted")
			}
		})
	}
}
