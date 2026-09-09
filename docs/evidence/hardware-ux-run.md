# Guided hardware and import recovery evidence

Scope: IMP-01, IMP-03, UX-01, UX-02 contributions. All 71 complete acceptance
scenarios remain required; no complete guest-boot or firmware-state support claim.

## Implemented behavior

- Readable required/available/missing storage figures and a visible destination
  recovery action; detected disk capacity prevents misleading lower limits.
- Unambiguous OVF CPU/RAM defaults, standard omitted-unit disk bytes and verified
  VirtualBox `MegaBytes` handling. Unknown values require entry.
- Native controls for CPU/RAM, supported machine/CPU/firmware, storage, all source
  disks/media and explicit original/new network adapters. Hardware can be selected
  before preparation and is bound to that exact request. Completed preparations
  can be selected after reconnecting without repeating conversion.
- Exportable settings and explicit no-overwrite 0700 folder creation in the explorer.
- Focused errors, retained review/submission-failure edits, and safe asynchronous
  handoff without replacing another open task.

## Executed evidence

`hardware-ux-race-001` failed only because an existing exact-count search test did
not include the new CPU-capability command. `hardware-ux-race-002` passed all ten
affected Go package groups with race detection and actual temporary IPC sockets.
`hardware-ux-artifacts-001` passed all three package, staged installer/uninstaller
and private daemon/CLI/TUI integration tests. These are software evidence.

On the owner-authorized Fedora 44 disposable VM, source `76354da` was installed
with verified CLI/daemon hashes. `hardware-ux-upgrade-001` and
`hardware-ux-restart-001` preserved all existing guests, jobs, journal tables and
source media. No helper, host network or existing guest was changed.

`hardware-ux-native-001` correctly stopped before preparation because its fixture
assumed BIOS appeared in libvirt's autoselection enum. Investigation established
that enum does not enumerate QEMU's default ROM path. The software correction has
separate `hardware-ux-bios-001` race evidence and positive/negative capability tests.

`hardware-ux-native-002` completed real two-disk preparation but its test expected
Enter to open a destination directory; the existing browser correctly selected it.
The corrected test retained that run and continued in a new output directory.

`hardware-ux-native-003` passed an actual 80x24 terminal walkthrough with the installed
binary: detected resources, edited CPU/RAM, explicit pool/UEFI choice, both SATA disk
mappings, settings export and explorer folder creation. The actual TUI plan was then
applied through the shared CLI service. Native libvirt XML confirmed **3 vCPUs,
1024 MiB RAM, two disks and no NICs**, with the new guest powered off. No boot was
attempted. Existing guests, existing jobs, owner media and the synthetic source
were preserved.

Retained native outputs: `~/virmill-tests/hardware-ux-76354da/native-003` on the
explicitly authorized test VM. New guest UUID: `e229e858-824f-4d15-af36-90253ae52367`.
Preparation operation: `faf39494-e5ec-4a05-9090-1be0cab7b730`; creation operation:
`07549f24-063d-4cd6-a2ad-b8c29dec59cc`. Tiny fixtures and created resources remain
available for inspection; no cleanup or unrelated resource deletion was performed.

Three agents independently implemented the native form/explorer, OVF/space handling,
and native capability observation/service tests. Root owned shared contracts,
integration, review corrections, packages, tracking and every remote mutation.

## Remaining limits

Incomplete wizard edits do not automatically survive a TUI restart; use explicit
settings export. Accepted plans/jobs and completed source metadata are durable.
Full firmware/TPM restoration, guest boot compatibility, multi-NIC routing and all
other mandatory release scenarios retain their existing unqualified/incomplete
statuses. A powered-off UEFI definition does not verify NVRAM or TPM restoration.
