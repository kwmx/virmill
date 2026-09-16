# 1.0.0-beta.5 owner pre-release verification

Scope: REL-01 and REL-04 release evidence for the roadmap's phase 2 work. All
71 acceptance scenarios remain mandatory and unaccepted. No status is promoted,
and this is not 1.0 qualification.

## Build

`cdcd0958f5f3a92ce395f680a70f4580a3068187` is the version bump to
`1.0.0-beta.5`, built offline with the pinned Go 1.27.1 toolchain and vendored
dependencies. Since beta.4 (`f6a2c9a`) it adds the everyday VM lifecycle:
power actions on both connections with an unanswered shutdown that frees the VM
(`a2f470b`), next-boot CPU and memory edits while a VM runs (`0c686ce`,
ADR 0061), disk grow (`b4b523b`, `1eae6f1`, `bdffe06`), removal of
Virmill-created and UEFI VMs (`858e4c0`, ADR 0063), disk add with a reviewed
disposition (`377f932`, `790665e`, `116e0e3`, `62bc9da`), disk move (`52670ad`,
`f707137`, `82ea6d8`, `ae2a832`), and the two stuck-VM fixes (`3439d5b`,
`cbfd91b`, `13f677b`, `6b2c32f`). It also carries the phase 1 import and pool
work that beta.4 predated.

`make verify` passed: the full suite, `go vet`, the traceability matrix check
and the private-values check. `make release-check` still fails closed, naming
every unaccepted scenario, which is why this is a pre-release.

`beta5-deb-install-container-001` **passed** in Debian 13 and Ubuntu 24.04
containers: `dpkg-deb` archive checks, apt installation of `1.0.0~beta.5`,
`dpkg --verify`, systemd unit checks, `virmill version` as an ordinary user
reporting revision `cdcd0958…`, and a clean purge that left nothing behind.
lintian reports no errors — informational tags plus the same warnings beta.4
carried (`hardening-no-pie`, `no-manual-page`, `duplicate-files`).

The source archive is `git archive` of the build commit, as for beta.3 and
beta.4.

## Native results

Every run used the authorized disposable Fedora 44 test VM through the
installed packages. The table is the whole series, not a selection: most of
these runs failed, and in nearly every case the *effect* on the host was
correct while Virmill's own confirmation, a message, or the probe was wrong.
That is what the runs were for.

| Evidence | Result | What it established |
| --- | --- | --- |
| `power-session-native-001` | **passed** | Start, pause, resume, save, restore, force off from paused and running, and an unanswered stop then force off, on `qemu:///session` |
| `power-system-native-001` | failed | A probe idempotency-key collision, before any change |
| `power-system-native-002` | **passed** | The same power set plus graceful reboot and stop, on `qemu:///system` with an Ubuntu guest |
| `resources-running-native-001` | **passed** | Next-boot CPU and memory edits while the VM ran |
| `disk-grow-session-native-001`, `-system-native-001` | failed | The disks grew, but a stale plan was refused with the wrong code |
| `disk-grow-session-native-002`, `-system-native-002` | **passed** | Grow on both connections, 1→3 GiB and 5→7 GiB, the second booted afterwards |
| `removal-created-native-001`, `removal-uefi-native-001` | **passed** | Definition removal keeping every volume, NVRAM and TPM state |
| `disk-add-session-native-001`…`-005` | failed | The volume and the disk were correct each time; the read-back compared digests of a document libvirt reorders, and a stranded job then held the VM until a disposition existed |
| `disk-add-session-native-006`, `-007` | failed | **The addition itself passed** in both: volume at the reviewed size, definition holding the reviewed disk. Both ended failed because the probe then started the VM and the host refused to execute QEMU — the per-user libvirt problem, not the disk |
| `disk-move-session-native-001`, `-002` | failed | The copy failed; the first hid its cause in one flattened message, the second named the hop and the cause (`EOF`) |
| `disk-move-session-native-003`, `-004` | failed | **The move itself passed** in both directions, including reclaiming a volume name a previous move had freed; the probe's own checks were wrong |
| `disk-move-session-native-005` | **passed** | Move end to end: copy verified, definition retargeted at the same target, bus and port, original deleted, VM started and forced off with the moved disk |

Records: [power](power-run.md), [running edits](running-edits-run.md),
[grow](disk-grow-run.md), [removal](removal-created-run.md),
[add](disk-add-run.md), [move](disk-move-run.md).

Add and move are qualified on `qemu:///session` only. The addition disposition
is software-tested and was used twice to free stranded VMs on the test host,
but has no recorded native run. Moving a booted guest's own boot disk is
untested. No guest operating system, physical hardware or real Debian/Ubuntu
host is qualified by these results.

## Known limitation shipped with this build

On a host with no per-user `virtqemud.socket` unit — Fedora 44 among them — the
session coordinator cannot start VMs unless a libvirt client has already been
run from a login shell, because the daemon the coordinator would start inherits
its no-new-privileges setting and can never launch QEMU. The daemon also exits
two minutes after it is last used, so the condition returns. Virmill now
refuses such a boot before making a plan and says what to run, and `virmill
doctor` reports it, but the manual step remains. Making the coordinator start
that daemon outside its own service cgroup, so the hardening stays and no
manual step is needed, is open.

## Artifacts

| File | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.5.x86_64.rpm` | `5fa1d274499c5cd56e90931bfcda0173208ee9103d30ea3138bb6cfd6e8f4da6` |
| `virmill-host-helper-1.0.0-0.beta.5.x86_64.rpm` | `d18568859c8e5bdeba8569d4d60dab2769d449c9726a3743a9c511400ec668cd` |
| `virmill_1.0.0~beta.5_amd64.deb` | `9e7af3e3afc0406f340225e96d616bebf22a2dc24a5d272dd6a7be7b1a2eee2f` |
| `virmill-host-helper_1.0.0~beta.5_amd64.deb` | `a445cbe4d93a6ba5eed4a706c5de4db3f07bbff6abd92d30037e5ca505c59294` |
| `virmill-1.0.0-beta.5-source.tar.gz` | `6b83c30042e5393fe270eb27d941b1ae4d0c68fc5f1324194222b2469159eca3` |
| built `build/bin/virmill` | `a5c1be6b4ef58ec3d1218260cfb52521a84e306bb2f1e8764bbd3d7c9f04b9f6` |
| built `build/bin/virmilld` | `bfca67215b6546c6c7acf2da9823fbabed8f78753d67ddce40bd6031d35a2ecd` |
| built `build/bin/virmill-host-helper` | `642df160ec9ca9d8067a35c082f7f4153b8add7ddafb06a0b2cd591d9f63ba09` |

GitHub serves the DEBs as `virmill_1.0.0.beta.5_amd64.deb` and
`virmill-host-helper_1.0.0.beta.5_amd64.deb`; the bytes are the same and
`SHA256SUMS` lists those names. The packages are unsigned.
