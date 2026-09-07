// Package ui defines implemented service actions shared by both interfaces.
package ui

import (
	"context"
	"virmill.local/core/internal/app"
)

type Client interface {
	Call(context.Context, string, app.Request) (app.Response, error)
}
type Action struct {
	Command  string
	Method   string
	Section  string
	Summary  string
	Argument string // id/path positional argument, parameters JSON form, or empty
	Mutation string
}

var Actions = []Action{
	{"plugin call", "plugin.call", "Plugins", "Plan a confined read-only action on explicitly selected VMs", "id", "call"},
	{"plugin result", "plugin.result", "Plugins", "Read a durable plugin result by operation ID", "id", ""},
	{"plugin new", "plugin.develop", "Plugins", "Plan buildable Go action source at a new destination", "path", "new"},
	{"plugin pack", "plugin.develop", "Plugins", "Plan a signed package; signing key stays outside the payload", "path", "pack"},
	{"plugin list", "plugin.list", "Plugins", "List local installations and active immutable versions", "", ""},
	{"plugin show", "plugin.show", "Plugins", "Inspect retained versions, trust and activation state", "id", ""},
	{"plugin install", "plugin.plan", "Plugins", "Plan signed package installation with exact key and grants", "path", "install"},
	{"plugin update", "plugin.plan", "Plugins", "Plan verified update; preserve the previous version and disable until reviewed", "path", "update"},
	{"plugin enable", "plugin.plan", "Plugins", "Plan enabling supported confined actions after permission checks", "id", "enable"},
	{"plugin disable", "plugin.plan", "Plugins", "Plan blocking new invocations while preserving resource metadata", "id", "disable"},
	{"plugin remove", "plugin.plan", "Plugins", "Plan removal from active inventory; retain data and created resources", "id", "remove"},
	{"plugin rollback", "plugin.plan", "Plugins", "Plan activation of the retained prior executable; no data migration", "id", "rollback"},
	{"plugin permissions show", "plugin.permissions", "Plugins", "Inspect exact declared and installed permission scopes", "id", ""},
	{"plugin permissions grant", "plugin.plan", "Plugins", "Plan explicit additional declared scopes and disable until reviewed", "id", "grant"},
	{"plugin permissions revoke", "plugin.plan", "Plugins", "Plan scope revocation and stop new invocations", "id", "revoke"},
	{"plugin validate", "plugin.validate", "Plugins", "Validate a development manifest; no trust or signature claim", "path", ""},
	{"plugin test", "plugin.test", "Plugins", "Run confined summary conformance using synthetic selected VM records", "path", ""},
	{"host inspect", "host.inspect", "Overview", "Inspect read-only local host prerequisites", "", ""},
	{"host capabilities", "host.capabilities", "Settings", "Probe the selected local libvirt connection", "", ""},
	{"storage pool list", "storage.pool.list", "Storage", "List existing local libvirt pools without adoption or activation", "", ""},
	{"storage pool show", "storage.pool.get", "Storage", "Inspect pool XML and available capacity by stable UUID", "id", ""},
	{"storage access grant", "storage.access.grant", "Storage", "Plan actor-only read access to one stopped VM's managed volume through the approved helper", "id", "grant-read"},
	{"storage access revoke", "storage.access.revoke", "Storage", "Plan exact access restoration from a successful original grant operation", "id", "revoke-read"},
	{"storage access result", "storage.access.result", "Storage", "Read the durable helper observation for an access operation", "id", ""},
	{"host helper identity", "host.helper.identity", "Settings", "Show the coordinator's public helper key fingerprint and administrator-policy status", "", ""},
	{"network list", "network.list", "Networks", "List existing libvirt networks; no isolation verification implied", "", ""},
	{"network cidr check", "network.cidr.check", "Networks", "Check candidate CIDRs against host addresses, every route table, defined networks and explicit planned allocations", "parameters", ""},
	{"host pci list", "host.pci.list", "Devices", "Discover native PCI identity, drivers and observed IOMMU groups without detachment or passthrough authority", "", ""},
	{"network show", "network.get", "Networks", "Inspect live/persistent network configuration by stable UUID", "id", ""},
	{"vm list", "inventory.list", "VMs", "List native libvirt inventory without adoption", "", ""},
	{"vm show", "inventory.get", "VMs", "Inspect live and persistent state by UUID", "id", ""},
	{"vm recovery inspect", "vm.recovery.inspect", "Protection", "Inspect configured disks, backing sources, firmware, TPM and unresolved dependencies without reading source bytes", "id", ""},
	{"vm boot show", "vm.boot.get", "VMs", "Inspect separate live and next-boot device order using disk targets and NIC MACs", "id", ""},
	{"vm create", "vm.create", "VMs", "Plan managed copies, optional NoCloud provisioning and a new powered-off VM from a prepared source", "id", "create"},
	{"vm creation result", "vm.creation.result", "VMs", "Observe active progress and durable volume/definition stages by creation operation ID", "id", ""},
	{"vm creation resume", "vm.creation.resume", "VMs", "Plan definition recovery using complete reverified retained volumes; never allocate or upload again", "id", "resume-definition"},
	{"vm creation cleanup", "vm.creation.cleanup", "VMs", "Plan explicit retention or guarded deletion of failed-creation volumes and close the original recipe", "id", "cleanup"},
	{"vm creation accept", "vm.creation.accept", "VMs", "Plan explicit acceptance of retained BIOS chipset devices after complete disk readback; preserve the original partial job", "id", "accept-devices"},
	{"vm start", "vm.plan", "VMs", "Plan VM start", "id", "start"},
	{"vm stop", "vm.plan", "VMs", "Plan graceful stop; never escalate on timeout", "id", "stop"},
	{"vm pause", "vm.plan", "VMs", "Plan pause", "id", "pause"},
	{"vm resume", "vm.plan", "VMs", "Plan resume", "id", "resume"},
	{"vm save", "vm.plan", "VMs", "Plan managed save (not an independent backup)", "id", "save"},
	{"vm restore-saved", "vm.plan", "VMs", "Plan restoration of saved state", "id", "restore-saved"},
	{"vm autostart", "vm.plan", "VMs", "Plan autostart policy", "id", "autostart"},
	{"vm set", "vm.plan", "VMs", "Plan next-boot CPU/RAM, boot order or retained-media ejection on a powered-off VM", "id", "set"},
	{"import inspect", "import.inspect", "VMs", "Inspect bounded OVA packaging; no extraction or guest execution", "path", ""},
	{"import prepare", "import.prepare", "VMs", "Plan independent conversion of every selected OVA disk; no VM is defined", "path", "prepare"},
	{"import prepare-disks", "import.prepare-disks", "VMs", "Plan independent copies of an explicitly selected existing disk set and its backing files", "path", "prepare-disks"},
	{"import prepare-install", "import.prepare-install", "VMs", "Plan copied ISO installation media and verified empty guest disks", "path", "prepare-install"},
	{"import result", "import.result", "VMs", "Show prepared artifact paths and verification by operation ID", "id", ""},
	{"import verify", "import.verify", "VMs", "Verify declared prepared-import artifact hashes; no guest boot claim", "path", ""},
	{"lab validate", "lab.validate", "Labs", "Validate declarative schema, network intent and dependency graph", "path", ""},
	{"config validate", "document.validate", "Settings", "Validate a bundled declarative document offline", "path", ""},
	{"backup verify-manifest", "backup.verify-manifest", "Protection", "Check declared recovery metadata and optional member integrity; no complete-capture or boot proof", "path", ""},
	{"operation list", "operation.list", "Jobs", "List durable jobs", "", ""},
	{"operation show", "operation.get", "Jobs", "Inspect durable job state", "id", ""},
	{"operation watch", "operation.watch", "Jobs", "Read ordered events after a cursor", "id", ""},
	{"operation cancel", "operation.cancel", "Jobs", "Request cancellation at a safe boundary", "id", ""},
	{"operation reconcile", "operation.reconcile", "Jobs", "Observe an uncertain effect without replaying it", "id", ""},
	{"plan show", "plan.show", "Jobs", "Show the exact immutable plan", "id", ""},
}
