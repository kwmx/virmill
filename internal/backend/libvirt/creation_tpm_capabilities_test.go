//go:build linux && cgo

package libvirt

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"strconv"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

// Synthetic metadata only. These paths are never opened and no native
// connection, emulator, firmware or TPM operation is used by these tests.
const creationTPMCapsXML = `<domainCapabilities>
  <path>/fixture/tpm-capabilities/qemu-system-x86_64</path>
  <domain>kvm</domain><machine>pc-q35-10.2</machine><arch>x86_64</arch>
  <vcpu max="255"/>
  <os supported="yes"><loader supported="yes">
    <value>/fixture/tpm-capabilities/CODE.fd</value>
    <enum name="type"><value>pflash</value></enum>
    <enum name="secure"><value>no</value><value>yes</value></enum>
  </loader></os>
  <cpu><mode name="host-passthrough" supported="yes"/></cpu>
  <devices>
    <disk supported="yes"><enum name="bus"><value>sata</value><value>scsi</value><value>virtio</value></enum></disk>
    <interface supported="yes"/>
    <graphics supported="yes"><enum name="type"><value>vnc</value></enum></graphics>
    <tpm supported="yes">
      <enum name="model"><value>tpm-crb</value></enum>
      <enum name="backendModel"><value>emulator</value></enum>
      <enum name="backendVersion"><value>2.0</value></enum>
    </tpm>
  </devices>
</domainCapabilities>`

const creationTPMDescriptorJSON = `{
  "mapping": {
    "device": "flash", "mode": "split",
    "executable": {"filename": "/fixture/tpm-capabilities/CODE.fd", "format": "raw"},
    "nvram-template": {"filename": "/fixture/tpm-capabilities/VARS.fd", "format": "raw"}
  },
  "targets": [{"architecture": "x86_64", "machines": ["pc-q35-*"]}],
  "features": []
}`

func creationTPMSpec(t *testing.T) domain.CreationSpec {
	t.Helper()
	target, _ := creationFixture()
	target.Spec.Firmware = domain.CreationFirmware{
		Mode: "uefi", Code: "/fixture/tpm-capabilities/CODE.fd",
		Template: "/fixture/tpm-capabilities/VARS.fd", Format: "raw", TPM: true,
	}
	if err := validateCreationSpec(target.Spec); err != nil {
		t.Fatalf("invalid synthetic creation intent: %v", err)
	}
	return target.Spec
}

func decodeCreationTPMCaps(t *testing.T, raw string) domainCaps {
	t.Helper()
	var caps domainCaps
	if err := xml.Unmarshal([]byte(raw), &caps); err != nil {
		t.Fatalf("invalid synthetic capability XML: %v", err)
	}
	return caps
}

func TestCreationTPMCapabilitiesRequireEachAdvertisedClaim(t *testing.T) {
	spec := creationTPMSpec(t)
	if err := checkCaps(decodeCreationTPMCaps(t, creationTPMCapsXML), spec); err != nil {
		t.Fatalf("complete synthetic capability tuple rejected: %v", err)
	}
	const model = `<enum name="model"><value>tpm-crb</value></enum>`
	const backend = `<enum name="backendModel"><value>emulator</value></enum>`
	const version = `<enum name="backendVersion"><value>2.0</value></enum>`
	for _, tc := range []struct{ name, old, replacement string }{
		{"missing support", `<tpm supported="yes">`, `<tpm>`},
		{"support denied", `<tpm supported="yes">`, `<tpm supported="no">`},
		{"support unknown", `<tpm supported="yes">`, `<tpm supported="unknown">`},
		{"missing model", model, ""},
		{"different model", model, `<enum name="model"><value>tpm-tis</value></enum>`},
		{"missing backend", backend, ""},
		{"different backend", backend, `<enum name="backendModel"><value>passthrough</value></enum>`},
		{"missing version", version, ""},
		{"different version", version, `<enum name="backendVersion"><value>1.2</value></enum>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Count(creationTPMCapsXML, tc.old) != 1 {
				t.Fatal("fixture mutation must change exactly one TPM claim")
			}
			caps := decodeCreationTPMCaps(t, strings.Replace(creationTPMCapsXML, tc.old, tc.replacement, 1))
			err := checkCaps(caps, spec)
			var typed *domain.Error
			if !errors.As(err, &typed) || typed.Code != "UNSUPPORTED_CAPABILITY" {
				t.Fatalf("incomplete TPM tuple must fail visibly: %v", err)
			}
			// The same broken capability document must pass every other gate,
			// including the exact UEFI loader, when TPM is not requested. This
			// prevents loader/CPU/device failures from masking the TPM predicate.
			withoutTPM := spec
			withoutTPM.Firmware.TPM = false
			if err := checkCaps(caps, withoutTPM); err != nil {
				t.Fatalf("another capability gate masked the TPM rejection: %v", err)
			}
		})
	}
}

func TestCreationTPMCapabilitiesAcceptDeclarationsWithoutRuntimeEvidence(t *testing.T) {
	spec := creationTPMSpec(t)
	if err := checkCaps(decodeCreationTPMCaps(t, creationTPMCapsXML), spec); err != nil {
		t.Fatal(err)
	}
	var descriptor firmwareDescriptor
	if err := json.Unmarshal([]byte(creationTPMDescriptorJSON), &descriptor); err != nil {
		t.Fatal(err)
	}
	if !descriptorMatches(descriptor, spec) {
		t.Fatal("exact non-Secure-Boot descriptor mapping rejected")
	}
	// The complete advertised TPM tuple and a descriptor with no features both
	// pass. Neither pure predicate executes the named firmware or obtains a
	// protocol result; nil errors therefore cannot certify EFI_TCG2 or recovery.
	t.Log("synthetic declaration predicates passed; EFI_TCG2, TPM commands, auxiliary identity, boot and recovery remain unverified")
}

func TestCreationTPMDescriptorRejectsUnenrolledSMMForBothPolicies(t *testing.T) {
	for _, format := range []string{"raw", "qcow2"} {
		for _, secureBoot := range []bool{false, true} {
			t.Run(format+"/secureBoot="+strconv.FormatBool(secureBoot), func(t *testing.T) {
				spec := creationTPMSpec(t)
				spec.Firmware.SecureBoot = secureBoot
				spec.Firmware.Format = format
				var descriptor firmwareDescriptor
				if err := json.Unmarshal([]byte(creationTPMDescriptorJSON), &descriptor); err != nil {
					t.Fatal(err)
				}
				descriptor.Mapping.Executable.Format = format
				descriptor.Mapping.Template.Format = format
				// Feature projection of the recorded 40/41 SB+SMM descriptors:
				// secure-capable code with an unenrolled template. The synthetic
				// filenames identify no installed package or runtime firmware.
				descriptor.Features = []string{"acpi-s3", "requires-smm", "secure-boot", "verbose-dynamic"}
				if descriptorMatches(descriptor, spec) {
					t.Fatal("unenrolled SMM descriptor accepted by the current boolean policy")
				}
				// Alter only the missing/incompatible policy claim. A match now
				// proves the prior failure was not code/template/format/target.
				if secureBoot {
					descriptor.Features = append(descriptor.Features, "enrolled-keys")
				} else {
					descriptor.Features = []string{"acpi-s3", "secure-boot", "verbose-dynamic"}
				}
				if !descriptorMatches(descriptor, spec) {
					t.Fatal("another descriptor mismatch masked the key/SMM policy rejection")
				}
			})
		}
	}
}
