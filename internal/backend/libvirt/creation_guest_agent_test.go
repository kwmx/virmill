//go:build linux && cgo

package libvirt

import (
	"strings"
	"testing"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
)

func guestAgentXMLFixture(t *testing.T) (string, *domain.CreationDevicePolicy) {
	t.Helper()
	target, volumes := creationFixture()
	policy, err := domain.DefaultCreationDevices(target.Spec.Machine)
	if err != nil {
		t.Fatal(err)
	}
	target.Spec.DevicePolicy = policy
	target.Spec.GuestAgent = true
	wanted, err := creationXML(target, volumes, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return wanted, policy
}
func TestCreationGuestAgentIsOptInAndUsesAutomaticSocket(t *testing.T) {
	target, volumes := creationFixture()
	old, err := creationXML(target, volumes, "binding")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(old, "<channel") || strings.Contains(old, "virtio-serial") {
		t.Fatal("legacy creation changed")
	}
	target.Spec.GuestAgent = true
	if _, err = creationXML(target, volumes, "binding"); err == nil {
		t.Fatal("channel missing explicit topology policy")
	}
	wanted, policy := guestAgentXMLFixture(t)
	if !strings.Contains(wanted, creationGuestAgentDevices) {
		t.Fatal("channel/controller intent absent")
	}
	if err = matchesCreationPolicy(wanted, wanted, policy); err != nil {
		t.Fatal(err)
	}
	withAliases := strings.Replace(wanted, `<controller type="virtio-serial" index="0" model="virtio"/>`, `<controller type="virtio-serial" index="0" model="virtio"><alias name="virtio-serial0"/></controller>`, 1)
	withAliases = strings.Replace(withAliases, `</channel>`, `<alias name="channel0"/></channel>`, 1)
	if err = matchesCreationPolicy(wanted, withAliases, policy); err != nil {
		t.Fatal("known aliases rejected", err)
	}
}
func TestCreationGuestAgentRejectsUnreviewedChannelSemantics(t *testing.T) {
	wanted, policy := guestAgentXMLFixture(t)
	for name, edit := range map[string][2]string{
		"explicit socket":                {`<channel type="unix">`, `<channel type="unix"><source mode="bind" path="/run/libvirt/qemu/channel/domain-1-vm/org.qemu.guest_agent.0"/>`},
		"foreign socket":                 {`<channel type="unix">`, `<channel type="unix"><source mode="bind" path="/tmp/unreviewed.sock"/>`},
		"empty source":                   {`<channel type="unix">`, `<channel type="unix"><source/>`},
		"TCP listener":                   {`<channel type="unix">`, `<channel type="tcp">`},
		"guest target":                   {`name="org.qemu.guest_agent.0"`, `name="other.agent"`},
		"duplicate target":               {`</channel>`, `<target type="virtio" name="org.qemu.guest_agent.0"/></channel>`},
		"duplicate channel":              {`</devices>`, `<channel type="unix"><target type="virtio" name="other.agent"/></channel></devices>`},
		"duplicate controller":           {`</devices>`, `<controller type="virtio-serial" index="0" model="virtio"/></devices>`},
		"wrong controller":               {`type="virtio-serial" controller="0"`, `type="virtio-serial" controller="1"`},
		"wrong port":                     {`bus="0" port="1"`, `bus="0" port="2"`},
		"hidden state":                   {`name="org.qemu.guest_agent.0"`, `name="org.qemu.guest_agent.0" state="connected"`},
		"duplicate attribute":            {`name="org.qemu.guest_agent.0"`, `name="org.qemu.guest_agent.0" name="other"`},
		"channel namespace":              {`<channel type="unix">`, `<channel xmlns="urn:unknown" type="unix">`},
		"unexpected driver":              {`</channel>`, `<driver queues="4"/></channel>`},
		"unexpected alias":               {`</channel>`, `<alias name="unreviewed"/></channel>`},
		"duplicate alias":                {`</channel>`, `<alias name="channel0"/><alias name="channel0"/></channel>`},
		"controller ports":               {`model="virtio"/>`, `model="virtio" ports="32"/>`},
		"controller PCI escape":          {`model="virtio"/>`, `model="virtio"><address type="pci" domain="0x0001" bus="0x00" slot="0x06" function="0x0"/></controller>`},
		"controller nonexistent PCI bus": {`model="virtio"/>`, `model="virtio"><address type="pci" domain="0x0000" bus="0xff" slot="0x06" function="0x0"/></controller>`},
		"controller extra child":         {`model="virtio"/>`, `model="virtio"><driver iommu="on"/></controller>`},
	} {
		t.Run(name, func(t *testing.T) {
			changed := strings.Replace(wanted, edit[0], edit[1], 1)
			if changed == wanted {
				t.Fatal("fixture edit not applied")
			}
			if err := matchesCreationPolicy(wanted, changed, policy); err == nil {
				t.Fatal("unreviewed channel accepted")
			}
		})
	}
	target, volumes := creationFixture()
	target.Spec.DevicePolicy = policy
	legacy, err := creationXML(target, volumes, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err = matchesCreationPolicy(legacy, wanted, policy); err == nil {
		t.Fatal("unexpected channel certified against legacy receipt")
	}
}
func TestCreationGuestAgentNativeSimulatedRoundTrip(t *testing.T) {
	c, err := native.NewConnect("test:///default")
	if err != nil {
		t.Skipf("native simulated parser unavailable: %v", err)
	}
	defer c.Close()
	wanted, policy := guestAgentXMLFixture(t)
	d, err := c.DomainDefineXMLFlags(wanted, native.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Free()
	defer d.Undefine()
	observed, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
	if err != nil {
		t.Fatal(err)
	}
	if err = matchesCreationPolicy(wanted, observed, policy); err != nil {
		t.Fatal(err, observed)
	}
	t.Log("native in-memory XML parser only; QEMU channel transport and in-guest agent remain untested")
}
