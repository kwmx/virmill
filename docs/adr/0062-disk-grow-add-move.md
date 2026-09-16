# ADR 0062: Grow, add and move a VM's disks

Status: accepted for Grow, which is implemented, with native evidence
`disk-grow-session-native-002` and `disk-grow-system-native-002`
([run record](../evidence/disk-grow-run.md)); Add and Move are proposed.

## Context

Roadmap phase 2 and STO-01 ask for disk management on existing VMs. Virmill can
create VMs with disks and remove selected disks (ADR 0054), but cannot grow a
disk, add one or move one to another pool. Users otherwise need another tool,
which the owner asked to avoid.

The constraints are the ones creation and removal already follow. Every change
is a reviewed plan run as a durable job with a receipt; recovery observes and
never replays. The unprivileged coordinator writes new volumes only through
libvirt upload and verifies them by read-back (ADR 0057–0060). Untrusted images
are parsed only by the sandboxed `qemu-img` (ADR 0003, ADR 0005). XML changes
preserve unknown content (`xmlpatch`). Disks are named by guest target, as in
disk removal.

## Decision

Three new `vm.plan` actions, each with its own schema and versioned operation
so older builds refuse them: `grow-disk` (`vm.disk.grow-v1`), `add-disk`
(`vm.disk.add-v1`) and `move-disk` (`vm.disk.move-v1`). The first version works
on stopped VMs only, like other hardware edits. The CLI gets `vm disk grow`,
`vm disk add` and `vm disk move`; the TUI gets one form for each beside Remove.
They are built in this order, smallest first.

### Grow

- Input: the guest target and a larger size; shrinking is refused.
- Effect: `StorageVol.Resize` without shrink or allocate flags. The VM XML does
  not change, but the plan still binds the VM fingerprint.
- Space: libvirt checks pool space only when allocating, so Virmill checks the
  growth against the pool's free space and asks for `pool-overcommit` when the
  growth exceeds it.
- Refused: disks with a backing chain or internal snapshots, and disks another
  VM also uses.
- Security: on a system connection libvirt resizes a qcow2 image with
  `qemu-img` as root. This is allowed only for a VM's own managed volume, which
  QEMU already opens on every boot, never for an imported or untrusted image.
  This narrows ADR 0005 for this case.
- The review says the guest's partitions and filesystems are not grown.
- Completion: the volume's capacity equals the request and its key, path and
  generation are unchanged.

### Add

- Defaults: 20 GiB, in the boot disk's pool, on the boot disk's bus, at the
  next free target.
- Effect: a blank qcow2 made by the sandboxed tool, uploaded and read back as in
  creation, then one `<disk type="volume">` added to the saved definition.
- Refused: a bus with no free port; no controller is inserted in this version.
- Recovery: until the saved definition changes, a failure has written at most a
  new, unreferenced volume. Nothing unsafe is in flight and nothing needs a
  replay, so the job fails plainly, releases this VM's locks and names the
  unused file so it can be deleted. Only a failure at or after the define
  leaves the job needing recovery, which then observes the volume and the
  definition instead of writing again.
- Disposition: an addition left needing recovery is closed by
  `vm.disk.add.dispose-v1`, which inherits its locks and records one durable
  disposition. Accepting keeps the reviewed disk and is offered only when the
  saved definition names it; deleting removes the new volume and only while no
  definition names it, under the generation and dependency guards creation
  cleanup uses. Neither uploads nor defines anything. Without this an addition
  that failed after its define would hold the VM and its pool with no way out,
  which happened twice on the test host before it existed.
- The definition is compared with the digest that tolerates libvirt's own
  reformatting, as cold restore does, and the readback also confirms the stored
  definition names the reviewed target with the reviewed pool and volume. A
  byte-exact comparison of Virmill's own XML cannot hold: libvirt reindents and
  requotes what it stores.

### Move

- Effect: allocate in the destination pool, copy by download and upload with a
  SHA-256 over one connection, read back, point the disk at the new volume, then
  delete the old volume after the reference checks of ADR 0054.
- Acknowledgements add `data-loss-delete-old-copy` unless the old copy is kept.
- `StorageVolCreateXMLFrom` is not used: it converts as root and its output
  cannot be checked byte for byte.
- Peak space is two copies until the old one is deleted; the review says so.
- Deletion is never replayed during recovery.

## Consequences

- Growing, adding and moving disks no longer need another tool.
- Running VMs need a shutdown first. Live resize, attach and block copy can
  follow in a later ADR.
- A full clone of a VM (STO-02) reuses the copy and read-back from Move and is
  decided separately.
