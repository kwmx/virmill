# Changelog

## 1.0.0-beta.6 — owner-test pre-release

From an image to a VM you can use, in one page and one confirmation.

- **New VM** (press `n`) replaces Create VM and Import (ADR 0065). Choose an
  ISO, disk image or OVA and Virmill reads it by itself. One settings page shows
  name, CPU, memory, the installer's disk size and **Start it and open its
  display**; **Advanced settings** keeps everything else. **Create VM** shows one
  confirmation for everything that follows: setting up libvirt's standard
  storage pool when there is none (or starting a stopped one), copying the
  images, creating and starting the VM and removing the prepared copy. Progress
  stays on the same screen and ends with the VM running and its display open.
  From a host with no storage pool, a disk image now takes three keypresses
  after choosing the file, down from 72.
- Every change now has one plain confirmation instead of a checkbox per
  acknowledgement. It lists everything you agree to in plain words, and `d`
  shows the technical plan. The CLI still takes each `--ack`.
- On a host with several storage pools and none named `default`, New VM
  preselects the one that starts with the host, or the one with the most free
  space, and names it.
- **Display** replaces Console on running VMs, and `v` opens it from the VM
  list. A VM's only graphical display
  opens straight away, in its own window, and Virmill stays usable while it is
  open. A viewer that cannot attach says why.
- The installer disk defaults to a size whose preparation fits the free space,
  with a note, and a refused preview is shown on the New VM page.
- Your own VMs start without a manual libvirt step (ADR 0064). The packages now
  ship a per-user libvirt socket, `virmill-virtqemud.socket`, which the
  coordinator's service starts before itself. The libvirt daemon is then started
  by your systemd whenever something connects, instead of by the coordinator,
  whose hardening stopped that daemon launching QEMU. It still exits when idle
  and starts again on the next connection. Where your distribution ships its own
  per-user libvirt socket, that one is used instead. If a VM start is still
  refused, the refusal and `virmill doctor` now say to run
  `systemctl --user restart virmilld.service`.

## 1.0.0-beta.5 — owner-test pre-release

Everyday VM lifecycle: the things you do to a VM after it exists.

- Power actions on both your own session and the system connection: start,
  graceful stop, force off, restart, pause, resume, save and restore.
  **Force off** is in the TUI under Advanced and works on a paused VM too, which
  is the way out of a VM that cannot resume. A shutdown the guest ignores now
  fails after 60 seconds with `WAIT_TIMEOUT`, having forced nothing, and leaves
  the VM free to force off or try again.
- Change a running VM's CPU count and memory for its next boot
  (ADR 0061). The running VM keeps what it has until you shut it down, and the
  parts of its definition Virmill does not model are preserved.
- Grow a stopped VM's disk: `virmill vm disk grow` and **Advanced → Grow VM
  disk** (ADR 0062). Works on a file disk or a pool volume, never shrinks, and
  refuses a disk with a backing file, snapshots, or one another VM uses. Only
  the virtual disk grows — extend partitions and filesystems inside the guest.
- Add an empty disk to a stopped VM: `virmill vm disk add` and **Advanced → Add
  VM disk** (ADR 0062). It goes in the VM's own pool, on the bus its disks
  already use, at the next free target. If an addition stops before it
  finishes, `virmill operation dispose-disk-addition` and **Close disk
  addition** finish it — keep the new disk, or delete the volume no VM uses —
  so nothing is left holding the VM and its pool.
- Move a disk to another storage pool: `virmill vm disk move` and **Advanced →
  Move VM disk** (ADR 0062). The disk is copied through libvirt, hashed as it
  is copied and read back against that digest, and only then does the VM start
  using the copy; the original is deleted unless you pass `keepOldCopy`, which
  the review asks you to acknowledge. The guest sees the same disk at the same
  place. Both copies exist while it runs, so the review says how much room the
  destination needs.
- Remove a VM Virmill created, or a UEFI VM, while keeping every disk
  (ADR 0063). A UEFI VM's firmware settings file and an emulated TPM's state
  are kept too, and the review names both. Passthrough TPMs, device NVRAM and
  firmware state Virmill does not recognise are still refused.
- A boot that never reached the guest no longer strands the VM. It used to
  leave a job that could not be resolved holding the VM's lock, so even forcing
  it off was refused; a start or restore that provably did nothing now fails
  plainly and frees the VM.
- On a host where nothing has started libvirt for your own user, Virmill says so
  before making a plan instead of failing inside libvirt. The coordinator would
  otherwise start that daemon itself, and a daemon started that way can never
  launch QEMU, so every VM start failed with a bare permission error. The
  refusal and `virmill doctor` both name what to run.

- Create storage pools in Virmill: `virmill storage pool create`, **Storage →
  Create pool**, and **Create storage pool** inside VM setup. With no options it
  plans libvirt's standard `default` pool at `/var/lib/libvirt/images`; `--name`,
  `--path` and `--no-autostart` adjust it, as does **Storage → Custom pool** in
  the TUI. In VM setup the form stays open while
  the pool is created, and the new pool is then selected. Existing folders keep
  their permissions and files; existing pools are never changed (ADR 0056).
