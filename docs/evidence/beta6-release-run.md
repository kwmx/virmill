# 1.0.0-beta.6 owner pre-release verification

Scope: REL-01 and REL-04 release evidence for New VM (ADR 0065) and the
per-user libvirt socket (ADR 0064). All 71 acceptance scenarios remain
mandatory; 2 are accepted. No status is promoted, and this is not 1.0
qualification.

## Build

`3e24ce8f41a3674e48e3ca9b835c167002fd0d34` is the version bump to
`1.0.0-beta.6`, built offline with the pinned Go 1.27.1 toolchain and vendored
dependencies. Since beta.5 (`cdcd095`) it adds:

- the per-user libvirt socket units (`ba6f7e6`, `6efe903`, ADR 0064);
- one plain confirmation for every change (`74ed286`);
- the display in its own window, opened after start, and the Display button
  (`25d2dba`);
- New VM (`85a1c09`, `9a2f6dd`, `c5a5290`);
- the walkthrough probe and its fixes.

`make verify` passed before the bump commit: the full suite, `go vet`, the
traceability matrix check and the private-values check. `make release-check`
still fails closed, naming every unaccepted scenario, which is why this is a
pre-release.

The source archive is `git archive` of the build commit.

## Package checks

| Evidence | Result | What it established |
| --- | --- | --- |
| `beta6-rpm-upgrade-native-001` | **passed** | RPM upgrade from beta.5 on the Fedora 44 test VM: installed binaries match the packages, revision `3e24ce8…`, user coordinator active, the TUI starts and exits at 80x24, guests, networks, source-media metadata and the helper policy preserved |
| `beta6-deb-install-container-001` | **passed** | Debian 13 and Ubuntu 24.04 containers: archive checks, lintian, apt installation of `1.0.0~beta.6`, `dpkg --verify`, systemd unit checks, `virmill version` reporting revision `3e24ce8…`, and a purge that left nothing behind |

## Native feature evidence

The builds the native runs used differ from the release build only in the
version constant and later records.

| Evidence | Build | Result |
| --- | --- | --- |
| `new-vm-baseline-native-001` | beta.5 | the old path: 72 keypresses, two reviews, ten checkboxes, no display |
| `new-vm-walkthrough-native-005` | `9a2f6dd` | **passed**: disk image, ISO and OVA on `qemu:///session` from no pools, 3 keys and one confirmation each, viewer started |
| `new-vm-system-native-002` | `c5a5290` | **passed**: the same on `qemu:///system` with several pools and the default network, with screenshots of each viewer window drawing the VM's screen |
| `session-socket-native-001` | `ba6f7e6` | **passed**: a session VM started after libvirt's idle exit with no manual step |

Failed runs in these series are listed in their records:
[New VM](new-vm-run.md), [session socket](session-socket-run.md).

## Artifacts

| File | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.6.x86_64.rpm` | `4d866fba5fe4f2f65b134dce3fbecc87a4b3c14942e7429ee19cc48a597acdcd` |
| `virmill-host-helper-1.0.0-0.beta.6.x86_64.rpm` | `045f8d684a71238d4eb4dd5eb2b8e04fcd30737e94293742e311db5aadcf0f5b` |
| `virmill_1.0.0.beta.6_amd64.deb` | `e3bb8c6cfdfd360a054cd0b586d3a0f7c28cc9db80ed9bd7721076c99d7ce13d` |
| `virmill-host-helper_1.0.0.beta.6_amd64.deb` | `3656380740983df02d6dbe7ad16531972d223b3fe46896d98ab805adb4ad73b9` |
| `virmill-1.0.0-beta.6-source.tar.gz` | `22941ca65a34b4adf22a24ba6c6cb4830b2385266d2e9ccaa53198462a01a5e0` |

## Ships unfixed

- On `qemu:///system`, removing a VM together with its disks is refused when
  other guests on the host use disk files outside storage pools
  (`RECOVERY_REQUIRED: disk source outside reconciled storage pools`). Removing
  the definition and keeping the disks works.
- Not walked natively: the system connection with no pool, a real desktop
  session, and appliances whose adapters need a network on a host with none.
