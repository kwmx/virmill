//go:build linux && cgo

package libvirt

import (
	"maps"
	"strings"
	"testing"

	"virmill.local/core/internal/backend/xmlpatch"
)

// These are adapter contract tests at the XML/recipe boundary, not native
// libvirt execution or guest-agent installation/readiness evidence.
const agentEditOriginal = `<domain type="kvm"><name>guest</name><uuid>570c4866-5b5e-4583-817c-602538d19b9c</uuid><memory unit="KiB">131072</memory><currentMemory unit="KiB">131072</currentMemory><vcpu>1</vcpu><metadata><x:keep xmlns:x="urn:opaque" value="retained">opaque text</x:keep></metadata><os><type arch="x86_64" machine="pc">hvm</type></os><devices><controller type="pci" index="0" model="pci-root"/><disk type="file" device="disk"><driver name="qemu" type="raw"/><source file="/existing/source.raw"/><target dev="hda" bus="ide"/></disk><!--unchanged--><serial type="pty"><target port="0"/></serial><console type="pty"><target type="serial" port="0"/></console></devices></domain>`

func agentEditRecipe(t *testing.T, original string) (map[string]any, string) {
	t.Helper()
	view, err := xmlpatch.InspectGuestAgent(original)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := xmlpatch.EnableGuestAgent(original)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := xmlpatch.GuestAgentDigest(proposal, view.ControllerIndex, view.Port, view.AddsController)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"editVersion": float64(3), "enableGuestAgent": true, "applyMode": "next-boot", "agentController": float64(view.ControllerIndex), "agentPort": float64(view.Port), "agentAddsController": view.AddsController, "xmlSHA256": digest, "vmID": "570c4866-5b5e-4583-817c-602538d19b9c", "editBeforeFingerprint": strings.Repeat("a", 64)}, proposal
}

