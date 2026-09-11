# TUI task completion evidence — 11 September 2026

Installed source revision: `47d1a8bee825b58356ae5b9e20d05706476b1283`.
This is a tested owner beta update, not a declaration that the public beta or
complete 1.0 is release-qualified. All 71 acceptance requirements remain intact.

## Delivered and verified

The native `beta-ux-workflow-native-001` fixture used the installed 80×24 TUI for
all mutation approvals and Apply actions:

1. Choose a generated two-disk OVA through the unified browser.
2. Read source defaults; change the name, CPU to 3 and RAM to 1024 MiB.
3. Choose a private destination and prepare both 1 MiB disks.
4. Continue automatically into VM settings; choose the existing test pool and BIOS.
5. Visit Advanced hardware, then disk and network settings.
6. Create a private folder and export the chosen settings through the browser.
7. Preview creation, return to retained settings, preview and apply.
8. Observe automatic completion without pressing Refresh.
9. Select only the new VM and open Boot / installer; reorder the two observed disks.
10. Preview, return to retained boot settings, apply and observe completion.

Native readback proved 3 CPUs, 1024 MiB, two disks and boot order changed from
`sda,sdb` to `sdb,sda`. Every other persistent XML element remained unchanged.
The new guest (`fe456a77-a92a-4f06-b986-586e981ae029`) remained powered off.
All preexisting guests, jobs and supplied media metadata were preserved. This
fixture does not prove guest boot, graphical installer access or guest readiness.

Software race tests passed for the complete TUI, CLI and shared application
packages. Focused coverage includes plan fact preservation, readable estimates,
firmware selection, step validation, 80×24/120×36 form rendering, boot-selector
refusals, asynchronous reply isolation, no overlapping job polling, review Back
restoration, and exact idempotency-key reuse after a simulated lost Apply reply.
That lost-response test is a client request test, not a native crash drill.
Scoped vet and diff checks passed.

Three package integration tests passed: RPM/DEB metadata, staged install/uninstall
preservation, and real private daemon/CLI/TUI IPC. Guarded RPM replacement and idle
ordinary-user coordinator restart passed with the existing journal and guests
unchanged. The new coordinator PID was observed as 74567.

## Installed artifacts

| Artifact | SHA-256 |
| --- | --- |
| Core RPM | `6646ffbc1bb4531b289fdfaded7ec4a04d85dbb3635da89d7b9af2c797751b29` |
| `/usr/bin/virmill` | `70dab3c5b580b83dbd4d71c1b2de85f63f99d390d20c8c3020dade8c0d82d488` |
| `/usr/bin/virmilld` | `77d1b73f1f13e253805d5d13ca32c83fcb9d0b5b60f203185ac6e5b0ea214aac` |

Pinned Go/vendor dependencies remain unchanged. Build/package manifests are at
`build/beta-ux-packages.json`, with RPM/DEB/source artifacts under `dist/`.
Native fixture artifacts remain under the authorized VM's
`~/virmill-tests/beta-ux-47d1a8b/walkthrough`. No publication occurred.

## Responsibilities and remaining work

- Review agent: concise plan summary, warnings and full technical fact retention.
- Creation agent: basic firmware, validation and help; updated operator guide and
  complete native TUI fixture.
- Audit agent: baseline beta workflow audit and observed-device boot form.
- Parent: shared CLI alias, workspace integration, form restoration, idempotency,
  observed jobs, readable confirmation, native operations and release evidence.

The audit's competing import entry points, lost review inputs and inaccessible
boot editing are corrected by this update. Console/viewer access for a complete
installer session and guided protection workflows still require implementation
and native tests before publication readiness. Other missing mandatory v1 work
remains in the complete tracker; no requirement status was promoted.
