# Changelog

## 1.0.0-beta.4 — owner-test pre-release

Unsigned development beta; not a certified 1.0. See the
[beta guide](docs/beta-testing.md) for installation and known limitations.
The first beta with `virmill update`: install it by hand once, and later
betas can be installed with `virmill update`.

- `virmill update` installs the newest release from GitHub. It downloads the
  RPM or DEB packages this host uses, checks each against the release's
  `SHA256SUMS` and its own package name and version, installs them with dnf or
  apt (which asks for your password) and restarts your coordinator. It refuses
  while jobs are unfinished. Virmill checks for a newer release at most once a
  day and says so in the TUI and after interactive commands;
  `virmill update checks off` or `VIRMILL_UPDATE_CHECK=0` turns that off. See
  [updates](docs/updates.md) and ADR 0055.
- The TUI frame is simpler. The header shows the page, running or failed jobs
  and the connection; one line lists the keys for the current page; buttons
  wrap onto a second row instead of hiding behind `>`. Empty lists say what
  belongs there and how to add it, lists show "2 VMs · row 1 of 2", and
  details pages offer More and Back instead of list buttons. Unfinished
  sections are labeled: Templates (not ready), Labs (validate only), Devices
  (discovery only) and Plugins (preview).
- TUI lists show more than name and state: CPU and RAM for VMs, type and
  address for networks, size and free space for storage pools, and for jobs
  the task, its VM or resource, and when it started ("Start VM · web01 ·
  2 h ago") instead of an operation ID. Network and storage details are
  written in plain language, and `x` shows every field with libvirt XML as
  XML instead of escaped JSON.
- The TUI Overview is a home screen: VM counts, storage pools with their free
  space, whether the host is ready, running or failed jobs and the last job,
  each with the key that opens the details, above the VM list. It no longer
  adds up the free space of pools that share a filesystem. Settings shows the
  connection, display options, version and the same readable host check as
  `virmill doctor`, instead of a raw field dump. Both now list what is missing
  or needs attention before what is ready.
- TUI task menus offer only power actions the selected VM's state allows
  (no "Resume paused VM" for a stopped VM). Advanced tools lists the tasks
  More does not, so nothing appears twice; All tools and search still list
  everything. Remove VM is in More for a stopped VM. The Jobs page no longer
  offers Create VM, and an empty Jobs list no longer offers Activity. Help
  is grouped by task and fits an 80x24 terminal.
- `operation.list` and `operation.get` add each job's plan `operation`,
  `resourceIDs` and, when known, `targetName`. Existing fields are unchanged.
- `virmill doctor` prints a readable report: what is ready, what is missing and
  why, and the exact `dnf` or `apt` command to install it. It now checks that
  libvirt is installed and running and that you can open `/dev/kvm`, checks
  `virt-viewer` (the console viewer) instead of `remote-viewer`, and no longer
  reports the unused `virt-v2v`. `--output json` keeps the full data, with
  new `purpose`, `optional`, `packages` and `installer` fields.
- Binaries are built stripped (`-s -w`), about a third smaller. lintian no
  longer reports `unstripped-binary-or-object` for the DEBs.

## 1.0.0-beta.3 — owner-test pre-release

Unsigned development beta; not a certified 1.0. See the
[beta guide](docs/beta-testing.md) for installation and known limitations.

- Leaving an unused Import started from the VM list (`i`, then Esc) returns to
  the list instead of the VM's More tasks menu. Import started from a task menu
  still returns to that menu.
- DEB packages now install on Debian 13 and Ubuntu 24.04. The published
  1.0.0-beta.2 DEBs fail to unpack because they list no directories. The
  rebuilt packages add directory entries, `md5sums`, a copyright file and
  changelog, a maintainer address, a longer description and commit-time
  timestamps, and drop the Essential `util-linux` dependency.

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
