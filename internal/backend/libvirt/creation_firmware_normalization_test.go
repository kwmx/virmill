//go:build linux && cgo

package libvirt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

const observedNonSecureFirmware = `    <firmware>
      <feature enabled='no' name='enrolled-keys'/>
      <feature enabled='no' name='secure-boot'/>
    </firmware>
`

func creationFirmwareReplay(t testing.TB) (domain.CreationTarget, []domain.CreatedVolume, string, string) {
	t.Helper()
	read := func(name, digest string) []byte {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", "creation", "uefi-normalization", name))
		if err != nil {
			t.Fatal(err)
		}
		actual := sha256.Sum256(data)
		if hex.EncodeToString(actual[:]) != digest {
			t.Fatalf("captured fixture %s differs from its recorded bytes", name)
		}
		return data
	}
	observed := read("observed.xml", "36a51eefeb3f4b48f46f1308668b1e4c3cef3b19c03ba8b9fd7da6b70c61c500")
	// Re-pinned after private-values-redaction-001 replaced the private source directory.
	planJSON := read("reviewed-plan.json", "5590ab87a96b96e660240eb48cc4bfd1dcadbaa424cfc63aa3f87b912c7d7723")
	var plan struct {
		PlanID      string `json:"planID"`
		InputDigest string `json:"inputDigest"`
		Review      struct {
			Target  domain.CreationTarget `json:"target"`
			Volumes []domain.VolumeIntent `json:"volumes"`
		} `json:"review"`
	}
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		t.Fatal(err)
	}
	binding, err := operations.Digest([]string{plan.PlanID, plan.InputDigest})
	if err != nil || binding != "3fde7525e4b89e4acd513aa61dc7340841adc72c8ef10298252fe9ed319b6059" {
		t.Fatal("reviewed plan does not reproduce the creation binding", binding, err)
	}
	volumes := make([]domain.CreatedVolume, len(plan.Review.Volumes))
	for i, intent := range plan.Review.Volumes {
		volumes[i].Intent = intent
	}
	wanted, err := creationXML(plan.Review.Target, volumes, binding)
	if err != nil {
		t.Fatal(err)
	}
	return plan.Review.Target, volumes, wanted, string(observed)
}

// This deliberately bypasses any firmware normalization. It retains precisely
// the pre-existing memory and reviewed-device rules to isolate the native delta.
func creationFirmwarePriorMatch(t *testing.T, wanted, observed string, policy *domain.CreationDevicePolicy) bool {
	t.Helper()
	w, err := xmlTree(wanted)
	if err != nil {
		t.Fatal(err)
	}
	g, err := xmlTree(observed)
	if err != nil {
		t.Fatal(err)
	}
	if err := memoryKiB(w); err != nil {
		t.Fatal(err)
	}
	if err := memoryKiB(g); err != nil {
		t.Fatal(err)
	}
	if err := normalizeCreationPCI(w, g, policy); err != nil {
		t.Fatal(err)
	}
	return matchesNode(w, g)
}

func TestCreationFirmwareNormalizationIsSoleCapturedMismatch(t *testing.T) {
	target, _, wanted, observed := creationFirmwareReplay(t)
	if creationFirmwarePriorMatch(t, wanted, observed, target.Spec.DevicePolicy) {
		t.Fatal("fixture no longer reproduces the prior mismatch")
	}
	withoutAttribute := strings.Replace(observed, "<os firmware='efi'>", "<os>", 1)
	withoutFeatures := strings.Replace(observed, observedNonSecureFirmware, "", 1)
	if withoutAttribute == observed || withoutFeatures == observed {
		t.Fatal("captured normalization fixture did not apply")
	}
	for _, changed := range []string{withoutAttribute, withoutFeatures} {
		if creationFirmwarePriorMatch(t, wanted, changed, target.Spec.DevicePolicy) {
			t.Fatal("only one half of the native firmware delta explains the mismatch")
		}
	}
	withoutBoth := strings.Replace(withoutAttribute, observedNonSecureFirmware, "", 1)
	if !creationFirmwarePriorMatch(t, wanted, withoutBoth, target.Spec.DevicePolicy) {
		t.Fatal("additional mismatch remains after removing only the two captured firmware additions")
	}
}

