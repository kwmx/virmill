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
	Argument string
	Mutation string
}

var Actions = []Action{
	{"plugin validate", "plugin.validate", "Plugins", "Validate a development manifest; no trust or signature claim", "path", ""},
	{"plugin test", "plugin.test", "Plugins", "Run confined summary conformance using synthetic selected VM records", "path", ""},
	{"host inspect", "host.inspect", "Overview", "Inspect read-only local host prerequisites", "", ""},
	{"host capabilities", "host.capabilities", "Settings", "Probe the selected local libvirt connection", "", ""},
	{"vm list", "inventory.list", "VMs", "List native libvirt inventory without adoption", "", ""},
	{"vm show", "inventory.get", "VMs", "Inspect live and persistent state by UUID", "id", ""},
	{"vm start", "vm.plan", "VMs", "Plan VM start", "id", "start"},
	{"vm stop", "vm.plan", "VMs", "Plan graceful stop; never escalate on timeout", "id", "stop"},
	{"vm pause", "vm.plan", "VMs", "Plan pause", "id", "pause"},
	{"vm resume", "vm.plan", "VMs", "Plan resume", "id", "resume"},
	{"vm save", "vm.plan", "VMs", "Plan managed save (not an independent backup)", "id", "save"},
	{"vm restore-saved", "vm.plan", "VMs", "Plan restoration of saved state", "id", "restore-saved"},
	{"vm autostart", "vm.plan", "VMs", "Plan autostart policy", "id", "autostart"},
	{"vm set", "vm.plan", "VMs", "Plan a lossless powered-off vCPU edit", "id", "set"},
	{"import inspect", "import.inspect", "VMs", "Inspect bounded OVA packaging; no extraction or guest execution", "path", ""},
	{"lab validate", "lab.validate", "Labs", "Validate declarative schema, network intent and dependency graph", "path", ""},
	{"config validate", "document.validate", "Settings", "Validate a bundled declarative document offline", "path", ""},
	{"backup verify-manifest", "backup.verify-manifest", "Protection", "Check recovery member completeness; not a boot test", "path", ""},
	{"operation list", "operation.list", "Jobs", "List durable jobs", "", ""},
	{"operation show", "operation.get", "Jobs", "Inspect durable job state", "id", ""},
	{"operation watch", "operation.watch", "Jobs", "Read ordered events after a cursor", "id", ""},
	{"operation cancel", "operation.cancel", "Jobs", "Request cancellation at a safe boundary", "id", ""},
	{"operation reconcile", "operation.reconcile", "Jobs", "Observe an uncertain effect without replaying it", "id", ""},
	{"plan show", "plan.show", "Jobs", "Show the exact immutable plan", "id", ""},
}
