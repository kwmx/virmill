package app

import "virmill.local/core/internal/backend/xmlpatch"

func hardwareRequest(input map[string]any) bool {
	_, boot := input["bootOrder"]
	_, media := input["ejectMedia"]
	return boot || media
}
func configurationVersion(operation string, input map[string]any) bool {
	return operation == "vm.configure-resources" && input["editVersion"] == float64(1) && !hardwareRequest(input) || operation == "vm.configure-hardware" && input["editVersion"] == float64(2) && hardwareRequest(input)
}
func configurationPreviewDigest(before string, input map[string]any) (string, error) {
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
