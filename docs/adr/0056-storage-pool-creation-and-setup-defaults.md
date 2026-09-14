# ADR 0056: Create storage pools in Virmill and suggest setup defaults

Status: implemented; native evidence recorded separately.

A first VM or import stopped at "No active file-based pool", and the Storage page
sent users to another tool to create one. The owner asked for one program with
simple defaults and optional advanced settings. Specification document 03 lists
`storage pool create`, STO-01 covers pool creation, and ADR 0006 forbids only
implicit creation or activation, not an explicit reviewed one.

`storage.pool.create` is an ordinary reviewed plan and durable job with one
acknowledgement, `host-mutation`. With no input it plans libvirt's standard pool:
name `default`, folder `/var/lib/libvirt/images` on `qemu:///system`, or
`$XDG_DATA_HOME/libvirt/images` (normally `~/.local/share/libvirt/images`) on
`qemu:///session`, starting with the host. Name, folder and autostart are
optional (`--name`, `--path`, `--no-autostart`). Only persistent directory pools
are created.

The job defines, starts and then enables autostart. Before definition a durable
record binds the new pool UUID to the job. Every step reconciles by that UUID and
is never replayed; an uncertain step needs recovery. Planning and each step refuse
an existing name or UUID, a folder equal to, inside or containing another pool's
folder, system folders, top-level folders and non-canonical paths. Existing pools
are never adopted, started, changed or removed. Libvirt creates a missing folder
with its default mode; an existing folder keeps its owner, mode, security label
and files, and those files are listed as volumes. Nothing here deletes a pool or
its folder. Libvirt has no create-only definition call, so an external writer
between the final check and definition can still collide; that is reported as
needing recovery, never repaired.

`storage.pool.start` starts an existing stopped persistent file-based pool
(`dir`, `fs`, `netfs`) as a reviewed job, bound to the pool's reviewed
fingerprint, and by default also enables autostart (`--no-autostart` leaves it
unchanged). It never builds a folder, redefines, stops or deletes a pool; a
missing folder fails with libvirt's reason. VM setup offers Start pool NAME,
preferring `default`, when stopped pools are the only candidates, and follows
it like Create storage pool.

The Storage page offers Create pool, and its empty page no longer names other
tools. In VM setup, when no usable pool exists, Create storage pool shows the
review over the form. After approval the form stays open and editable while the
job runs; the exact new pool (UUID, name, active) is then selected. A failed job
is explained in the form. The VM draft stays editing, never submitting, and the
pool job remains in Jobs with its own completion card.

VM setup now preselects visible defaults instead of leaving required choices
blank: libvirt's `default` pool, or the only usable pool, and the firmware the
source declares. Without a declaration BIOS is preselected and labelled
*Suggested*, which records the assumption required by specification document 05;
the review shows the final choice. With several pools and no `default`, the user
chooses. This supersedes the earlier rule that unknown firmware is left unset.

Disk sets and installers, the user's own images, also get one new e1000e
adapter on libvirt's active `default` network when that network uses NAT,
connected and labelled *Suggested*; e1000e has in-box drivers in common Linux
and Windows guests. Appliance adapters keep their mapping and start disconnected,
as specification document 05 requires for untrusted appliances. Without an active
default NAT network no adapter is added and the user chooses. An appliance
adapter with no network is suggested the active default NAT network, still
disconnected, with the e1000e model. An appliance disk on a controller QEMU cannot
offer, such as VMware SCSI or IDE on Q35, gets SATA as a labelled suggestion.

Import follows the same approach. Prepared copies default to a new folder under
`$XDG_DATA_HOME/virmill/imports` (normally `~/.local/share/virmill/imports`),
named after the VM and the time. The TUI creates only that private parent, mode
0700, just before the preview; the preparation job still creates and reviews the
destination itself, and a folder the user chose is never created. When
preparation succeeds and the VM settings are complete, the creation review opens
directly; incomplete settings open the form at the missing field. A created VM's
result offers Start VM, which opens the VM and shows the ordinary start review.
These change presentation only: preparation and creation remain two reviewed
operations (ADR 0037).

STO-01 remains in progress: disk growth, move and space accounting, and native
evidence for pool creation, are separate.
