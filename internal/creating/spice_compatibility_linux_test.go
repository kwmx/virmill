//go:build linux && amd64

package creating

import (
	"encoding/json"
	"os"
	"testing"

	"virmill.local/core/internal/validation"
)

func TestCreationDisplaySchemaKeepsLegacyAndRequiresExplicitSpicePolicy(t *testing.T) {
	raw, err := os.ReadFile("../../examples/creation/prepared-ova.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		graphics      string
		policy, valid bool
	}{
		{"none", false, true}, {"vnc-unix", false, true},
		{"spice-unix", false, false}, {"spice-unix", true, true},
		{"spice-tcp", true, false},
	} {
		t.Run(tc.graphics+map[bool]string{true: "-policy", false: "-legacy"}[tc.policy], func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			hw := value["hardware"].(map[string]any)
			hw["graphics"] = tc.graphics
			delete(hw, "devicePolicy")
			if tc.policy {
				hw["devicePolicy"] = map[string]any{"version": 1, "chipset": "q35", "pciPlacement": "libvirt-auto", "usbController": "none", "memoryBalloon": "none", "watchdogAction": "none", "input": "ps2", "audio": "none", "serial": "isa-serial"}
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			err = validation.Schema("vm-creation-input", encoded)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}
