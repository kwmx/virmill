package xmlpatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapturedQEMU12BootMediaAndOpaquePreservation(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", "configuration", name+".xml"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	cpus, memory := uint64(3), uint64(3072)
	cases := []struct {
		before, after string
		edit          HardwareEdit
	}{
		{"boot-media-fixture-attached", "boot-order-applied", HardwareEdit{BootOrder: []BootChoice{{Kind: "disk", ID: "sda"}, {Kind: "disk", ID: "vda"}}}},
		{"boot-order-applied", "boot-media-ejected", HardwareEdit{Resources: ResourceEdit{VCPUs: &cpus, MemoryMiB: &memory}, EjectMedia: "sda", BootOrder: []BootChoice{{Kind: "disk", ID: "vda"}}}},
	}
	four := uint64(4)
	cases = append(cases, struct {
		before, after string
		edit          HardwareEdit
	}{"boot-media-external-metadata", "boot-media-opaque-edited", HardwareEdit{Resources: ResourceEdit{VCPUs: &four}, BootOrder: []BootChoice{{Kind: "disk", ID: "vda"}}}})
	for _, test := range cases {
		t.Run(test.after, func(t *testing.T) {
			before, observed := read(test.before), read(test.after)
			proposed, err := EditHardware(before, test.edit)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := HardwareDigest(proposed)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := HardwareDigest(observed)
			if err != nil {
				t.Fatal(err)
			}
			if actual != expected {
				t.Fatal("captured native definition differs from the exact reviewed edit")
			}
			changed := strings.Replace(observed, "action='none'", "action='reset'", 1)
			changedHash, err := HardwareDigest(changed)
			if err != nil || changedHash == actual {
				t.Fatal("unrelated watchdog change must remain significant", err)
			}
		})
	}
	opaque := read("boot-media-opaque-edited")
	altered := strings.Replace(opaque, "unknown extension retained", "unknown extension changed", 1)
	originalHash, _ := HardwareDigest(opaque)
	alteredHash, err := HardwareDigest(altered)
	if err != nil || alteredHash == originalHash {
		t.Fatal("opaque namespace contents lost significance", err)
	}
	t.Log("captured XML regression only; actual guest execution is recorded separately in the native evidence ledger")
}
