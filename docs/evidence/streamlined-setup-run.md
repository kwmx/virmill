# Native check of storage pool creation and the streamlined setup (684ffa6)

Scope: storage pool creation ([ADR 0056](../adr/0056-storage-pool-creation-and-setup-defaults.md))
and the streamlined import flow, before the next owner beta. This is not a
release and promotes no status. All 71 acceptance scenarios remain mandatory.

## Build

`684ffa60cd799db79c483c188d9f5516c2f20370` is the code of `7663fc7` plus evidence.
The product version is still `1.0.0-beta.4`, so the build is identified by its
revision. Software regressions `storage-pool-regression-001` (`ad07259`) and
`streamlined-setup-regression-001` (`7663fc7`) passed, as did the full Go test
suite and vet.

## Native results

Both runs used the authorized Fedora 44 test VM through the installed packages.
The TUI used a private client state directory.

| Evidence | Result |
| --- | --- |
| `streamlined-684ffa6-install-native-001` | **passed** |
| `pool-creation-native-001` | **passed** |

The install replaced the beta.4 RPMs with this build (`rpm -U --replacepkgs`),
checked the three installed binary hashes, version and revision, the CLI
inventory commands and an 80×24 TUI start and exit. Guest and network
definitions, source-media metadata and the inactive helper policy were preserved.

The pool probe, on `qemu:///session`:

- **Existing folder** (mode 0750, one file): the pool was defined, started and
  set to autostart. The folder's mode, owner and SELinux label and the file's
  bytes were unchanged, the file was listed as a volume, and Virmill listed the
  pool as active.
- **Refusals**, each with no new pool or folder: the same folder, the same name,
  a folder inside the new pool, and `/etc`.
- **Missing folder** with `--no-autostart`: libvirt created the folder; the pool
  runs without autostart.
- **System connection**: planning libvirt's default pool was refused because
  existing system pools use folders inside `/var/lib/libvirt/images`, as ADR 0056
  requires. Nothing was applied on the system connection.
- **TUI** at 120×36: Storage lists the new pools and offers Create pool. Task
  search opens Create storage pool, whose review states the folder and warnings.
  Esc cancelled it with nothing applied.

The probe leaves its two session pools, `virmill-pool-62e06457` and
`virmill-pool-62e06457n`, with their folders in its stage for review.

## Not covered

- Creating a pool from inside VM setup, and selecting it automatically, were
  checked by TUI state tests only.
- The system default pool at `/var/lib/libvirt/images` was not created: the test
  VM already has pools inside that folder. A host with no pools is the target
  case.
- The default import folder, the direct VM review after preparation and Start VM
  were checked by software tests only. A native import walk-through remains.
