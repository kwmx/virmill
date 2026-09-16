package tui

import "virmill.local/core/internal/ui"

// Keep task language independent of CLI spelling. This map is deliberately
// exhaustive: new registered actions need an intentional place in the UI.
var actionText = map[string]struct{ label, description string }{
	"import sources":                {"Browse prepared images", "Continue VM setup from a completed image preparation."},
	"vm creation options":           {"View supported hardware", "Inspect this host's CPU, memory and firmware choices."},
	"plugin call":                   {"Run plugin action", "Choose a plugin action and the VMs it may read."},
	"plugin result":                 {"View plugin action result", "Read the saved result of a plugin job."},
	"plugin new":                    {"Create plugin project", "Generate Go plugin source in a new folder."},
	"plugin pack":                   {"Package plugin", "Build a signed plugin package using your signing key."},
	"plugin list":                   {"Browse installed plugins", "See installed plugins and their active versions."},
	"plugin show":                   {"View plugin details", "Inspect plugin versions, trust and activation state."},
	"plugin install":                {"Install plugin", "Review a signed package and its permissions before installation."},
	"plugin update":                 {"Update plugin", "Review a new version; keep the previous version for rollback."},
	"plugin enable":                 {"Enable plugin", "Allow supported plugin actions after permission checks."},
	"plugin disable":                {"Disable plugin", "Block new plugin actions while keeping its data."},
	"plugin remove":                 {"Remove plugin", "Remove the active installation; keep its data and created resources."},
	"plugin rollback":               {"Restore previous plugin version", "Activate a retained version without migrating plugin data."},
	"plugin permissions show":       {"View plugin permissions", "Compare requested permissions with the scopes already granted."},
	"plugin permissions grant":      {"Grant plugin permissions", "Review additional scopes; the plugin stays disabled until reviewed."},
	"plugin permissions revoke":     {"Revoke plugin permissions", "Remove selected scopes and block new plugin actions."},
	"plugin validate":               {"Validate plugin manifest", "Check a development manifest without trusting or installing it."},
	"plugin test":                   {"Test plugin compatibility", "Run confined protocol checks with generated test state."},
	"host inspect":                  {"Check host setup", "Inspect local prerequisites without changing the host."},
	"host capabilities":             {"Check virtualization support", "Inspect capabilities of the selected libvirt connection."},
	"storage pool list":             {"Browse storage pools", "See existing pools without activating or adopting them."},
	"storage pool show":             {"View storage pool details", "Inspect pool configuration and reported available capacity."},
	"storage pool create":           {"Create storage pool", "Set up a folder for VM disks; libvirt's standard folder by default."},
	"import discard":                {"Remove prepared copy", "Delete a finished import's work folder and prepared images to free disk space."},
	"storage pool start":            {"Start storage pool", "Start the selected stopped pool so VMs can use it."},
	"storage access grant":          {"Grant disk read access", "Review temporary access to one stopped VM's managed volume."},
	"storage access revoke":         {"Revoke disk read access", "Restore access settings from the original grant job."},
	"storage access result":         {"View disk access result", "Inspect the saved result of a disk access job."},
	"host helper identity":          {"View host helper identity", "See the public key fingerprint and administrator policy status."},
	"network list":                  {"Browse networks", "See existing networks; this does not verify isolation."},
	"network create":                {"Create network", "Review a network definition with explicit host and guest access."},
	"network creation result":       {"View network creation result", "Inspect the retained network, subnet reservation and current state."},
	"network creation resume":       {"Resume interrupted network creation", "Activate the exact retained network without redefining it."},
	"network cidr check":            {"Check subnet conflicts", "Compare proposed subnets with host routes and existing networks."},
	"host pci list":                 {"Browse PCI devices", "Inspect device identity, drivers and observed IOMMU groups."},
	"device usb list":               {"Browse USB devices", "Inspect device identity, serial numbers and observed ports."},
	"network show":                  {"View network details", "Compare the active and saved network configuration."},
	"vm list":                       {"Browse virtual machines", "See native libvirt guests without adopting them."},
	"vm show":                       {"View virtual machine details", "Inspect the selected VM's active and saved configuration."},
	"vm recovery inspect":           {"Check VM recovery requirements", "Inspect disks, firmware, TPM and unresolved capture dependencies."},
	"vm recovery auxiliary inspect": {"Inspect firmware and TPM files", "Check authorized file metadata without reading or capturing state."},
	"snapshot create":               {"Capture stopped VM", "Copy all required disks, configuration, firmware and TPM state."},
	"snapshot list":                 {"Browse local captures", "See retained local recovery sets."},
	"snapshot show":                 {"Verify local capture", "Inspect and verify every member of a retained recovery set."},
	"snapshot restore":              {"Restore capture as new VM", "Create independent disks and a new disconnected VM from a capture."},
	"vm console show":               {"Open console", "Open the guest display or a configured serial console. Closing it keeps the VM running."},
	"vm boot show":                  {"View VM boot order", "Compare active and next-boot disk and network device order."},
	"vm guest-agent enable":         {"Enable guest integration", "Add the management channel before installing QEMU guest tools."},
	"vm guest-agent show":           {"Guest integration status", "Check the guest-agent management channel."},
	"vm resources show":             {"CPU and memory settings", "Compare current and next-boot values, then review changes."},
	"backup receipt show":           {"Inspect recovery receipt", "Read recovery identifiers for a verified backup operation."},
	"backup receipt read":           {"Open recovery receipt", "Read a saved receipt without accessing its backup repository."},
	"backup receipts":               {"Saved backups", "Choose a verified backup to recover or save its recovery receipt."},
	"vm boot set":                   {"Edit boot order and installer", "Choose which device boots first or eject an installer disc for the next boot."},
	"vm readiness show":             {"Check guest agent", "Check the running VM's guest-agent channel and response."},
	"guest tools install":           {"Guest tools", "Install supported guest integration tools with a reviewed plan."},
	"guest tools catalog":           {"Supported guest tools", "Read Linux installation requirements and Windows guidance."},
	"guest recipe run":              {"Run guest setup recipe", "Review a non-root recipe using explicit SSH credentials and host keys."},
	"guest recipe result":           {"View guest setup result", "Inspect saved recipe stages and completion status."},
	"vm create":                     {"Create VM from prepared source", "Review disks, hardware and provisioning for a new powered-off VM."},
	"vm creation result":            {"View VM creation result", "Inspect disk preparation and VM definition progress."},
	"vm creation resume":            {"Resume interrupted VM creation", "Finish the VM definition using verified retained disks."},
	"vm creation cleanup":           {"Clean up failed VM creation", "Choose whether to retain or delete failed-creation volumes."},
	"vm creation accept":            {"Accept retained VM devices", "Review retained BIOS chipset devices after full disk verification."},
	"vm start":                      {"Start VM", "Review starting the selected virtual machine."},
	"vm stop":                       {"Shut down VM", "Request a graceful shutdown; a timeout never forces power off."},
	"vm reboot":                     {"Restart VM", "Request one graceful restart without a forced-stop fallback."},
	"vm pause":                      {"Pause VM", "Suspend execution while keeping the VM in memory."},
	"vm resume":                     {"Resume paused VM", "Continue execution of a paused virtual machine."},
	"vm save":                       {"Save VM state and stop", "Save managed runtime state; this is not an independent backup."},
	"vm restore-saved":              {"Resume saved VM", "Restore the VM's previously saved runtime state."},
	"vm remove":                     {"Remove VM", "Remove a stopped VM. Keep disks by default, or select disks to delete; backups are kept."},
	"vm disk grow":                  {"Grow VM disk", "Make a stopped VM's disk larger; partitions inside the guest are not changed."},
	"vm disk add":                   {"Add VM disk", "Give a stopped VM one more empty disk in its own storage pool."},
	"vm disk add dispose":           {"Close disk addition", "Accept the new disk, or delete its unused volume, and free the VM."},
	"vm autostart":                  {"Change VM automatic startup", "Choose whether libvirt starts this VM automatically."},
	"vm set":                        {"Edit VM hardware", "Review next-boot CPU and memory, boot order or media changes."},
	"import source describe":        {"Inspect image or folder", "Detect the image format and settings before import."},
	"import describe":               {"Read appliance settings", "Show declared hardware before verifying and preparing disk files."},
	"import inspect":                {"Inspect OVA appliance", "Read appliance packaging without extracting or running the guest."},
	"import prepare":                {"OVA appliance", "Choose an appliance, review its disks and prepare independent copies."},
	"import prepare-disks":          {"Existing disk images", "Browse for disks and choose where to prepare independent copies."},
	"import prepare-install":        {"ISO installer", "Choose an installer and add blank disks for a fresh installation."},
	"import result":                 {"View image preparation result", "See prepared artifact paths and verification results."},
	"import verify":                 {"Verify prepared images", "Check artifact hashes without making a guest boot claim."},
	"lab validate":                  {"Validate lab definition", "Check the lab schema, network intent and resource dependencies."},
	"config validate":               {"Validate configuration file", "Check a declarative document against the bundled schema."},
	"backup policy validate":        {"Validate backup schedule", "Check selectors, timing and timezone without installing a schedule."},
	"backup repository init":        {"Create backup repository", "Create private encrypted storage using an explicit credential file."},
	"backup repository check":       {"Check backup repository", "Run a full repository data-integrity check and save the result."},
	"backup create":                 {"Back up a local capture", "Encrypt a complete capture and verify independent recovery."},
	"backup restore":                {"Recover capture from backup", "Restore an exact repository snapshot as a new local capture."},
	"backup result":                 {"View backup or recovery result", "Read saved repository checks and recovery verification."},
	"backup policy preview":         {"Preview backup schedule", "See planned times in the selected timezone without starting jobs."},
	"backup verify-manifest":        {"Validate recovery manifest", "Check recovery metadata and optional member integrity."},
	"operation list":                {"Browse jobs", "See durable background jobs and their current states."},
	"operation show":                {"View job details", "Inspect one job's saved state and recovery information."},
	"operation watch":               {"Read job events", "Read ordered events after a selected event position."},
	"operation cancel":              {"Cancel job safely", "Request cancellation when the job reaches a safe boundary."},
	"operation reconcile":           {"Check interrupted job outcome", "Inspect an uncertain effect without repeating the operation."},
	"plan show":                     {"Review saved plan", "Open the exact saved plan and its approval requirements."},
}

// TUI entries that reuse a registered request with other parameters.
var tuiActionText = map[string]struct{ label, description string }{
	forceOff.Command: {"Force off VM", "Cut power at once, like pulling the plug; unsaved guest data may be lost."},
}

func actionLabel(a ui.Action) string {
	if text, ok := actionText[a.Command]; ok {
		return text.label
	}
	if text, ok := tuiActionText[a.Command]; ok {
		return text.label
	}
	return "Unrecognized action"
}

func actionDescription(a ui.Action) string {
	if text, ok := actionText[a.Command]; ok {
		return text.description
	}
	if text, ok := tuiActionText[a.Command]; ok {
		return text.description
	}
	return "This action needs a display description."
}
