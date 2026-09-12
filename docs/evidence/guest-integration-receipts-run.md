# Guest integration and receipt recovery beta evidence

Installed source: `e53f2e21e0ecf6b0c7a2bd6153f487df12d56236` (12 September 2026).
Implementation digest: `aeff9a74528122d8c825e24272e324fb3a0d2ed3b080a11ddd4a6cc5332395e9`.
Initial integrated source `96532cb` remains separately recorded below.
Go 1.27.1 and existing exact vendored dependencies; no dependency versions changed.

The beta adds guided existing-VM guest-agent connection setup, fresh state during
refresh, and portable backup receipts selected through a recovery form. It keeps
the unified OVA/ISO/existing-disk source wizard and its metadata/default distinction.
See [decision 0042](../adr/0042-guided-guest-integration-and-recovery-receipts.md),
[guest setup](../guest-agent-setup.md) and [backup recovery](../local-backups.md).

## Executed checks

- `guest-integration-receipts-core-001`: focused race tests passed across shared
  application, XML/native adapter, local backup, CLI and TUI packages.
- `guest-integration-receipts-core-002`: full race suite was blocked specifically
  by sandbox Unix socket `setsockopt` permission failures. The failed log remains.
- `guest-integration-receipts-core-003`: full Go race suite passed with temporary
  local IPC permitted. Native opt-in tests retain their own qualification rules.
  `go vet -tags libvirt_dlopen ./...` and `git diff --check` also passed.
- `guest-integration-receipts-fixture-001`: three pure native-fixture contract tests
  passed. This alone is not hardware evidence.
- `guest-integration-receipts-packages-001`: RPM/DEB structure, temporary staged
  install/uninstall preservation, private coordinator and an actual 80×24 terminal
  action-navigation check passed. All four core/helper RPM/DEB artifacts built.
- `guest-integration-receipts-native-upgrade-001` and `...native-restart-001`:
  authorized Fedora 44 VM core RPM replacement and idle ordinary-user coordinator
  restart passed. Existing eight jobs, journal contents, guests and media were
  preserved. The coordinator switched from PID 76838 to 81789.
- `guest-integration-native-channel-001`: real libvirt 12.0.0/QEMU 10.2.2 on the
  observed `pc-i440fx-10.2` KVM machine. A new stopped diskless/networkless fixture
  received one reviewed channel/controller addition through installed Virmill.
  Exact readback preserved opaque and unrelated configuration. Repeat enable was
  refused clearly. Both before/after 80×24 TUI guidance paths passed. The fixture
  was undefined after ownership checks; all preexisting guests/jobs/media remained.

Native fixture UUID: `ac2731be-c784-4a15-ac0e-c4af59f2a611`.
Initial definition SHA-256: `ca96201626d6791a9dec2d8badddd20de2bdeed187dd181e310a9cc8138bce7b`.
Remote evidence is retained under `~/virmill-tests/guest-integration-96532cb` on the
authorized host; local copies are ignored under `build/guest-integration-delivery`.
The native fixture never booted and has no guest OS. It proves configuration and
UI behavior, not agent package installation, agent response or OS compatibility.

Final terminal inspection led to a small UI cleanup: VM details now keep internal
fingerprints and duplicate identity fields behind `x`, and guest-tools guidance
and its footer match the observed setup state and actual Enter behavior.
`guest-integration-screen-polish-001` retained a failed obsolete text assertion;
`...screen-polish-002` passed after the assertion was updated. No product failure
was hidden. The final package checks (`guest-integration-final-packages-001`),
RPM replacement (`...final-upgrade-001`) and idle restart (`...final-restart-001`)
passed. Current coordinator PID is 82340.

`guest-integration-final-native-channel-001` repeated the bounded native edit and
both terminal paths on installed `e53f2e2`; all passed. Final fixture UUID was
`d5fcf23f-6c61-44ce-a287-38b952930a63`, definition digest
`2ca6d8a3a9422ba5ef115a93c4c16e29ed28ac68c3f9217aa94d389819b65129`.
It was undefined after verification. Final remote evidence is under
`~/virmill-tests/guest-integration-e53f2e2`, with local copies in
`build/guest-integration-final-delivery`. Earlier results remain intact.

## Installed artifacts

| Artifact | SHA-256 |
| --- | --- |
| `/usr/bin/virmill` | `472e4ae0ce295a6bd1c2169c0d5edc5a67086a018ddfab0d5d65dd2daffa57a8` |
| `/usr/bin/virmilld` | `3e86f876ddd2047bbc5cf3d52921b86f898bb7837ee0409bd900c9755d2f309f` |
| Core RPM | `03969431ff1688028fc9e181d063264ad1bff21528b325b7535aa4c2635af254` |

The separate helper package was built but not installed in this update. Helper
policy, host networking and supplied guest media were not changed. `.gitignore`
was reviewed; no VM media, build artifacts or private host configuration is tracked.

## Remaining qualification

Receipt projection/import/export and recovery navigation have service, filesystem,
CLI/TUI and real SQLite tests, using a synthetic restic adapter. Existing earlier
native encrypted backup recovery evidence remains separate; this slice did not
repeat a real repository recovery through the new chooser. A Linux guest with
verified SSH bootstrap is still needed for actual package installation and agent
ping verification; see [the preparation plan](guest-tools-native-plan.md).

No acceptance scenario was promoted. The complete 71-scenario matrix remains
2 accepted, 57 in progress and 12 not implemented. This installed owner beta is
not a complete 1.0 release qualification or publication authorization.
