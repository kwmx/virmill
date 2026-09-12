package xmlpatch

import (
	"strings"
	"testing"
)

const toolsBindingTarget = `<target type="virtio" name="org.qemu.guest_agent.0" state="disconnected"/>`
const toolsBindingChannel = `<channel type="unix"><source mode="bind" path="/run/libvirt/qemu/channel/7-fixture/org.qemu.guest_agent.0"/>` + toolsBindingTarget + `<alias name="channel0"/><address type="virtio-serial" controller="0" bus="0" port="1"/></channel>`
const toolsBindingXML = `<?xml version="1.0"?><!--keep-prolog--><domain type="kvm" id="7" xmlns:opaque="urn:retained"><name>fixture</name><uuid>570c4866-5b5e-4583-817c-602538d19b9c</uuid><metadata><opaque:policy>keep exact &#x20; text</opaque:policy></metadata><memory unit="KiB">262144</memory><devices><disk type="file" device="disk"><driver name="qemu" type="qcow2"/><source file="/original/disk.qcow2"/><target dev="vda" bus="virtio"/></disk><interface type="network"><source network="private-lab"/><mac address="52:54:00:01:02:03"/><model type="virtio"/></interface><!--keep device comment-->` + toolsBindingChannel + `<channel type="spicevmc"><target type="virtio" name="com.redhat.spice.0" state="connected"/></channel><opaque:unknown setting="untouched"/></devices></domain><!--keep-epilog-->`

func TestGuestToolsLiveDigestNormalizesOnlyAgentConnectionObservation(t *testing.T) {
	want, err := GuestToolsLiveDigest(toolsBindingXML)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		toolsBindingTarget,
		strings.Replace(toolsBindingTarget, `state="disconnected"`, `state="connected"`, 1),
		strings.Replace(toolsBindingTarget, ` state="disconnected"`, "", 1),
	} {
		got, err := GuestToolsLiveDigest(strings.Replace(toolsBindingXML, toolsBindingTarget, target, 1))
		if err != nil || got != want {
			t.Fatalf("connection observation changed binding: %s, %v", target, err)
		}
	}
}

func TestGuestToolsLiveDigestPreservesOtherConfigurationAndOpaqueBytes(t *testing.T) {
	want, err := GuestToolsLiveDigest(toolsBindingXML)
	if err != nil {
		t.Fatal(err)
	}
	changes := map[string][2]string{
		"runtime domain identity":    {`id="7"`, `id="8"`},
		"domain type":                {`type="kvm"`, `type="qemu"`},
		"VM UUID":                    {`570c4866-5b5e-4583-817c-602538d19b9c`, `570c4866-5b5e-4583-817c-602538d19b9d`},
		"VM name":                    {`<name>fixture</name>`, `<name>other</name>`},
		"memory":                     {`262144`, `524288`},
		"disk source":                {`/original/disk.qcow2`, `/different/disk.qcow2`},
		"disk driver":                {`type="qcow2"`, `type="raw"`},
		"disk bus":                   {`dev="vda" bus="virtio"`, `dev="vda" bus="scsi"`},
		"NIC network":                {`private-lab`, `public-lan`},
		"NIC MAC":                    {`52:54:00:01:02:03`, `52:54:00:01:02:04`},
		"NIC model":                  {`<model type="virtio"/>`, `<model type="e1000"/>`},
		"agent socket":               {`/run/libvirt/qemu/channel/7-fixture/`, `/run/libvirt/qemu/channel/8-other/`},
		"agent alias":                {`name="channel0"`, `name="channel1"`},
		"agent controller":           {`controller="0"`, `controller="1"`},
		"agent port":                 {`port="1"`, `port="2"`},
		"other channel state":        {`name="com.redhat.spice.0" state="connected"`, `name="com.redhat.spice.0" state="disconnected"`},
		"unknown attribute":          {`setting="untouched"`, `setting="changed"`},
		"unknown text":               {`keep exact &#x20; text`, `keep different &#x20; text`},
		"namespace identity":         {`urn:retained`, `urn:changed`},
		"comment":                    {`keep device comment`, `changed device comment`},
		"prolog":                     {`keep-prolog`, `changed-prolog`},
		"epilog":                     {`keep-epilog`, `changed-epilog`},
		"outside whitespace":         {`</memory><devices>`, "</memory>\n<devices>"},
		"outside attribute spelling": {`id="7"`, `id='7'`},
	}
	for name, replacement := range changes {
		t.Run(name, func(t *testing.T) {
			raw := strings.Replace(toolsBindingXML, replacement[0], replacement[1], 1)
			if raw == toolsBindingXML {
				t.Fatal("fixture mutation did not apply")
			}
			got, err := GuestToolsLiveDigest(raw)
			if err != nil {
				t.Fatal("valid changed XML could not be compared", err)
			}
			if got == want {
				t.Fatal("unrelated configuration change was normalized away")
			}
		})
	}
}

