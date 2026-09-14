# 1.0.0-beta.4 owner pre-release verification

Scope: REL-01 and REL-04 release evidence, plus scoped UX, network, job and
updater checks. All 71 acceptance scenarios remain mandatory. No status is
promoted, and this is not 1.0 qualification.

## Build

`f6a2c9a98bfcc272765ca93f99fd4f40ffc1607b` is the version bump to
`1.0.0-beta.4` (`1fbc5f0`) plus one fix. Since beta.3 it adds the readable
`virmill doctor` (`294df97`), stripped binaries, the redesigned TUI (`036da3a`,
`0498b7f`, `e3b5b4b`, `2be7f70`) and `virmill update` (`6bb2162`, ADR 0055).

The package integration test found a regression while this build was checked.
From `0498b7f`, `operation.get` returned the plan's `operation`, `resourceIDs`
and `targetName`, and the CLI's strict job follower rejected them, so
`plan apply --wait` failed. `f6a2c9a` returns the plain job from
`operation.get` again; `operation.list` keeps the extra fields. Earlier runs
had checked only the test's last output line, which is printed either way. The
test was rerun on the published beta.3 source, `0627800`, and passed there, so
the beta.3 record stands; the pre-release record for `b627747` is corrected.

`make verify` passed, and all three package integration tests passed with the
private IPC opt-in, judged by their results. `beta4-reproducibility-001` built
twice from a clean worktree of the build commit; both produced binaries and
packages identical to the delivered files. `beta4-deb-install-container-001`
**passed** in Debian 13 and Ubuntu 24.04 containers: archive checks, apt
installation, `dpkg --verify`, unit checks, `virmill version` as an ordinary
user and a clean purge. lintian reports no errors, only warnings,
informational and experimental tags. The source archive is `git archive` of the
build commit, as for beta.3.

## Updater

`update-regression-001` ran the update, CLI and TUI tests, and
`update-live-github-001` verified the published beta.3 RPMs from GitHub against
`SHA256SUMS` and rpm's own package metadata. `update-e2e-install-native-001`
**passed** on the test VM: a test build of `9f6176d` labelled `1.0.0-beta.2`
(`update-e2e-testbuild-install-native-001`) ran `virmill update --yes`, which
downloaded and verified the published beta.3, installed it through dnf with one
confirmation and no password, and restarted the coordinator. The installed
binaries matched the published beta.3.

## Native results

All runs used the authorized Fedora 44 test VM through the installed packages,
with a private client state directory for each TUI probe. The VM started from
the published beta.3, installed by the updater test.

| Evidence | Result |
| --- | --- |
| `beta4-install-native-001` | **passed** |
| `beta4-tui-workspace-native-001` | **passed** (80×24 and 120×36) |
| `beta4-network-form-native-001` | **passed** |
| `beta4-general-sources-native-001` | **passed** |
| `beta4-update-check-native-001` | **passed** |
| `beta4-job-activity-native-001` | failed: probe expectation |
| `beta4-job-activity-native-002` | **passed** |

The install upgraded beta.3 in place and checked the three installed binary
hashes, version and revision, the CLI inventory commands and an 80×24 TUI start
and exit. Guest and network definitions, source-media metadata and the inactive
helper policy were unchanged. The installed beta.4 reached GitHub and reported
itself as the newest release. No plan was applied and no guest was started. All
43 jobs are terminal, including the historical recovery-required fixture job,
and the owner's draft folder held only its lock file.

Job activity 001 failed because the probe compared a job from `operation list`
with the same job from `operation show`; the list now also carries the plan's
task fields. The probe compares the stored job fields.

SPICE creation, removal and resource probes were not rerun for this build;
they passed on `b627747` (`prerelease-b627747-run.md`), and the only product
change since then outside the updater is the `operation.get` fix.

## Artifacts

| File | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.4.x86_64.rpm` | `1081f117391876d8e659433fd584ad4b984024cab59b31baea5318d162799838` |
| `virmill-host-helper-1.0.0-0.beta.4.x86_64.rpm` | `057e9a59d52f40c397023b9525c53f3a8d1630d2c0e8921e394d4f7d4f9cca83` |
| `virmill_1.0.0~beta.4_amd64.deb` | `b07d74af941433977c2f5c9af26398d1f7b10a8a665f41caa42ed0bbd481d052` |
| `virmill-host-helper_1.0.0~beta.4_amd64.deb` | `003b70716a3f0d2912dc5eb536c2e70f0f79c8065512dc987a526c8a78d7b385` |
| `virmill-1.0.0-beta.4-source.tar.gz` | `86d06bc20b878ae257ac516a0faa7e19187b7c6bfc3583ec9badd52aa1717fb3` |
| installed `/usr/bin/virmill` | `755678817cf24959478d385397ad98f2ae7258e3816e0b85fca0746e0c7d897a` |
| installed `/usr/bin/virmilld` | `1cb78f9a8ad5775a038088bb4f1505cd036ade8a62f5a3a1822adf6edc91cd26` |
| installed `/usr/libexec/virmill-host-helper` | `29d56c012aa3577549284af98ecaaa888648da7d5343cdf731a7e05816687055` |

GitHub serves the DEBs as `virmill_1.0.0.beta.4_amd64.deb` and
`virmill-host-helper_1.0.0.beta.4_amd64.deb`; the bytes are the same. The
packages are unsigned. No guest OS family, physical hardware or real
Debian/Ubuntu host is qualified by these results.
