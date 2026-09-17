package tui

import (
	"strings"

	"virmill.local/core/internal/validation"
)

// acknowledgementText explains each protocol acknowledgement in plain words.
// Labels explain; they never add or grant one. The confirmation shows these
// sentences instead of checkboxes (ADR 0065), so every acknowledgement a
// planner can ask for needs one here, and a test enforces that.
var acknowledgementText = map[string]string{
	"guest-agent-host-access":             "Allow a high-trust management channel between this host and guest",
	"exclusive-configuration-writer":      "No other administrator is changing this VM configuration",
	"host-mutation":                       "Allow the reviewed changes on this host",
	"data-loss-delete-disks":              "Permanently delete only the listed disk files; this cannot be undone",
	"data-loss-delete-old-copy":           "Delete the disk's original copy once the VM uses the new one; this cannot be undone",
	"data-loss-hard-stop":                 "Cut power without a shutdown; unsaved guest data may be lost",
	"remove-vm-definition":                "Remove this VM's saved definition",
	"exclusive-lifecycle-writer":          "No other administrator is changing or starting these VMs",
	"exclusive-storage-writer":            "No other administrator or tool is changing this storage",
	"copy-managed-volumes":                "Create independent managed copies of the disks",
	"hand-over-prepared-copy":             "Release the prepared copy as its disks are copied, when it shares the pool's filesystem; if copying stops, import the original again",
	"new-vm-identity":                     "Give the new VM its own identity",
	"network-attachment":                  "Connect the VM to the reviewed networks",
	"new-firmware-state":                  "Create new firmware state; existing guest keys are not restored",
	"creation-device-policy":              "Use the reviewed controllers and device settings",
	"guest-agent-channel":                 "Allow the reviewed guest-agent management channel",
	"guest-admin-package-install":         "Allow administrator package installation inside this guest",
	"offline-sources":                     "The source images are not being used or changed",
	"source-preservation":                 "Keep the original source images untouched",
	"next-boot-only":                      "Apply these settings at the next boot",
	"watchdog-reset":                      "Allow the configured watchdog to reset the guest",
	"start-selected-vm":                   "Start this selected VM",
	"start-vm":                            "Start the VM as soon as it is created",
	"delete-prepared-copy":                "Delete the prepared copy and its work folder; the original source is kept",
	"guest-root-provisioning":             "Let cloud-init create the user and apply the reviewed settings at first boot",
	"rotate-guest-host-keys":              "Give the guest fresh SSH host keys",
	"guest-passwordless-sudo":             "Give the cloud user administrator access (sudo) without a password",
	"attach-readonly-media":               "Attach the installer media read-only",
	"review-network-exposure":             "Accept the reviewed network access",
	"write-import-artifacts":              "Save prepared copies of the images in your import folder",
	"offline-source-files":                "The source files are not open in another program, such as VirtualBox, while they are copied",
	"pool-overcommit":                     "The disk may be larger than the pool's free space, so the VM can run out of space as it writes",
	"replace-boot-order":                  "Replace the VM's boot order with the one shown",
	"eject-retain-media":                  "Eject the selected disc; its file is kept",
	"guest-reboot":                        "Restart the guest; programs running inside it are closed",
	"abandon-creation":                    "Close this unfinished VM creation for good; it will never become a working VM",
	"accept-observed-devices":             "Accept the VM's existing chipset devices as they are; nothing in the VM changes",
	"allow-active-jobs-to-finish":         "Let plugin tasks that are already running finish on the version they started with",
	"close-disk-addition":                 "Close the unfinished disk addition; it will not count as a completed change",
	"define-retained-volumes":             "Define the VM again from the disks already copied, without copying them again",
	"delete-new-volumes":                  "Delete the disk files this unfinished creation made; this cannot be undone",
	"encrypted-backup":                    "Create an encrypted backup in the repository you chose",
	"exclusive-external-writer":           "No other tool uses this VM or its files until this finishes",
	"exclusive-network-writer":            "No other administrator or tool is changing this network",
	"exclusive-offline-volume":            "Keep the VM stopped, and let nothing else write to this disk, until this finishes",
	"host-permission-change":              "Change who can read this disk file on this host",
	"inherit-recovery-resources":          "Take over the unfinished job so it can be closed safely",
	"local-storage-mutation":              "Write to the backup repository and working folders on this host",
	"network-firewall":                    "Add firewall rules on this host for this network",
	"network-host-access":                 "Let VMs on this network reach this host for addresses and name lookups",
	"persistent-disk-read-access":         "Let you read this disk's entire contents until you remove that access",
	"plugin-installation-change":          "Change which plugins are installed for you",
	"recovered-set-publication":           "Save the restored disks and settings on this host as a new recovery point; no VM is created",
	"guest-execution":                     "Run the setup commands shown inside the guest",
	"guest-resource-pressure":             "Take CPUs or memory away from the running guest, which can slow it down or stop programs inside it",
	"guest-host-key-binding":              "Trust the guest's SSH host key recorded for this VM",
	"non-root-guest-setup":                "Run the setup as the guest user, without administrator rights",
	"non-idempotent-recipe":               "This setup may not be safe to run twice; running it again can change the guest again",
	"new-restored-identity":               "Give the restored VM a new name and identity",
	"all-network-interfaces-disconnected": "Restore the VM with no network connections; add them afterwards",
	"offline-source-read":                 "Keep the VM stopped while its disks and settings are read",
	"private-recovery-state":              "Keep a private copy of the VM's disks, settings and firmware keys on this host",
	"write-plugin-artifact":               "Create new plugin developer files in the folder you chose",
	"retain-partial-volumes":              "Keep the disk files the unfinished creation left behind; nothing is deleted",
}

