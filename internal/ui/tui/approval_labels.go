package tui

import "strings"

// Labels explain exact protocol acknowledgements; they never add or grant one.
// Keep the stable ID alongside the explanation for unfamiliar and plugin scopes.
func acknowledgementLabel(id string) string {
	text := map[string]string{
		"guest-agent-host-access":        "Allow a high-trust management channel between this host and guest",
		"exclusive-configuration-writer": "No other administrator is changing this VM configuration",
		"host-mutation":                  "Allow the reviewed changes on this host",
		"data-loss-delete-disks":         "Permanently delete only the listed disk files; this cannot be undone",
		"remove-vm-definition":           "Remove this VM's saved definition",
		"exclusive-lifecycle-writer":     "No other administrator is changing or starting these VMs",
		"exclusive-storage-writer":       "No other administrator or tool is changing this storage",
		"copy-managed-volumes":           "Create independent managed copies of the disks",
		"new-vm-identity":                "Give the new VM its own identity",
		"network-attachment":             "Connect the VM to the reviewed networks",
		"new-firmware-state":             "Create new firmware state; existing guest keys are not restored",
		"creation-device-policy":         "Use the reviewed controllers and device settings",
		"guest-agent-channel":            "Allow the reviewed guest-agent management channel",
		"guest-admin-package-install":    "Allow administrator package installation inside this guest",
		"offline-sources":                "The source images are not being used or changed",
		"source-preservation":            "Keep the original source images untouched",
		"next-boot-only":                 "Apply these settings at the next boot",
		"watchdog-reset":                 "Allow the configured watchdog to reset the guest",
		"start-selected-vm":              "Start this selected VM",
		"review-network-exposure":        "Accept the reviewed network access",
	}[id]
	if text == "" {
		text = strings.ReplaceAll(id, "-", " ")
		if text != "" {
			text = strings.ToUpper(text[:1]) + text[1:]
		}
	}
	return text + " [" + id + "]"
}
func (m Workspace) confirmationLines(width, height int) []string {
	lines := []string{"Confirm reviewed changes", "Check each consequence, then choose Apply.", "Plan: " + m.Plan.ID, ""}
	selectedLine := 0
	for i, ack := range m.Plan.Acknowledgements {
		prefix := "  [ ] "
		if i < len(m.Approved) && m.Approved[i] {
			prefix = "  [x] "
		}
		if i == m.AckIndex {
			prefix = ">" + prefix[1:]
			selectedLine = len(lines)
		}
		// Wrap prose before adding the checkbox, so layout detection does not
		// mistake confirmation text for a preformatted table.
		label := strings.TrimSuffix(acknowledgementLabel(ack), " ["+ack+"]")
		for row, text := range wrap(label, max(1, width-6)) {
			if row == 0 {
				lines = append(lines, prefix+text)
			} else {
				lines = append(lines, "      "+text)
			}
		}
		lines = append(lines, wrap("      ["+ack+"]", width)...)
	}
	lines = append(lines, "")
	prefix := "  "
	if m.AckIndex == len(m.Approved) {
		prefix = "> "
		selectedLine = len(lines)
	}
	lines = append(lines, prefix+"[ Apply reviewed plan ]", "", "Esc returns to the full plan and exact resource identities.")
	offset := m.Offset
	if selectedLine < offset {
		offset = selectedLine
	}
	if selectedLine >= offset+max(1, height-2) {
		offset = selectedLine - max(1, height-3)
	}
	return pageLines(lines, width, height, offset)
}
