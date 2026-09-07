//go:build linux && cgo

package libvirt

import (
	"strings"
	"testing"
)

const creationNVRAMObservedPath = "/var/lib/libvirt/qemu/nvram/Virmill UEFI TPM cold probe_VARS.fd"

func TestCreationNVRAMObservedPathSyntax(t *testing.T) {
	target, _, wanted, observed := creationFirmwareReplay(t)
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"captured path with spaces", creationNVRAMObservedPath, true},
		{"another unverified absolute path", "/fixture/unverified-other-state.fd", true},
		{"interior spaces", "/fixture/guest  one VARS.fd", true},
		{"ordinary dot filename", "/fixture/.guest..VARS.fd", true},
		{"unicode filename", "/fixture/ضيف VARS.fd", true},
		{"exact empty remains unresolved", "", true},
		{"root only", "/", false},
		{"relative", "VARS.fd", false},
		{"dot", ".", false},
		{"parent", "..", false},
		{"dot component", "/fixture/./VARS.fd", false},
		{"parent component", "/fixture/../VARS.fd", false},
		{"doubled root separator", "//fixture/VARS.fd", false},
		{"doubled interior separator", "/fixture//VARS.fd", false},
		{"trailing separator", "/fixture/VARS.fd/", false},
		{"leading space", " /fixture/VARS.fd", false},
		{"trailing space", "/fixture/VARS.fd ", false},
		{"leading tab", "\t/fixture/VARS.fd", false},
		{"trailing newline", "/fixture/VARS.fd\n", false},
		{"whitespace only", " \n\t", false},
		{"embedded tab", "/fixture/guest\tVARS.fd", false},
		{"embedded newline", "/fixture/guest\nVARS.fd", false},
		{"embedded carriage return", "/fixture/guest\rVARS.fd", false},
		{"embedded NUL", "/fixture/guest\x00VARS.fd", false},
		{"embedded delete", "/fixture/guest\x7fVARS.fd", false},
		{"embedded C1 control", "/fixture/guest\u0085VARS.fd", false},
		{"bidirectional format", "/fixture/guest\u202eVARS.fd", false},
		{"encoded leading space", "&#x20;/fixture/VARS.fd", false},
		{"encoded embedded newline", "/fixture/guest&#xA;VARS.fd", false},
		{"CDATA surrounding space", "<![CDATA[ /fixture/VARS.fd ]]>", false},
		{"oversized path", "/fixture/" + strings.Repeat("a", 4096), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := strings.Replace(observed, creationNVRAMObservedPath, tc.text, 1)
			// Exercise both captured normalization and the same definition without
			// that cohort. Neither route may trim or relax an assigned path.
			withoutFirmware := strings.Replace(strings.Replace(changed, "<os firmware='efi'>", "<os>", 1), observedNonSecureFirmware, "", 1)
			for _, raw := range []string{changed, withoutFirmware} {
				err := matchesCreationPolicy(wanted, raw, target.Spec.DevicePolicy)
				if (err == nil) != tc.valid {
					t.Fatalf("observed NVRAM syntax accepted=%t, want %t: %v", err == nil, tc.valid, err)
				}
			}
		})
	}
}

func TestCreationNVRAMNonemptyWantedStillValidatesObservedPath(t *testing.T) {
	for _, tc := range []struct {
		name, wanted, observed string
		valid                  bool
	}{
		{"exact valid path", "/fixture/guest VARS.fd", "/fixture/guest VARS.fd", true},
		{"another valid path is not equal", "/fixture/guest VARS.fd", "/fixture/other VARS.fd", false},
		{"empty cannot replace assigned path", "/fixture/guest VARS.fd", "", false},
		{"equal root only", "/", "/", false},
		{"equal relative", "VARS.fd", "VARS.fd", false},
		{"equal noncanonical", "/fixture/../VARS.fd", "/fixture/../VARS.fd", false},
		{"equal surrounding whitespace", " /fixture/VARS.fd ", " /fixture/VARS.fd ", false},
		{"observed is not silently trimmed", "/fixture/VARS.fd", " /fixture/VARS.fd ", false},
		{"equal whitespace only", " \n\t", " \n\t", false},
		{"equal embedded control", "/fixture/guest\tVARS.fd", "/fixture/guest\tVARS.fd", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Structural matching can receive an already assigned expected path.
			// Equal strings must not bypass the observed destination check.
			domainXML := func(value string) string {
				return "<domain><os><nvram>" + value + "</nvram></os><devices/></domain>"
			}
			err := matchesCreation(domainXML(tc.wanted), domainXML(tc.observed))
			if (err == nil) != tc.valid {
				t.Fatalf("nonempty-path match accepted=%t, want %t: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestCreationNVRAMObservedShapeRemainsStrict(t *testing.T) {
	target, _, wanted, observed := creationFirmwareReplay(t)
	for name, changed := range map[string]string{
		"duplicate element":               strings.Replace(observed, "</nvram>", "</nvram><nvram>/fixture/other.fd</nvram>", 1),
		"duplicate attribute":             strings.Replace(observed, "templateFormat='raw'", "templateFormat='raw' templateFormat='raw'", 1),
		"conflicting duplicate attribute": strings.Replace(observed, "templateFormat='raw'", "templateFormat='raw' templateFormat='qcow2'", 1),
		"foreign element":                 strings.Replace(observed, "<nvram ", "<nvram xmlns='urn:foreign' ", 1),
		"foreign attribute":               strings.Replace(observed, "<nvram ", "<nvram xmlns:x='urn:foreign' x:source='file' ", 1),
		"foreign namespace declaration":   strings.Replace(observed, "<nvram ", "<nvram xmlns:x='urn:foreign' ", 1),
		"structured source":               strings.Replace(observed, creationNVRAMObservedPath, "<source file='/fixture/VARS.fd'/>", 1),
		"mixed source and text":           strings.Replace(observed, creationNVRAMObservedPath, creationNVRAMObservedPath+"<source file='/fixture/VARS.fd'/>", 1),
	} {
		t.Run(name, func(t *testing.T) {
			withoutFirmware := strings.Replace(strings.Replace(changed, "<os firmware='efi'>", "<os>", 1), observedNonSecureFirmware, "", 1)
			for _, raw := range []string{changed, withoutFirmware} {
				if err := matchesCreationPolicy(wanted, raw, target.Spec.DevicePolicy); err == nil {
					t.Fatal("ambiguous, foreign or structured NVRAM became an accepted path")
				}
			}
		})
	}
}