func TestGuestToolsLiveDigestDetectsRemovedAndRenamedAgent(t *testing.T) {
	want, err := GuestToolsLiveDigest(toolsBindingXML)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"channel removed": strings.Replace(toolsBindingXML, toolsBindingChannel, "", 1),
		"target removed":  strings.Replace(toolsBindingXML, toolsBindingTarget, "", 1),
		"agent renamed":   strings.Replace(toolsBindingXML, `name="org.qemu.guest_agent.0"`, `name="org.qemu.guest_agent.other"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := GuestToolsLiveDigest(raw)
			if err != nil {
				t.Fatal(err)
			}
			if got == want {
				t.Fatal("agent removal or identity change was hidden")
			}
			if got != Digest(raw) {
				t.Fatal("domain without recognized agent must retain exact raw binding")
			}
		})
	}
}

func TestGuestToolsLiveDigestRejectsMalformedAndAmbiguousTargets(t *testing.T) {
	mutateTarget := func(target string) string { return strings.Replace(toolsBindingXML, toolsBindingTarget, target, 1) }
	tests := map[string]string{
		"duplicate state":             mutateTarget(strings.Replace(toolsBindingTarget, `state="disconnected"`, `state="disconnected" state="connected"`, 1)),
		"duplicate name":              mutateTarget(strings.Replace(toolsBindingTarget, `name="org.qemu.guest_agent.0"`, `name="org.qemu.guest_agent.0" name="different"`, 1)),
		"duplicate channel":           strings.Replace(toolsBindingXML, "</devices>", toolsBindingChannel+"</devices>", 1),
		"two target children":         mutateTarget(toolsBindingTarget + `<target type="virtio" name="another"/>`),
		"foreign target element":      mutateTarget(strings.Replace(toolsBindingTarget, "<target ", "<opaque:target ", 1)),
		"foreign agent channel":       strings.Replace(toolsBindingXML, toolsBindingChannel, strings.Replace(strings.Replace(toolsBindingChannel, "<channel ", "<opaque:channel ", 1), "</channel>", "</opaque:channel>", 1), 1),
		"foreign state attribute":     mutateTarget(strings.Replace(toolsBindingTarget, `state="disconnected"`, `opaque:state="disconnected"`, 1)),
		"state namespace shadow":      mutateTarget(strings.Replace(toolsBindingTarget, `state="disconnected"`, `state="disconnected" opaque:state="connected"`, 1)),
		"unknown target attribute":    mutateTarget(strings.Replace(toolsBindingTarget, "/>", ` extra="changed"/>`, 1)),
		"unsupported state":           mutateTarget(strings.Replace(toolsBindingTarget, "disconnected", "invalid", 1)),
		"empty state":                 mutateTarget(strings.Replace(toolsBindingTarget, "disconnected", "", 1)),
		"wrong channel type":          strings.Replace(toolsBindingXML, `<channel type="unix">`, `<channel type="tcp">`, 1),
		"wrong target type":           mutateTarget(strings.Replace(toolsBindingTarget, `type="virtio"`, `type="xen"`, 1)),
		"target nested content":       mutateTarget(strings.Replace(toolsBindingTarget, "/>", `><source file="unexpected"/></target>`, 1)),
		"target comment":              mutateTarget(strings.Replace(toolsBindingTarget, "/>", `><!--unexpected--></target>`, 1)),
		"duplicate devices":           strings.Replace(toolsBindingXML, "</domain>", "<devices/></domain>", 1),
		"duplicate runtime attribute": strings.Replace(toolsBindingXML, `id="7"`, `id="7" id="8"`, 1),
		"directive":                   `<!DOCTYPE domain>` + toolsBindingXML,
		"malformed XML":               strings.TrimSuffix(toolsBindingXML, "</domain><!--keep-epilog-->"),
		"size bound":                  strings.Repeat("x", Limit+1),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if digest, err := GuestToolsLiveDigest(raw); err == nil {
				t.Fatalf("invalid XML accepted with digest %q", digest)
			}
		})
	}
}
