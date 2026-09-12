package app

import (
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
)

func hardwareRequest(input map[string]any) bool {
	_, boot := input["bootOrder"]
	_, media := input["ejectMedia"]
	return boot || media
}
func guestAgentRequest(input map[string]any) bool {
	_, ok := input["enableGuestAgent"]
	return ok
}
func configurationVersion(operation string, input map[string]any) bool {
	if guestAgentRequest(input) {
		return operation == "vm.configure-guest-agent" && input["editVersion"] == float64(3) && !hardwareRequest(input)
	}
	return operation == "vm.configure-resources" && input["editVersion"] == float64(1) && !hardwareRequest(input) || operation == "vm.configure-hardware" && input["editVersion"] == float64(2) && hardwareRequest(input)
}
func configurationPreviewDigest(before string, input map[string]any) (string, error) {
	if input["editVersion"] == float64(3) {
		view, err := xmlpatch.ParseGuestAgentInput(input)
		if err != nil {
			return "", err
		}
		observed, err := xmlpatch.InspectGuestAgent(before)
		if err != nil {
			return "", err
		}
		if observed.Present || !observed.CanEnable || observed.ControllerIndex != view.ControllerIndex || observed.Port != view.Port || observed.AddsController != view.AddsController {
			return "", domain.Fail("STALE_PLAN", "Guest-agent channel configuration changed; review again")
		}
		after, err := xmlpatch.EnableGuestAgent(before)
		if err != nil {
			return "", err
		}
		return xmlpatch.GuestAgentDigest(after, view.ControllerIndex, view.Port, view.AddsController)
	}
	if input["editVersion"] == float64(2) {
		edit, err := xmlpatch.ParseHardwareInput(input)
		if err != nil {
			return "", err
		}
		after, err := xmlpatch.EditHardware(before, edit)
		if err != nil {
			return "", err
		}
		return xmlpatch.HardwareDigest(after)
	}
	edit, err := resourceEdit(input)
	if err != nil {
		return "", err
	}
	after, err := xmlpatch.EditResources(before, edit)
	if err != nil {
		return "", err
	}
	return xmlpatch.Digest(after), nil
}

// GuestAgentConnection reports observed configuration and state together.
// It does not claim guest software is installed or responsive.
type GuestAgentConnection struct {
	xmlpatch.GuestAgentChannelView
	State          string `json:"state"`
	HasManagedSave bool   `json:"hasManagedSave"`
}
