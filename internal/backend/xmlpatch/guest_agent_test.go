package xmlpatch

import (
	"strings"
	"testing"
)

const guestAgentBase = `<?xml version="1.0"?><!--retain--><domain type='kvm' xmlns:q='urn:opaque'><name>guest</name><uuid>570c4866-5b5e-4583-817c-602538d19b9c</uuid><metadata><q:policy label='keep'> exact &#x20; value </q:policy></metadata><os><type arch='x86_64' machine='pc'>hvm</type></os><devices><disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='/original/disk.qcow2'/><target dev='vda' bus='virtio'/><q:retained custom='yes'>unchanged</q:retained></disk><!--device comment--><serial type='pty'><target port='0'/></serial><console type='pty'><target type='serial' port='0'/></console></devices></domain>`
const guestAgentControllerXML = `<controller type="virtio-serial" index="0" model="virtio"/>`
const guestAgentChannelXML = `<channel type="unix"><target type="virtio" name="org.qemu.guest_agent.0"/><address type="virtio-serial" controller="0" bus="0" port="1"/></channel>`

func guestAgentDeviceXML(fragment string) string {
	return strings.Replace(guestAgentBase, "</devices>", fragment+"</devices>", 1)
}
func TestGuestAgentEnablePreservesOpaqueOriginalAndIsIdempotent(t *testing.T) {
	view, err := InspectGuestAgent(guestAgentBase)
	if err != nil {
		t.Fatal(err)
	}
	if view.Present || !view.CanEnable || !view.AddsController || view.ControllerIndex != 0 || view.Port != 1 {
		t.Fatalf("%+v", view)
	}
	out, err := EnableGuestAgent(guestAgentBase)
	if err != nil {
		t.Fatal(err)
	}
	fragment := guestAgentControllerXML + guestAgentChannelXML
	if out != guestAgentDeviceXML(fragment) {
		t.Fatal("original bytes changed outside exact insertion")
	}
	if strings.Contains(out, "<source mode=") || strings.Contains(out, "/channel/") {
		t.Fatal("socket path manufactured")
	}
	configured, err := InspectGuestAgent(out)
	if err != nil || !configured.Present || configured.CanEnable {
		t.Fatalf("%+v %v", configured, err)
	}
	again, err := EnableGuestAgent(out)
	if err != nil || again != out {
		t.Fatal("safe existing channel was modified", err)
	}
	clone, err := removeGuestAgentAdditions(out, view)
	if err != nil || clone != guestAgentBase {
		t.Fatal("surgical clone changed original bytes", err)
	}
}
func TestGuestAgentEnableEmptyDevicesAndPortSelection(t *testing.T) {
	for _, raw := range []string{`<domain><devices/></domain>`, `<domain><devices /></domain>`} {
		out, err := EnableGuestAgent(raw)
		if err != nil {
			t.Fatal(err)
		}
		view, err := InspectGuestAgent(out)
		if err != nil || !view.Present || view.Port != 1 {
			t.Fatalf("%s %+v %v", out, view, err)
		}
	}
	controller := `<controller type="virtio-serial" index="2" model="virtio" ports="4"><alias name="virtio-serial2"/><address type="pci" domain="0x0000" bus="0x00" slot="0x07" function="0x0"/></controller>`
	other := `<channel type="spicevmc"><target type="virtio" name="com.redhat.spice.0"/><address type="virtio-serial" controller="2" bus="0" port="1"/></channel>`
	raw := guestAgentDeviceXML(controller + other)
	view, err := InspectGuestAgent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !view.CanEnable || view.AddsController || view.ControllerIndex != 2 || view.Port != 2 {
		t.Fatalf("%+v", view)
	}
	out, err := EnableGuestAgent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, controller+other) || !strings.Contains(out, `controller="2" bus="0" port="2"`) {
		t.Fatal("existing channel or controller changed")
	}
	// All nonreserved ports occupied: do not move a channel or add an unreviewed
	// controller to hide exhaustion of an existing explicit topology.
	full := strings.Replace(raw, `ports="4"`, `ports="2"`, 1)
	view, err = InspectGuestAgent(full)
	if err != nil || view.CanEnable || view.Reason == "" {
		t.Fatalf("full controller %+v %v", view, err)
	}
	if _, err = EnableGuestAgent(full); err == nil {
		t.Fatal("full controller unexpectedly changed")
	}
}
func TestGuestAgentRefusesAmbiguousOrExternalConfiguration(t *testing.T) {
	base := guestAgentControllerXML + guestAgentChannelXML
	for name, fragment := range map[string]string{
		"duplicate target":              base + guestAgentChannelXML,
		"external socket":               strings.Replace(base, `<target type="virtio" name="org.qemu.guest_agent.0"/>`, `<source mode="bind" path="/external/socket"/><target type="virtio" name="org.qemu.guest_agent.0"/>`, 1),
		"source without path":           strings.Replace(base, `<channel type="unix">`, `<channel type="unix"><source mode="bind"/>`, 1),
		"tcp agent":                     strings.Replace(base, `<channel type="unix">`, `<channel type="tcp">`, 1),
		"wrong target type":             strings.Replace(base, `<target type="virtio" name=`, `<target type="guestfwd" name=`, 1),
		"opaque channel":                strings.Replace(base, `<channel type="unix">`, `<channel type="unix"><reconnect enabled="yes"/>`, 1),
		"target state":                  strings.Replace(base, `name="org.qemu.guest_agent.0"`, `name="org.qemu.guest_agent.0" state="connected"`, 1),
		"duplicate target nodes":        strings.Replace(base, `</channel>`, `<target type="virtio" name="other"/></channel>`, 1),
		"duplicate alias":               strings.Replace(base, `</channel>`, `<alias name="channel0"/><alias name="channel0"/></channel>`, 1),
		"foreign channel":               strings.Replace(base, `<channel type="unix">`, `<channel xmlns="urn:foreign" type="unix">`, 1),
		"foreign target":                strings.Replace(base, `<target type="virtio" name=`, `<target xmlns="urn:foreign" type="virtio" name=`, 1),
		"duplicate controller":          guestAgentControllerXML + base,
		"unknown controller attributes": strings.Replace(base, `index="0" model="virtio"`, `index="0" model="virtio" experimental="yes"`, 1),
		"dependent controller child":    strings.Replace(base, guestAgentControllerXML, `<controller type="virtio-serial" index="0" model="virtio"><driver queues="8"/></controller>`, 1),
		"absent controller":             guestAgentChannelXML,
		"implicit port":                 strings.Replace(base, `<address type="virtio-serial" controller="0" bus="0" port="1"/>`, "", 1),
		"out of range":                  strings.Replace(base, `port="1"`, `port="31"`, 1),
		"reserved port":                 strings.Replace(base, `port="1"`, `port="0"`, 1),
		"wrong bus":                     strings.Replace(base, `bus="0" port="1"`, `bus="1" port="1"`, 1),
		"duplicate port":                base + strings.Replace(guestAgentChannelXML, GuestAgentTarget, "other.channel", 1),
		"unknown alias":                 strings.Replace(base, `</channel>`, `<alias name="--unexpected"/></channel>`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			raw := guestAgentDeviceXML(fragment)
			if _, err := InspectGuestAgent(raw); err == nil {
				t.Fatal("unsafe configuration inspected as supported")
			}
			if out, err := EnableGuestAgent(raw); err == nil || out != "" {
				t.Fatal("unsafe configuration modified")
			}
		})
	}
	for _, raw := range []string{"", `<domain/><domain/>`, `<!DOCTYPE x><domain><devices/></domain>`, `<domain><devices/><devices/></domain>`, `<domain><devices type="a" type="b"/></domain>`, strings.Repeat("x", Limit+1)} {
		if _, err := EnableGuestAgent(raw); err == nil {
			t.Fatal("invalid XML accepted")
		}
	}
}
func TestGuestAgentDigestAllowsOnlyExpectedNativeAdditions(t *testing.T) {
	proposal, err := EnableGuestAgent(guestAgentBase)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := GuestAgentDigest(proposal, 0, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	observed := strings.Replace(proposal, guestAgentControllerXML, `<controller model="virtio" index="0" type="virtio-serial"><alias name="virtio-serial0"/><address type="pci" domain="0x0000" bus="0x00" slot="0x06" function="0x0"/></controller>`, 1)
	observed = strings.Replace(observed, `</channel>`, `<alias name="channel0"/></channel>`, 1)
	got, err := GuestAgentDigest(observed, 0, 1, true)
	if err != nil || got != expected {
		t.Fatalf("native defaults differ %v %s %s", err, got, expected)
	}
	for name, raw := range map[string]string{
		"colliding PCI":             strings.Replace(observed, `</disk>`, `<address type="pci" domain="0x0000" bus="0x00" slot="0x06" function="0x0"/></disk>`, 1),
		"old source":                strings.Replace(observed, "/original/disk.qcow2", "/other/disk.qcow2", 1),
		"opaque text":               strings.Replace(observed, " exact &#x20; value ", " different ", 1),
		"old comment":               strings.Replace(observed, "device comment", "modified", 1),
		"extra PCI controller":      strings.Replace(observed, "</devices>", `<controller type="pci" index="9" model="pcie-root-port"/></devices>`, 1),
		"wrong alias":               strings.Replace(observed, `name="channel0"`, `name="channel7"`, 1),
		"wrong controller alias":    strings.Replace(observed, `name="virtio-serial0"`, `name="virtio-serial7"`, 1),
		"controller vector policy":  strings.Replace(observed, `model="virtio" index="0"`, `model="virtio" vectors="4" index="0"`, 1),
		"unknown new PCI attribute": strings.Replace(observed, `slot="0x06"`, `slot="0x06" multifunction="on"`, 1),
		"out of range PCI":          strings.Replace(observed, `slot="0x06"`, `slot="0x20"`, 1),
		"wrong PCI function":        strings.Replace(observed, `function="0x0"`, `function="0x1"`, 1),
		"external source":           strings.Replace(observed, `<channel type="unix">`, `<channel type="unix"><source mode="bind" path="/external/socket"/>`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			digest, err := GuestAgentDigest(raw, 0, 1, true)
			if err == nil && digest == expected {
				t.Fatal("unreviewed XML matched proposal")
			}
		})
	}
	for _, allocation := range []struct {
		controller, port uint
		adds             bool
	}{{0, 0, true}, {1, 1, true}, {0, 2, true}, {256, 1, false}} {
		if _, err := GuestAgentDigest(proposal, allocation.controller, allocation.port, allocation.adds); err == nil {
			t.Fatal("incorrect reviewed allocation accepted")
		}
	}
}
func TestGuestAgentDigestNeverNormalizesExistingController(t *testing.T) {
	original := guestAgentDeviceXML(`<controller type="virtio-serial" index="0" model="virtio"><alias name="virtio-serial0"/><address type="pci" domain="0x0000" bus="0x00" slot="0x07" function="0x0"/></controller>`)
	proposal, err := EnableGuestAgent(original)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := GuestAgentDigest(proposal, 0, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	altered := strings.Replace(proposal, `slot="0x07"`, `slot="0x08"`, 1)
	got, err := GuestAgentDigest(altered, 0, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if got == expected {
		t.Fatal("existing controller placement change was normalized away")
	}
}
