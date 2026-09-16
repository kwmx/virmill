# 1.0.0-beta.7 owner pre-release verification

Scope: REL-01 and REL-04 release evidence. All 71 acceptance scenarios remain
mandatory; 2 are accepted. No status is promoted, and this is not 1.0
qualification.

## Build

`4f2b31e4837aee77bd8c4f48fd9d502d99bb0497` is the version bump to `1.0.0-beta.7`, built offline with the pinned
Go 1.27.1 toolchain and vendored dependencies. Since beta.6 (`3e24ce8`) it adds
only `f01cc89` and `8da1ea6`: every VM button shows its key (`v` Display,
`o` Boot / installer), a running VM's details show Display once, and pages
with keyless buttons show **Tab Buttons**.

`make verify` passed before the bump commit. `make release-check` still fails
closed, naming every unaccepted scenario. The source archive is `git archive`
of the build commit.

The button changes are covered by the TUI's unit tests
(`TestVMButtonsShowTheirKeys`, `TestWorkspaceButtonsWrapInsteadOfHiding`) and
were rendered at 80x24. No native run exercised them; New VM and display native
evidence is recorded for beta.6 ([release record](beta6-release-run.md)).

## Package checks

| Evidence | Result | What it established |
| --- | --- | --- |
| `beta7-rpm-upgrade-native-001` | **passed** | RPM upgrade from beta.6 on the Fedora 44 test VM: installed binaries match the packages, revision `4f2b31e…`, user coordinator active, the TUI starts and exits at 80x24, guests, networks, source-media metadata and the helper policy preserved |
| `beta7-deb-install-container-001` | **passed** | Debian 13 and Ubuntu 24.04 containers: archive checks, lintian, apt installation of `1.0.0~beta.7`, `dpkg --verify`, systemd unit checks, `virmill version` reporting revision `4f2b31e…`, and a purge that left nothing behind |

## Artifacts

| File | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.7.x86_64.rpm` | `b3f2eb4b10b782387f47b9fe9752bcd351895b465189cdbd2e14a0a85f1fb29d` |
| `virmill-host-helper-1.0.0-0.beta.7.x86_64.rpm` | `9b39ab10723fff2344bf36048c3fd86c3a30aea3f44a1c81ddc5ad6e3f6d4fe9` |
| `virmill_1.0.0.beta.7_amd64.deb` | `a232bcfb72f2c7e2178e2520ade16eaac4398f077cf871a11196ac29079a16b8` |
| `virmill-host-helper_1.0.0.beta.7_amd64.deb` | `99cc738cd00fd51af1f0ace58e2c6e4a27cda946a75583a649e023f9d5549930` |
| `virmill-1.0.0-beta.7-source.tar.gz` | `442db4afe3b2357ef80a13fbe5a9dac74e4f7d1c79cf517d42937d64d2e71295` |

## Ships unfixed

- On a `qemu:///system` host whose pool images are readable only by root or
  qemu, removing a VM together with its disks is refused with
  `OPERATION_FAILED: permission denied`. Removing the definition and keeping the
  disks works.
