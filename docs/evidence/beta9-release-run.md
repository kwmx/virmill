# 1.0.0-beta.9 owner pre-release verification

Scope: REL-01 and REL-04 release evidence. All 71 acceptance scenarios remain
mandatory; 2 are accepted. No status is promoted, and this is not 1.0
qualification. Linked clones moved after 1.0 by owner-approved scope amendment
(ADR 0067); the release gate is unchanged.

## Build

`9f619906405133089c9d2115d16ec408cbf95302` is the version bump to `1.0.0-beta.9`, built offline with the pinned
Go 1.27.1 toolchain and vendored dependencies. Since beta.8 (`86171b5`) it adds
full clones (`0b9b26a`, `2389657`, ADR 0066), the boot-disk move record and
probe check (`b69e149`), and the scope amendment for linked clones (`45206fa`).

`make verify` passed before the bump commit. `make release-check` still fails
closed, naming every unaccepted scenario. The source archive is `git archive`
of the build commit.

Feature evidence: [vm-clone-run.md](vm-clone-run.md) on `2389657` and the
boot-disk moves in [disk-system-run.md](disk-system-run.md) on beta.8; both
builds differ from this one only in the version constant and records.

## Package checks

| Evidence | Result | What it established |
| --- | --- | --- |
| `beta9-rpm-upgrade-native-001` | **passed** | RPM upgrade from beta.8 on the Fedora 44 test VM: installed binaries match the packages, revision `9f61990…`, user coordinator active, the TUI starts and exits at 80x24, guests, networks, source-media metadata and the helper policy preserved |
| `beta9-deb-install-container-001` | **passed** | Debian 13 and Ubuntu 24.04 containers: archive checks, lintian, apt installation of `1.0.0~beta.9`, `dpkg --verify`, systemd unit checks, `virmill version` reporting revision `9f61990…`, and a purge that left nothing behind |

## Artifacts

| File | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.9.x86_64.rpm` | `bee6f54f9570173e489ce3c722dd59b862026cdfc4fcc373b3241ade90f1ab0e` |
| `virmill-host-helper-1.0.0-0.beta.9.x86_64.rpm` | `a8da1cb2e27aff8c1657a6bccdcc31ee9596a28b42b9acb30d3d8728815ccb81` |
| `virmill_1.0.0.beta.9_amd64.deb` | `da4accf2cbbd97e60f7638e0d954a6a55cdff184e11406404c03a2687685728c` |
| `virmill-host-helper_1.0.0.beta.9_amd64.deb` | `09f7965f4decb5cb1a0b2a1c214de2b40ea43b3dea57b9d74ffccacd1b5f6d88` |
| `virmill-1.0.0-beta.9-source.tar.gz` | `bbf6e75693e3873ff45a2d28ba5d483efbd8ea482fcb277fa10d97ba0ac060fb` |

## Ships unfixed

- The build commit's `packaging/completions/virmill.bash` predates `vm clone`,
  so CI's generated-files check failed on the `v1.0.0-beta.9` tag and on the
  record commit. The published RPM and DEB packages are not affected: packaging
  reads the regenerated file, and the RPM's bash completion includes `clone`.
  The source archive carries the older completion. `4d0fc98` commits the file
  on `main`, where CI passes again; the tag is left as published.
- Cloning a UEFI VM, cloning into one chosen pool and raw disks have software
  tests only.
- On a `qemu:///system` host whose pool images are readable only by root or
  qemu, deleting disks with a VM and deleting an unused volume through the
  disposition are refused with `permission denied`.
