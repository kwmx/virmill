# Changelog

## Unreleased

- Leaving an unused Import started from the VM list (`i`, then Esc) returns to
  the list instead of the VM's More tasks menu. Import started from a task menu
  still returns to that menu.

## 1.0.0-beta.2 — owner-test pre-release

Unsigned development beta; not a certified 1.0. 2 of 71 acceptance scenarios
are accepted and `make release-check` still blocks 1.0. See the
[beta guide](docs/beta-testing.md) and [owner handoff](docs/owner-beta-handoff.md)
for scoped evidence and known limitations.

- TUI resource workspace with visible action buttons, grouped task menus,
  keyboard action search and a local file explorer.
- Import: native import options with settings export, metadata-first appliance
  review, unified source review, cancellable large-image checks and private
  resumable setup drafts.
- Guided VM setup for CPU/RAM, firmware, disks, boot order and networks,
  including creating or refreshing a network without leaving setup.
- New graphical guests default to a private local SPICE display when the host
  offers it; serial console access for terminal sessions.
- Reviewed guest tools setup, guest integration channel edits and backup
  receipt recovery.
- Guided network creation for NAT, isolated lab and guest-only profiles.
- Observed CPU/RAM settings before edits, guided autostart settings, VM removal
  that keeps disks by default and explicit selected-disk deletion
  (`vm remove --delete-disk`, or per-disk choices in the TUI).
- Jobs → Activity shows full event messages with paging; completed jobs can open
  their VM; coordinator outages explain how to start it.
- Fixes: native qcow2 cluster-size metadata is accepted during removal,
  confirmation text and approval IDs stay readable at 80×24, and confirmation
  always opens at its heading.
- CI builds with the pinned, checksum-verified Go toolchain.

## 1.0.0-beta.1 — first owner-test beta (`1eccfc4`)

- Intake/precedence ADRs, pinned build inputs, vendored Go dependencies and complete
  acceptance traceability established.
- Shared service, CLI/TUI subset, native libvirt adapter, durable SQLite operation
  engine and private authenticated coordinator added.
- Strict parser, OVA/XML/network/graph/USB/manifest safety foundations and tests added.
- Language-neutral SDK, generator, signed plugin inventory, sandbox and conformance
  fixtures added.
- Durable signed plugin install/update/rollback and grant plans, confined installed
  read-only actions, source/package creation and CLI/TUI access added.
- Digest-bound human review details and client-relative path resolution added;
  recovery now includes unfinished jobs older than the recent-list limit.
- Development RPM/DEB build/staging installers and operator documentation added.

Mandatory workflow implementation and release qualification remain incomplete.
