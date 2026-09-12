# Guest integration and receipt recovery beta evidence

Installed source: `96532cb921e10ab496da8857df5e94a35d261319` (12 September 2026).
Implementation digest: `ad87b7d669a0ae58c710c5a278a68a6f8245b96c9aa0678d3f7fdf900e06bd5c`.
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

## Installed artifacts

| Artifact | SHA-256 |
| --- | --- |
| `/usr/bin/virmill` | `3254c67a8651cd6db3ab0331342c43d0192052cf15da6697bb1a17aef6b53ca2` |
| `/usr/bin/virmilld` | `957feef702132708366a502a6be482bcf11d8a60541d32b35c3a56ce39d36e55` |
| Core RPM | `613b57b710fdb20fa31509849c1ebd92a0158ae9fc9e4f273b3b149171990751` |

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