func TestCreationFirmwareNormalizationCapturedNonSecureDefinition(t *testing.T) {
	target, _, wanted, observed := creationFirmwareReplay(t)
	if strings.Contains(wanted, "<firmware>") || strings.Contains(wanted, `<os firmware=`) {
		t.Fatal("creation rendering changed to bypass match-only normalization")
	}
	if err := matchesCreationPolicy(wanted, observed, target.Spec.DevicePolicy); err != nil {
		t.Fatal("captured non-secure normalization differs from the reviewed explicit loader/template", err)
	}
}

func TestCreationFirmwareNormalizationRejectsChangedObservedSemantics(t *testing.T) {
	target, _, wanted, observed := creationFirmwareReplay(t)
	for name, edit := range map[string][2]string{
		"root foreign namespace":        {"<domain type='kvm'>", "<domain xmlns='urn:foreign' type='kvm'>"},
		"root extra namespace":          {"<domain type='kvm'>", "<domain xmlns:x='urn:foreign' type='kvm'>"},
		"root duplicate attr":           {"<domain type='kvm'>", "<domain type='kvm' type='kvm'>"},
		"firmware BIOS":                 {"firmware='efi'", "firmware='bios'"},
		"firmware empty":                {"firmware='efi'", "firmware=''"},
		"firmware uppercase":            {"firmware='efi'", "firmware='EFI'"},
		"OS unknown attr":               {"firmware='efi'", "firmware='efi' future='enabled'"},
		"OS duplicate attr":             {"firmware='efi'", "firmware='efi' firmware='efi'"},
		"OS foreign attr":               {"firmware='efi'", "x:firmware='efi' xmlns:x='urn:foreign'"},
		"OS foreign namespace":          {"<os firmware='efi'>", "<os xmlns='urn:foreign' firmware='efi'>"},
		"OS duplicate element":          {"</os>", "</os><os/>"},
		"OS extra boot configuration":   {"</os>", "<boot dev='network'/></os>"},
		"OS unknown child":              {"</os>", "<futureRuntime/></os>"},
		"firmware without OS attr":      {"<os firmware='efi'>", "<os>"},
		"OS attr without firmware":      {observedNonSecureFirmware, ""},
		"firmware unknown attr":         {"<firmware>", "<firmware future='enabled'>"},
		"firmware foreign namespace":    {"<firmware>", "<firmware xmlns='urn:foreign'>"},
		"firmware duplicate":            {"</firmware>", "</firmware>" + observedNonSecureFirmware},
		"firmware text":                 {"<firmware>", "<firmware>unreviewed"},
		"firmware unknown child":        {"</firmware>", "<state/></firmware>"},
		"enrolled keys yes":             {"enabled='no' name='enrolled-keys'", "enabled='yes' name='enrolled-keys'"},
		"secure boot yes":               {"enabled='no' name='secure-boot'", "enabled='yes' name='secure-boot'"},
		"feature missing":               {"      <feature enabled='no' name='secure-boot'/>\n", ""},
		"feature repeated":              {"name='secure-boot'", "name='enrolled-keys'"},
		"feature unknown":               {"name='secure-boot'", "name='other'"},
		"feature empty enabled":         {"enabled='no' name='secure-boot'", "enabled='' name='secure-boot'"},
		"feature noncanonical enabled":  {"enabled='no' name='secure-boot'", "enabled='false' name='secure-boot'"},
		"feature missing enabled":       {"enabled='no' name='secure-boot'", "name='secure-boot'"},
		"feature duplicate enabled":     {"enabled='no' name='secure-boot'", "enabled='no' enabled='no' name='secure-boot'"},
		"feature duplicate name":        {"name='secure-boot'", "name='secure-boot' name='secure-boot'"},
		"feature extra attr":            {"name='secure-boot'", "name='secure-boot' value='no'"},
		"feature foreign enabled":       {"enabled='no' name='secure-boot'", "x:enabled='no' name='secure-boot' xmlns:x='urn:foreign'"},
		"feature foreign element":       {"<feature enabled='no' name='secure-boot'/>", "<x:feature enabled='no' name='secure-boot' xmlns:x='urn:foreign'/>"},
		"feature text":                  {"<feature enabled='no' name='secure-boot'/>", "<feature enabled='no' name='secure-boot'>no</feature>"},
		"feature nested content":        {"<feature enabled='no' name='secure-boot'/>", "<feature enabled='no' name='secure-boot'><value/></feature>"},
		"loader code changed":           {"/usr/share/edk2/ovmf/OVMF_CODE.fd", "/usr/share/edk2/ovmf/OTHER_CODE.fd"},
		"loader secure enabled":         {"secure='no'", "secure='yes'"},
		"loader writable":               {"readonly='yes'", "readonly='no'"},
		"loader ROM":                    {"type='pflash'", "type='rom'"},
		"loader format changed":         {"type='pflash' format='raw'", "type='pflash' format='qcow2'"},
		"loader stateless added":        {"type='pflash'", "type='pflash' stateless='yes'"},
		"loader stateless no added":     {"type='pflash'", "type='pflash' stateless='no'"},
		"loader duplicate attr":         {"secure='no'", "secure='no' secure='no'"},
		"loader foreign attr":           {"secure='no'", "x:secure='no' xmlns:x='urn:foreign'"},
		"loader foreign element":        {"<loader ", "<loader xmlns='urn:foreign' "},
		"loader duplicate":              {"</loader>", "</loader><loader/>"},
		"NVRAM template changed":        {"template='/usr/share/edk2/ovmf/OVMF_VARS.fd'", "template='/usr/share/edk2/ovmf/OTHER_VARS.fd'"},
		"NVRAM format changed":          {"templateFormat='raw' format='raw'", "templateFormat='raw' format='qcow2'"},
		"NVRAM template format changed": {"templateFormat='raw'", "templateFormat='qcow2'"},
		"NVRAM type added":              {"<nvram ", "<nvram type='file' "},
		"NVRAM duplicate attr":          {"templateFormat='raw'", "templateFormat='raw' templateFormat='raw'"},
		"NVRAM foreign attr":            {"templateFormat='raw'", "x:templateFormat='raw' xmlns:x='urn:foreign'"},
		"NVRAM foreign element":         {"<nvram ", "<nvram xmlns='urn:foreign' "},
		"NVRAM duplicate":               {"</nvram>", "</nvram><nvram/>"},
		"NVRAM structured source":       {"/var/lib/libvirt/qemu/nvram/Virmill UEFI TPM cold probe_VARS.fd", "<source file='/fixture/VARS.fd'/>"},
		"machine changed":               {"machine='pc-q35-10.2'", "machine='pc-q35-9.2'"},
		"architecture changed":          {"arch='x86_64'", "arch='aarch64'"},
		"watchdog reset":                {"action='none'", "action='reset'"},
		"USB controller changed":        {"type='usb' index='0' model='none'", "type='usb' index='0' model='qemu-xhci'"},
		"balloon changed":               {"<memballoon model='none'/>", "<memballoon model='virtio'/>"},
		"audio backend changed":         {"<audio id='1' type='none'/>", "<audio id='1' type='pipewire'/>"},
		"TPM version changed":           {"version='2.0'", "version='1.2'"},
		"extra hostdev":                 {"</devices>", "<hostdev mode='subsystem' type='pci'/></devices>"},
		"extra metadata":                {"</metadata>", "<foreign xmlns='urn:unreviewed'/></metadata>"},
	} {
		t.Run(name, func(t *testing.T) {
			changed := strings.Replace(observed, edit[0], edit[1], 1)
			if changed == observed {
				t.Fatal("mutation did not apply")
			}
			if err := matchesCreationPolicy(wanted, changed, target.Spec.DevicePolicy); err == nil {
				t.Fatal("normalization accepted an unreviewed semantic change")
			}
		})
	}
}

