package xmlpatch

import (
	"encoding/json"
	"encoding/xml"
	"virmill.local/core/internal/domain"
)

// GuestToolsLiveDigest binds every byte outside the unique agent target start
// tag. Within that tag only the connected/disconnected observation is ignored.
// This is a versioned guest-tools recipe comparison, never a general VM digest.
func GuestToolsLiveDigest(raw string) (string, error) {
	root, err := positionedXML(raw)
	if err != nil {
		return "", err
	}
	devices, err := onlyChild(root, "devices")
	if err != nil {
		return "", err
	}
	var target *positionedNode
	for _, part := range devices.parts {
		ch := part.child
		if ch == nil || ch.name.Local != "channel" {
			continue
		}
		for _, p := range ch.parts {
			t := p.child
			if t == nil || t.name.Local != "target" {
				continue
			}
			name, _ := t.attr("name")
			if name != "org.qemu.guest_agent.0" {
				continue
			}
			kind, _ := ch.attr("type")
			typeName, _ := t.attr("type")
			if target != nil || ch.name.Space != "" || t.name.Space != "" || kind != "unix" || typeName != "virtio" || len(ch.children("target")) != 1 || !t.onlyAttrs("type", "name", "state") || len(t.parts) != 0 {
				return "", domain.Fail("UNSUPPORTED_CAPABILITY", "guest agent channel is ambiguous or unsupported")
			}
			state, found := t.attr("state")
			if found && state != "connected" && state != "disconnected" {
				return "", domain.Fail("UNSUPPORTED_CAPABILITY", "guest agent connection state is unsupported")
			}
			target = t
		}
	}
	if target == nil {
		return Digest(raw), nil
	}
	attrs := []xml.Attr{}
	for _, attr := range target.attrs {
		if attr.Name != (xml.Name{Local: "state"}) {
			attrs = append(attrs, attr)
		}
	}
	b, err := json.Marshal([]any{raw[:target.start], attrs, raw[target.startEnd:]})
	if err != nil {
		return "", err
	}
	return Digest(string(b)), nil
}
