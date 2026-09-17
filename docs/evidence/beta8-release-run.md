# 1.0.0-beta.8 owner pre-release verification

Scope: REL-01 and REL-04 release evidence. All 71 acceptance scenarios remain
mandatory; 2 are accepted. No status is promoted, and this is not 1.0
qualification.

## Build

`86171b5112f56abfe4bc59aee2ae2aa98a794aba` is the version bump to `1.0.0-beta.8`, built offline with the pinned
Go 1.27.1 toolchain and vendored dependencies. Since beta.7 (`4f2b31e`) it adds
closing an addition whose volume never existed (`ee442ce`), the keep
disposition with the dependency graph built only for a deletion (`f7dff36`),
and the probe and records for phase 2b item 2.

`make verify` passed before the bump commit. `make release-check` still fails
closed, naming every unaccepted scenario. The source archive is `git archive`
of the build commit.

Feature evidence is recorded in [disk-system-run.md](disk-system-run.md): add
and move on `qemu:///system` on 1.0.0-beta.7, and the dispositions on
`f7dff36`, which differs from this build only in the version constant and
records.

## Package checks

| Evidence | Result | What it established |
| --- | --- | --- |
| `beta8-rpm-upgrade-native-001` | **passed** | RPM upgrade from beta.7 on the Fedora 44 test VM: installed binaries match the packages, revision `86171b5…`, user coordinator active, the TUI starts and exits at 80x24, guests, networks, source-media metadata and the helper policy preserved |
| `beta8-deb-install-container-001` | **passed** | Debian 13 and Ubuntu 24.04 containers: archive checks, lintian, apt installation of `1.0.0~beta.8`, `dpkg --verify`, systemd unit checks, `virmill version` reporting revision `86171b5…`, and a purge that left nothing behind |

## Artifacts

| File | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.8.x86_64.rpm` | `e28be839b27385d2fd07a61a854c8390e34f4e744f6253812a159f48d46026ce` |
| `virmill-host-helper-1.0.0-0.beta.8.x86_64.rpm` | `f912d6ed47731d49e833d5668448fa98fb15058a76e90ce94c5b535f8aa8cd9f` |
| `virmill_1.0.0.beta.8_amd64.deb` | `f7d19635645fe1e1a0da92648bef2fd7df59df9eccccbc598dcca0be89bb1f9f` |
| `virmill-host-helper_1.0.0.beta.8_amd64.deb` | `f067310bec1420ce8aafa1aa66c4fc8f8f3eaf0f2e5c542c25782ca1bf4c0545` |
| `virmill-1.0.0-beta.8-source.tar.gz` | `d9176e1dbff4d705a517226cb9224e0e0cc77f48cb9243126955d441a71df471` |

## Ships unfixed

- On a `qemu:///system` host whose pool images are readable only by root or
  qemu, deleting an unused volume through the disposition, and removing a VM
  together with its disks, are refused with `permission denied`. Keep, and
  removing the definition while keeping disks, work.
- Moving a booted guest's own boot disk is untested.
