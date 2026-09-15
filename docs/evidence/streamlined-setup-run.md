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

## Pools inside VM setup

`pool_setup_probe.py` checks **Create storage pool** and **Start pool** inside VM
setup, which need a host without a usable pool. It makes one without touching
the test VM's own. Every XDG folder points to private folders, so
`qemu:///session` starts private libvirt daemons with no pools, networks or VMs,
and the probe runs its own coordinator there. Libvirt keeps session sockets under
`XDG_CONFIG_HOME`, and a Unix socket path must be shorter than 108 bytes, so the
runtime and libvirt configuration folders live in a short private folder in the
user's `/run/user` folder. The host's own pools, networks and VMs are listed
before and after.

| Evidence | Build | Result |
| --- | --- | --- |
| `pool-setup-native-001` | `b2d5662` | failed: probe environment. The private configuration folder was deep in the stage, so libvirt's QEMU probe socket exceeded 108 bytes and VM setup reported no KVM machine. The probe now keeps those folders short |
| `pool-setup-native-002` | `b2d5662` | failed: probe check. Its focus test matched a choice's closing `>` together with the button on the next line, so it pressed Enter on the pool field; VM setup correctly said to choose Create storage pool. The shared probe helpers now match within one line |
| `pool-setup-native-003` | `b2d5662` | failed: **product defect**. Create storage pool and the first VM passed, but the second import stopped at "A setup was submitted" although the first setup had finished. Fixed in `1d7d1ae` |
| `pool-setup-native-004` | `1d7d1ae` | **passed** |

In run 4:

- **Create storage pool.** VM setup selected no pool, said "No storage pool yet",
  and offered Create storage pool. Its review had one item (`host-mutation`) and
  named libvirt's `default` pool. VM setup stayed open with "Creating storage pool
  default…", then "Storage pool default is ready and selected." One review of 8
  items ran `import.prepare-disks`, `vm.create.devices-v1`, `vm.start` and
  `import.discard`. The VM was running 8 s after that review was applied, and the
  imports folder was empty.
- **Start pool.** The probe stopped the pool in the private session. VM setup said
  "Your storage pools are stopped" and offered Start pool default next to Create
  storage pool. One review item started it, the form showed "Starting storage pool
  default…" and selected it, and one approval reached a running VM in 9 s.
- The pool is active, starts with the host, and uses libvirt's session default
  folder, `$XDG_DATA_HOME/libvirt/images`. The private coordinator ran exactly
  `storage.pool.create` and `storage.pool.start` once and `import.prepare-disks`,
  `vm.create.devices-v1`, `vm.start`, `import.discard` and `vm.hard-stop` twice,
  all succeeded. Both VMs were hard-stopped through reviewed plans. The host's
  own pools, networks and VMs were unchanged.

The install `drafts-1d7d1ae-install-native-001` passed. Left for review: the
private pool and the two stopped VMs. Their images stay in the probe's stage;
their libvirt definitions are in the private `/run/user` folder, which the test
VM clears when it restarts.

## Not covered

- The system default pool at `/var/lib/libvirt/images` was not created: the test
  VM already has pools inside that folder. The in-setup runs used the session
  default instead.
- The default import folder, the direct VM review after preparation and Start VM
  are covered natively by the
  [one-approval import record](one-approval-import-run.md).