func TestGuestAgentEditNativeAdapterProposalAndNormalization(t *testing.T) {
	input, proposal := agentEditRecipe(t, agentEditOriginal)
	resources, wanted, err := configurationXML(agentEditOriginal, input)
	if err != nil || wanted != proposal || resources.VCPUs != nil || resources.MemoryMiB != nil {
		t.Fatalf("wrong channel-only proposal %v %+v", err, resources)
	}
	matched, err := configurationMatches(proposal, input)
	if err != nil || !matched {
		t.Fatal("exact proposal did not match", err)
	}
	observed := strings.Replace(proposal, `<controller type="virtio-serial" index="0" model="virtio"/>`, `<controller model="virtio" type="virtio-serial" index="0"><alias name="virtio-serial0"/><address type="pci" domain="0x0000" bus="0x00" slot="0x04" function="0x0"/></controller>`, 1)
	observed = strings.Replace(observed, `</channel>`, `<alias name="channel0"/></channel>`, 1)
	matched, err = configurationMatches(observed, input)
	if err != nil || !matched {
		t.Fatal("narrow native defaults did not match", err)
	}
	if checkSecureConfiguration(observed, observed) != nil {
		t.Fatal("identical secure readback refused")
	}
	if checkSecureConfiguration(observed, strings.Replace(observed, `<channel type="unix">`, `<channel type="unix"><source path="secret"/>`, 1)) == nil {
		t.Fatal("sensitive hidden readback was accepted")
	}
	// Applying the same stale edit to an already-enabled VM must not become a
	// silent success. Durable reconciliation instead uses configurationMatches.
	if _, _, err = configurationXML(proposal, input); err == nil {
		t.Fatal("stale already-enabled proposal accepted")
	}
}
func TestGuestAgentEditNativeAdapterRefusesDriftAndExtraChanges(t *testing.T) {
	input, proposal := agentEditRecipe(t, agentEditOriginal)
	for name, xml := range map[string]string{
		"old disk":          strings.Replace(proposal, "/existing/source.raw", "/other/source.raw", 1),
		"old memory":        strings.Replace(proposal, ">131072<", ">262144<", 1),
		"opaque metadata":   strings.Replace(proposal, "opaque text", "changed text", 1),
		"old comment":       strings.Replace(proposal, "unchanged", "changed", 1),
		"extra controller":  strings.Replace(proposal, "</devices>", `<controller type="pci" model="pcie-root-port" index="9"/></devices>`, 1),
		"extra device":      strings.Replace(proposal, "</devices>", `<interface type="network"><source network="default"/></interface></devices>`, 1),
		"explicit socket":   strings.Replace(proposal, `<channel type="unix">`, `<channel type="unix"><source mode="bind" path="/external/socket"/>`, 1),
		"wrong port":        strings.Replace(proposal, `controller="0" bus="0" port="1"`, `controller="0" bus="0" port="2"`, 1),
		"duplicate channel": strings.Replace(proposal, "</devices>", `<channel type="unix"><target type="virtio" name="org.qemu.guest_agent.0"/><address type="virtio-serial" controller="0" bus="0" port="2"/></channel></devices>`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			matched, err := configurationMatches(xml, input)
			if err == nil && matched {
				t.Fatal("unreviewed readback matched")
			}
		})
	}
	modifiedBefore := strings.Replace(agentEditOriginal, "/existing/source.raw", "/other/source.raw", 1)
	if _, _, err := configurationXML(modifiedBefore, input); err == nil {
		t.Fatal("changed source escaped plan digest")
	}
	changedRecipe := maps.Clone(input)
	changedRecipe["xmlSHA256"] = strings.Repeat("b", 64)
	if _, _, err := configurationXML(agentEditOriginal, changedRecipe); err == nil {
		t.Fatal("wrong digest accepted")
	}
	matched, err := configurationMatches(proposal, changedRecipe)
	if err == nil && matched {
		t.Fatal("wrong readback digest accepted")
	}
	changedRecipe = maps.Clone(input)
	changedRecipe["agentAddsController"] = false
	if _, _, err := configurationXML(agentEditOriginal, changedRecipe); err == nil {
		t.Fatal("controller allocation changed")
	}
	changedRecipe = maps.Clone(input)
	changedRecipe["agentPort"] = float64(2)
	if _, _, err := configurationXML(agentEditOriginal, changedRecipe); err == nil {
		t.Fatal("port allocation changed")
	}
}
func TestGuestAgentEditNativeAdapterRejectsNonChannelRecipes(t *testing.T) {
	input, proposal := agentEditRecipe(t, agentEditOriginal)
	for name, value := range map[string]any{"applyMode": "live", "enableGuestAgent": false, "vcpus": float64(2), "agentPort": 1.5, "agentController": "0", "agentAddsController": "true", "source": "/external/socket"} {
		t.Run(name, func(t *testing.T) {
			bad := maps.Clone(input)
			bad[name] = value
			if _, _, err := configurationXML(agentEditOriginal, bad); err == nil {
				t.Fatal("invalid recipe reached patch")
			}
			if match, err := configurationMatches(proposal, bad); err == nil && match {
				t.Fatal("invalid recipe reconciled")
			}
		})
	}
}
func TestGuestAgentEditNativeAdapterReusesExistingControllerExactly(t *testing.T) {
	existing := strings.Replace(agentEditOriginal, "</devices>", `<controller type="virtio-serial" index="2" model="virtio"><alias name="virtio-serial2"/><address type="pci" domain="0x0000" bus="0x00" slot="0x06" function="0x0"/></controller><channel type="spicevmc"><target type="virtio" name="com.redhat.spice.0"/><address type="virtio-serial" controller="2" bus="0" port="1"/></channel></devices>`, 1)
	input, proposal := agentEditRecipe(t, existing)
	if input["agentController"] != float64(2) || input["agentPort"] != float64(2) || input["agentAddsController"] != false {
		t.Fatal("wrong reused controller recipe", input)
	}
	_, expected, err := configurationXML(existing, input)
	if err != nil || expected != proposal {
		t.Fatal("reused controller proposal", err)
	}
	native := strings.Replace(proposal, `name="org.qemu.guest_agent.0"/><address type="virtio-serial" controller="2" bus="0" port="2"/></channel>`, `name="org.qemu.guest_agent.0"/><address type="virtio-serial" controller="2" bus="0" port="2"/><alias name="channel1"/></channel>`, 1)
	match, err := configurationMatches(native, input)
	if err != nil || !match {
		t.Fatal("native second channel alias", err)
	}
	native = strings.Replace(native, `slot="0x06"`, `slot="0x07"`, 1)
	match, err = configurationMatches(native, input)
	if err == nil && match {
		t.Fatal("preexisting controller change normalized away")
	}
}
func TestGuestAgentEditLegacyVersionsKeepExistingDigestContracts(t *testing.T) {
	memory := uint64(256)
	wanted, err := xmlpatch.EditResources(agentEditOriginal, xmlpatch.ResourceEdit{MemoryMiB: &memory})
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []float64{1, 2} {
		digest := xmlpatch.Digest(wanted)
		if version == 2 {
			digest, err = xmlpatch.HardwareDigest(wanted)
			if err != nil {
				t.Fatal(err)
			}
		}
		input := map[string]any{"editVersion": version, "applyMode": "next-boot", "memoryMiB": float64(memory), "xmlSHA256": digest}
		edit, got, err := configurationXML(agentEditOriginal, input)
		if err != nil || got != wanted || edit.MemoryMiB == nil || *edit.MemoryMiB != memory {
			t.Fatalf("legacy v%.0f changed: %v", version, err)
		}
		match, err := configurationMatches(got, input)
		if err != nil || !match {
			t.Fatalf("legacy v%.0f mismatch: %v", version, err)
		}
		normalized := strings.Replace(got, `type="kvm"`, `type='kvm'`, 1)
		match, err = configurationMatches(normalized, input)
		if err != nil || match != (version == 2) {
			t.Fatalf("legacy v%.0f digest semantics changed: %v match=%v", version, err, match)
		}
	}
}