- Import in one review: the import review also covers creating the VM with
  your settings and, with **After creation: Start the VM** (the default),
  starting it. Later steps run by themselves only if they ask for nothing you
  did not check; otherwise their own review opens (ADR 0057).
- Cloud images in the TUI: VM setup for a disk image offers **Cloud image: Yes,
  create my user with cloud-init**, suggested when the name looks like a cloud
  image. It asks for a user name, your SSH public key file and the image's https
  download address, and builds the reviewed NoCloud seed; passwordless sudo is
  the default (ADR 0059). Checked on the test host with Ubuntu 24.04's cloud
  image: the guest took a NAT address, and the reviewed key logged in with
  passwordless sudo after cloud-init finished.
- `virmill doctor` and the TUI host check say when firewalld holds a libvirt
  network's bridge in no zone. Its default zone then usually drops DHCP and DNS,
  so VMs on that network get no address. This happens when the network starts
  before firewalld at boot. The check shows the `firewall-cmd` command that
  puts the bridge back in libvirt's zone; it changes nothing itself.
- The coordinator no longer keeps a socket open after each libvirt call. Its
  libvirt event loop, which releases closed connections, only started with the
  first VM reboot; until then every VM, pool or network list and host check
  left one socket and two descriptors behind.
- Importing needs about one copy of disk space instead of up to three
  (ADR 0060). An OVA disk that is a streamOptimized VMDK or a single-file image
  is converted straight from the archive instead of being unpacked first.
  Preparation budgets what `qemu-img measure` says the conversion writes, and
  enforces it as the converter's output limit. Creation reserves each disk's
  prepared size, not its virtual size plus 25%. With **Prepared copy: Remove
  after creation** (the default), creation hands the prepared copy over when it
  shares the pool's filesystem: it frees each disk's prepared file as it copies
  it (`preparedCopy: "hand-over"`, acknowledgement `hand-over-prepared-copy`).
  If copying then stops, import the original again; it is never modified.
  Conversion no longer waits for the disk after every write: it writes through
  the host's cache and is flushed once before it is recorded. Large disks also
  get time in proportion to their size, instead of a flat 30 minutes.
- Creating a VM from a large prepared disk no longer times out. Planning checks
  every prepared image, which took about 80 seconds for a 34 GiB disk; the TUI
  and `vm create` now wait as long as they do for imports. If an automatic step
  of the one approval still fails, VM setup says "The VM was not created" with
  the reason, instead of staying on "Working…".
- After a setup has created its VM, the next import or Create VM starts
  directly instead of stopping at "A setup was submitted". That page still
  appears while the submitted job is unfinished or failed, and a submitted
  preparation still offers **Continue prepared setup**.
- VM setup suggests a free name ("… 2") when the suggested VM name is taken.
- Free the space an import used: `virmill import discard OPERATION_ID
  [--keep-images]` removes a finished preparation's work folder and prepared
  images, never the original source or created VMs. VM setup's **Prepared copy:
  Remove after creation** (the default) does this as the last step of the one
  approval (ADR 0058).
- Import problems stay on the import page: Preview checks the import before
  opening VM settings, and a problem found later returns there with the VM
  settings kept. Unset import choices read "Choose…".
- An OVA disk whose format the appliance does not declare keeps its format
  choice visible and gets a labelled suggestion from the file name (`.vmdk`,
  `.qcow2`, `.vdi`, `.vhdx`, `.vhd`).
- Start a stopped storage pool from Virmill: `virmill storage pool start UUID`,
  **Start pool** on a stopped pool's details, and **Start pool NAME** in VM
  setup when your only pools are stopped. It also starts with the host unless
  `--no-autostart`; nothing is created or changed in its folder.
- VM setup preselects libvirt's `default` pool (or the only usable pool) and the
  firmware the source declares. Otherwise BIOS is preselected and labelled
  *Suggested*.
- Import saves prepared copies in a new private folder under
  `~/.local/share/virmill/imports` by default, so the folder step is skipped;
  **Back: Destination** still chooses another folder.
- When image preparation finishes and the VM settings are complete, the VM review
  opens directly instead of the settings form. A created VM's result offers
  **Start VM**.
- VMs made from your own disk images or installer ISOs get one e1000e network
  adapter on libvirt's active `default` NAT network, connected and labelled
  *Suggested*. Appliance (OVA) adapters still start disconnected, now with the
  default NAT network and e1000e suggested, and appliance disks on controllers
  QEMU cannot offer (VMware SCSI, IDE) get SATA as a labelled suggestion.

- `virmill update` also requires each download, and `SHA256SUMS` itself, to
  match the SHA-256 digest GitHub recorded for the upload, as the install script
  does. It refuses a checksum file that disagrees with the uploaded files, and a
  file GitHub lists without a SHA-256 digest.

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
- `operation.list` adds each job's plan `operation`, `resourceIDs` and, when
  known, `targetName`. Existing fields and `operation.get` are unchanged.
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