func TestCreationFirmwareNormalizationRequiresExplicitNonSecureMapping(t *testing.T) {
	target, _, wanted, observed := creationFirmwareReplay(t)
	for name, edit := range map[string][4]string{
		"missing secure declaration":             {` secure="no"`, "", " secure='no'", ""},
		"secure loader":                          {`secure="no"`, `secure="yes"`, "secure='no'", "secure='yes'"},
		"missing readonly":                       {` readonly="yes"`, "", " readonly='yes'", ""},
		"writable loader":                        {`readonly="yes"`, `readonly="no"`, "readonly='yes'", "readonly='no'"},
		"missing loader type":                    {` type="pflash"`, "", " type='pflash'", ""},
		"ROM loader":                             {`type="pflash"`, `type="rom"`, "type='pflash'", "type='rom'"},
		"missing loader format":                  {` format="raw"`, "", " format='raw'", ""},
		"unknown loader format":                  {` format="raw"`, ` format="future"`, " format='raw'", " format='future'"},
		"missing loader path":                    {"/usr/share/edk2/ovmf/OVMF_CODE.fd", "", "/usr/share/edk2/ovmf/OVMF_CODE.fd", ""},
		"relative loader path":                   {"/usr/share/edk2/ovmf/OVMF_CODE.fd", "CODE.fd", "/usr/share/edk2/ovmf/OVMF_CODE.fd", "CODE.fd"},
		"missing template":                       {` template="/usr/share/edk2/ovmf/OVMF_VARS.fd"`, "", " template='/usr/share/edk2/ovmf/OVMF_VARS.fd'", ""},
		"missing template format":                {` templateFormat="raw"`, "", " templateFormat='raw'", ""},
		"relative template":                      {"/usr/share/edk2/ovmf/OVMF_VARS.fd", "VARS.fd", "/usr/share/edk2/ovmf/OVMF_VARS.fd", "VARS.fd"},
		"mismatched template and loader formats": {`templateFormat="raw"`, `templateFormat="qcow2"`, "templateFormat='raw'", "templateFormat='qcow2'"},
		"non-HVM":                                {">hvm</type>", ">linux</type>", ">hvm</type>", ">linux</type>"},
		"non-x86":                                {`arch="x86_64"`, `arch="aarch64"`, "arch='x86_64'", "arch='aarch64'"},
		"extra OS intent":                        {"</os>", "<futureConfig/></os>", "</os>", "<futureConfig/></os>"},
	} {
		t.Run(name, func(t *testing.T) {
			w := strings.Replace(wanted, edit[0], edit[1], 1)
			g := strings.Replace(observed, edit[2], edit[3], 1)
			if w == wanted || g == observed {
				t.Fatal("mapping mutation did not apply")
			}
			if err := matchesCreationPolicy(w, g, target.Spec.DevicePolicy); err == nil {
				t.Fatal("firmware additions accepted outside the explicit non-secure mapping cohort")
			}
		})
	}
	// Remove the explicit loader from both sides, leaving an otherwise matching
	// template. EFI feature declarations alone must not authorize a loader choice.
	w, g := wanted, observed
	for _, pair := range []struct {
		text  *string
		start string
	}{{&w, "<loader "}, {&g, "<loader "}} {
		start := strings.Index(*pair.text, pair.start)
		end := strings.Index((*pair.text)[start:], "</loader>") + start + len("</loader>")
		*pair.text = (*pair.text)[:start] + (*pair.text)[end:]
	}
	if matchesCreationPolicy(w, g, target.Spec.DevicePolicy) == nil {
		t.Fatal("EFI additions accepted without any explicit loader")
	}
}