func humanAcknowledgement(id string) string {
	text := strings.ReplaceAll(id, "-", " ")
	if text != "" {
		text = strings.ToUpper(text[:1]) + text[1:]
	}
	return text
}

// plainAcknowledgement is the sentence the confirmation shows. One Virmill has
// no words for, such as a plugin's, keeps its identifier visible instead of
// looking explained.
func plainAcknowledgement(id string) (string, bool) {
	if text, ok := acknowledgementText[id]; ok {
		return text, true
	}
	return humanAcknowledgement(id) + " (" + id + ")", false
}

// acknowledgementLabel keeps the exact identifier beside the explanation, for
// messages that name one acknowledgement.
func acknowledgementLabel(id string) string {
	text, ok := acknowledgementText[id]
	if !ok {
		text = humanAcknowledgement(id)
	}
	return text + " [" + id + "]"
}

func (m Workspace) planIssueLines() []string {
	if m.Error == "" {
		return []string{}
	}
	lines := []string{"Submission needs attention", "", validation.SafeText(m.Error), ""}
	if strings.Contains(m.Error, "helper-key.pem") {
		lines = append(lines, "A host administrator must finish helper setup before this action can run.", "See the installed network-helper.md and managed-volume-access.md guides.")
	}
	return append(lines, "Your settings and reviewed plan are kept. Check Jobs before trying again; an accepted job may still be running.", "PgUp/PgDn reads the complete issue and plan. Esc returns to your settings.", "")
}

func bulletLines(text string, width int) []string {
	out := []string{}
	for row, line := range wrap(validation.SafeText(text), max(1, width-4)) {
		if row == 0 {
			out = append(out, "  - "+line)
		} else {
			out = append(out, "    "+line)
		}
	}
	return out
}

// confirmationLines is the one confirmation every change goes through (ADR
// 0065): what will happen, everything the user agrees to in plain words, then
// a single Confirm. Identifiers, digests and resource keys stay in the
// complete plan, one key away.
func (m Workspace) confirmationLines(width, height int) []string {
	lines := m.planIssueLines()
	lines = append(lines, planConfirmation(*m.Plan, width)...)
	pool, poolAcks := m.newVMPoolLines(width)
	if len(pool) > 0 {
		lines = append(append(lines, pool...), "")
	}
	lines = append(lines, m.chainOfferLines(width)...)
	lines = append(lines, "By confirming, you agree that:")
	agreed := append(append([]string{}, poolAcks...), m.Plan.Acknowledgements...)
	if m.ChainOffer != nil {
		agreed = append(agreed, m.ChainOffer.Extras...)
	}
	seen := map[string]bool{}
	for _, ack := range agreed {
		if seen[ack] {
			continue
		}
		seen[ack] = true
		text, _ := plainAcknowledgement(ack)
		lines = append(lines, bulletLines(text, width)...)
	}
	if len(seen) == 0 {
		lines = append(lines, bulletLines("Nothing beyond the changes described above", width)...)
	}
	if len(m.Plan.Risks) > 0 {
		lines = append(lines, "", "Good to know:")
		for _, risk := range m.Plan.Risks {
			lines = append(lines, bulletLines(risk, width)...)
		}
	}
	lines = append(lines, "", "[ Confirm ]")
	return pageLines(lines, width, height, m.Offset)
}
