# Pre-release native check of b627747 (TUI redesign)

Scope: native regression runs of the TUI redesign (frame, list columns and
details, home screen and Settings, task menus and help) before the next owner
beta. This is not a release and promotes no status. All 71 acceptance scenarios
remain mandatory.

## Build

`b62774765880b5354e8dd575a92b54f834c316a0` is `2be7f70` plus the
`--replace-same-version` option of `install_owner_beta.py`. The product version
is still `1.0.0-beta.3`, so the build is identified by its revision, which
`virmill version` reports. `make verify` passed, and so did the package
integration test with the private IPC opt-in.

## Native results

All runs used the authorized Fedora 44 test VM through the installed packages,
with a private client state directory for each TUI probe.

| Evidence | Result |
| --- | --- |
| `prerelease-b627747-install-native-001` | **passed** |
| `prerelease-b627747-tui-workspace-native-001` | **passed** (80×24 and 120×36) |
| `prerelease-b627747-network-form-native-001` | **passed** |
| `prerelease-b627747-general-sources-native-001` | **passed** |
| `prerelease-b627747-resource-settings-native-001` | **passed** |
| `prerelease-b627747-autostart-native-001` | **passed** |
| `prerelease-b627747-job-activity-native-001` | failed: probe expectation |
| `prerelease-b627747-job-activity-native-002` | **passed** |
| `prerelease-b627747-disk-removal-native-001` | refused before any action |

The install replaced the beta.3 RPMs with this build (`rpm -U --replacepkgs`),
checked the three installed binary hashes, version and revision, the CLI
inventory commands and an 80×24 TUI start and exit. Guest and network
definitions, source-media metadata and the inactive helper policy were
unchanged.

Job activity 001 failed because the probe looked for the operation ID in the
selected Jobs row, which now names the task and its VM. The saved screen showed
the correct row selected. The probe now checks the row position; the details
page still proves the exact job ID. Run 002 passed with that probe.

Resource settings submitted a CPU/RAM change through the TUI on its own stopped
fixture, read back 2 vCPUs and 256 MiB, and undefined the fixture. Autostart
enabled and then restored automatic startup on the retained stopped guest. No
guest was started. Those two runs added three succeeded jobs; all 41 jobs are
terminal, including the historical recovery-required fixture job.

Disk removal 001 stopped at its precondition: the `qemu:///session` inventory
must be empty, and it still holds the storage pool retained by
`disk-removal-native-001`. Nothing was created or deleted. The pool is left for
the owner to decide on.

The owner's draft directory held only its lock file after every run.

## Artifacts

| File | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.3.x86_64.rpm` | `2a8b754039d74deba504c2a4924d78cdefccb30fa888a8f1b0be453e3c28c278` |
| `virmill-host-helper-1.0.0-0.beta.3.x86_64.rpm` | `aaf3e921d1e7f215c081cc261b8de704bbd34c93b9f29cf29d606d06a7d3f5d2` |
| installed `/usr/bin/virmill` | `532a72a3e7f36de3735a7f07ec5630598e4b51e435e25538ad921901b496031a` |
| installed `/usr/bin/virmilld` | `a5b54c0e8fc4be685dc9e83818a225dce5e7aaddc4fc3546ccb1769623ce55e2` |
| installed `/usr/libexec/virmill-host-helper` | `9acaad6aaae06d75e3a49072ff68992e9913330c6c902d8998a99623bdd165de` |

These packages are test builds and are not published. No guest OS family,
physical hardware or Debian/Ubuntu host is qualified by these results.