func TestCreationFirmwareNormalizationPreservesExistingNVRAMPathRule(t *testing.T) {
	target, _, wanted, observed := creationFirmwareReplay(t)
	const original = "/var/lib/libvirt/qemu/nvram/Virmill UEFI TPM cold probe_VARS.fd"
	for _, replacement := range []string{original, "/fixture/unverified-other-state.fd", "", "relative-state.fd", "<source file='/fixture/VARS.fd'/>"} {
		changed := strings.Replace(observed, original, replacement, 1)
		prior := strings.Replace(strings.Replace(changed, "<os firmware='efi'>", "<os>", 1), observedNonSecureFirmware, "", 1)
		before := creationFirmwarePriorMatch(t, wanted, prior, target.Spec.DevicePolicy)
		after := matchesCreationPolicy(wanted, changed, target.Spec.DevicePolicy) == nil
		if before != after {
			t.Fatalf("firmware normalization changed the prior NVRAM path rule: before=%t after=%t", before, after)
		}
	}
	// The existing empty-wanted-path rule accepts another absolute path. This
	// test documents its limit; no file identity, freshness or ownership is proven.
	if matchesCreationPolicy(wanted, strings.Replace(observed, original, "/fixture/unverified-other-state.fd", 1), target.Spec.DevicePolicy) != nil {
		t.Fatal("unexpected change to the separately scoped NVRAM path behavior")
	}
}

