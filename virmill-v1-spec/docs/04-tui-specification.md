# 04 — TUI interaction specification

## Navigation model

Primary sections: **Overview, VMs, Networks, Storage, Templates, Labs, Protection, Devices, Jobs, Plugins, Settings**. Keep a visible connection/host indicator and job status area. Keyboard operation is mandatory; mouse support is optional convenience. `?` opens contextual help, `/` searches, `Tab` switches focus, `Esc` goes back, and a configurable command-palette key exposes available actions.

The interface must not hide necessary actions behind undocumented single letters. Destructive actions require a labeled confirmation with exact affected resources. Closing a page or the application does not cancel an operation.

Use responsive layouts down to 80×24; narrow terminals switch to one pane. All tables have a details view instead of truncating critical fields. Offer an ASCII/no-color mode. Color supplements text and is not the sole indication of state. Sanitize guest names, log text and plugin output against terminal escape injection.

## Screens

| Screen | Information | Essential actions |
|---|---|---|
| Overview | Host health, capacity, running/stopped VMs, recent errors, overdue jobs | Diagnose, create/import, jump to failing job |
| VM list | Name, state, guest, resources, networks, ownership, pending changes | Search/filter/tag, lifecycle, create/import, multi-select safe actions |
| VM details | Summary, hardware, disks, networks, devices, protection, guest setup, activity | Preview edits; switch live/next-boot view; open console/SSH |
| Networks | Type, CIDR, host access, egress, DHCP/DNS, members, drift | Create/edit, attach NIC, inspect routes, port forwards, bridge wizard |
| Storage | Pools, free/allocated/virtual capacity, volumes and dependency graph | Import/create/expand/move, inspect references, garbage-collection preview |
| Templates | Versions, guest readiness, base dependencies, clone users | Create/verify/version, full/linked clone, safe retention |
| Labs | Definitions, resource graph/list, per-node readiness and health | Validate/plan/apply, start/stop, restore point, teardown preview |
| Protection | Snapshots, backup sets, schedules, repository health, restore-test date | Capture, verify, restore, test restore, retention preview |
| Devices | USB identity/availability/ownership, PCI eligibility, mounted volumes | Attach/detach, binding policy, watch reconnection |
| Jobs | State, phase, progress, logs, locks, cancellation/recovery options | Watch/detach, cancel safely, retry or reconcile |
| Plugins | Installed versions, trust, capabilities, effective grants, health | Install/validate, approve permissions, enable/disable/rollback |

## Creation/import wizard

Steps: source → inspection → guest profile/architecture → CPU/RAM/firmware → disks/storage → network memberships and route intent → guest setup → reviewed plan → running job → completion report. Each step is editable without redoing expensive conversion. Persist a draft excluding secrets; changing the source invalidates source-dependent selections.

The hardware page separates recommended compatible defaults from expert options. The network page supports adding multiple NICs; it never presents network choice as a single radio button for the whole VM.

The final review includes source preservation, virtual and estimated physical size, disk conversion, firmware/controller changes, license/provenance notices, network exposure, device ownership, expected downtime and what constitutes verified success.

## Hardware editor

Fields show current live value, next-boot value, requested value and supported apply modes. Prevent saving a blank/unparseable value. Multi-field edits form one plan, not a succession of hidden live mutations. If both live and persistent changes cannot be atomic, show the sequence and possible partial outcome.

Advanced XML uses an external editor or dedicated full-screen editor, with a semantic diff and validation. It is an expert escape hatch, not an excuse to omit ordinary controls. Reject destructive loss of unknown nodes and privileged injections under an untrusted import context.

## Networking workspace

Show a readable topology list and optional text graph generated from real membership. Every NIC displays network type, guest address source, default-route intent and whether the guest-side route was verified. Network detail labels distinguish internet egress, LAN access, host service access and guest-to-guest connectivity.

A VM connected to a protected lab and an internet/LAN network receives a visible dual-homed warning. Show that guest forwarding can bypass the intended lab boundary. Do not label a lab “secure sandbox” just because its host bridge has no NAT.

## Background work and recovery

Long operations move to Jobs immediately. The UI remains usable while conversion/backup runs. The user can detach, see locks preventing conflicting actions, and reopen progress. Cancellation says whether it is immediate, pending a safe boundary, or unavailable at the current point.

After a coordinator restart, show reconciled, interrupted and recovery-required jobs. Provide verified recovery actions; never display a generic retry button for a non-idempotent operation with unknown effects.

## Plugin UI contributions

Plugins contribute declared actions, forms, tables and status cards using a bounded declarative schema. They do not inject terminal escape sequences, arbitrary JavaScript/HTML, or TUI framework objects. Plugin identity, permission scope and job attribution remain visible. Core confirmation dialogs cannot be overridden.

## Usability acceptance

Test fresh-user import, multi-NIC setup, USB attachment, snapshot revert, backup restore and lab teardown using only the TUI. Repeat equivalent operations through CLI and compare plans/results. Test resizing, screen-reader-friendly linear output, canceled dialogs, lost backend connections, and a failed plugin without losing the surrounding interface state.
