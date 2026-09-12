package xmlpatch

import "virmill.local/core/internal/domain"

// ParseGuestAgentInput validates the versioned, immutable channel-only recipe.
// Allocation coordinates are generated from observed XML, never user flags.
func ParseGuestAgentInput(input map[string]any) (GuestAgentChannelView, error) {
	var view GuestAgentChannelView
	fail := func() (GuestAgentChannelView, error) {
		return view, domain.Fail("INVALID_INPUT", "invalid reviewed guest-agent channel recipe")
	}
	if input["editVersion"] != float64(3) || input["enableGuestAgent"] != true || input["applyMode"] != "next-boot" {
		return fail()
	}
	allowed := map[string]bool{"editVersion": true, "enableGuestAgent": true, "applyMode": true, "agentController": true, "agentPort": true, "agentAddsController": true, "vmID": true, "editBeforeFingerprint": true, "xmlSHA256": true}
	for key := range input {
		if !allowed[key] {
			return fail()
		}
	}
	c, ok := input["agentController"].(float64)
	if !ok || c < 0 || c > 255 || float64(uint(c)) != c {
		return fail()
	}
	p, ok := input["agentPort"].(float64)
	if !ok || p < 1 || p > 255 || float64(uint(p)) != p {
		return fail()
	}
	adds, ok := input["agentAddsController"].(bool)
	if !ok || adds && c != 0 {
		return fail()
	}
	view.ControllerIndex, view.Port, view.AddsController, view.CanEnable = uint(c), uint(p), adds, true
	return view, nil
}