func TestCreationFirmwareNormalizationIsContextualOnly(t *testing.T) {
	target, _, wanted, observed := creationFirmwareReplay(t)
	if err := matchesCreationPolicy(wanted, wanted, target.Spec.DevicePolicy); err != nil {
		t.Fatal("unchanged rendering no longer matches itself", err)
	}
	first := "<feature enabled='no' name='enrolled-keys'/>"
	second := "<feature enabled='no' name='secure-boot'/>"
	reordered := strings.Replace(strings.Replace(observed, first, "@FEATURE@", 1), second, first, 1)
	reordered = strings.Replace(reordered, "@FEATURE@", "<feature name='secure-boot' enabled='no'/>", 1)
	if err := matchesCreationPolicy(wanted, reordered, target.Spec.DevicePolicy); err != nil {
		t.Fatal("feature and attribute order changed meaning", err)
	}
	if creationDefaultAttr("/domain/os", xml.Attr{Name: xml.Name{Local: "firmware"}, Value: "efi"}) {
		t.Fatal("firmware attribute became a blanket default")
	}
	tree, err := xmlTree(observed)
	if err != nil {
		t.Fatal(err)
	}
	if creationDefaultChild("/domain/os", child(child(tree, "os"), "firmware")) {
		t.Fatal("firmware selection became a blanket default child")
	}
}

func TestCreationFirmwareNormalizationMatchingQcow2IsSynthetic(t *testing.T) {
	target, volumes, _, observed := creationFirmwareReplay(t)
	target.Spec.Firmware.Format = "qcow2"
	target.Spec.Firmware.Code = "/fixture/pinned-code.qcow2"
	target.Spec.Firmware.Template = "/fixture/pinned-template.qcow2"
	wanted, err := creationXML(target, volumes, "3fde7525e4b89e4acd513aa61dc7340841adc72c8ef10298252fe9ed319b6059")
	if err != nil {
		t.Fatal(err)
	}
	observed = strings.ReplaceAll(observed, "format='raw'", "format='qcow2'")
	observed = strings.ReplaceAll(observed, "templateFormat='raw'", "templateFormat='qcow2'")
	observed = strings.ReplaceAll(observed, "/usr/share/edk2/ovmf/OVMF_CODE.fd", target.Spec.Firmware.Code)
	observed = strings.ReplaceAll(observed, "/usr/share/edk2/ovmf/OVMF_VARS.fd", target.Spec.Firmware.Template)
	if err := matchesCreationPolicy(wanted, observed, target.Spec.DevicePolicy); err != nil {
		t.Fatal("matching explicit format and mapping were changed", err)
	}
	t.Log("synthetic mapping equality test only; the captured native normalization used raw firmware")
}
