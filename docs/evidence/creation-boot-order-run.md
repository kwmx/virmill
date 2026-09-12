# Guided boot order during VM creation

Scope: IMP-01/02/04 and UX-01/02 partial workflow evidence. No acceptance status
is promoted; all 71 scenarios remain mandatory.

`creation-boot-order-ui-001` passed the full TUI, CLI and shared creation packages
with race checking (7.472s, 3.549s and 15.386s). Five new regression families use
real form key updates to exercise attach-only compaction, disk-first moves,
multiple media disable/re-enable/reordering, invalid saved orders retained until
editing, and the advertised SATA suggestion for actual prepared ISO sources.
Unrelated source/controller/CPU/RAM/firmware/NIC choices and complete request
validation remain checked. These are synthetic service/form tests, not guest boot
or controller-driver qualification.

`creation-boot-order-fixture-001` passed three offline native-fixture decoder and
preservation checks. The live fixture uses only the existing generated ISO
preparation operation, a private frontend draft and actual 80×24 TUI edits. It
does not preview/apply, create/start a VM, change networks/helper configuration,
or edit supplied media. Native results are appended after execution.

Agents supplied the boot-control implementation, independent key-driven
regressions and read-only native fixture. Root owns integration, specification
mapping, docs, packaging, all remote actions and release tracking. No dependency,
API, recipe or draft schema changed. The helper authorization blocker belongs to
the separate network-creation completion test and is not bypassed here.

`creation-boot-order-packages-001` passed all three package/private-IPC checks.
`creation-boot-order-upgrade-native-001` installed the exact core RPM and
`creation-boot-order-restart-native-001` activated its coordinator. Both preserved
their existing guest/job observations; restart retained all 32 jobs, journal tables
and source-media metadata. No helper configuration or activation was performed.

Installed source: `7efd7c14cf830abcde4ce04fe446f4012f5cf61a`.
CLI SHA-256: `06ff804d65eb52ebd88cf7d66972c7568a2410942fe9f91bc2354547653bf33f`.
Coordinator SHA-256: `0d50f5ba9f116264aedc3f4ca3662213a3c332a2b29f7d729758365dded50454`.
Core RPM SHA-256: `3dd9c80e5b2187709f6f9465dd437bcd2e224a88dd65b42e70495e04284ac6ae`.
Unsigned RPM/DEB packages and manifests are retained in `build/boot-order-delivery/`.
The native stage is `~/virmill-tests/boot-order-7efd7c1`.

`creation-boot-order-native-001` passed against the installed runtime using
prepared generated ISO operation `5a464dae-5ff1-4080-a4ed-1b239b697aaa`.
The actual 80×24 wizard showed the initial SATA suggestions without manual bus
edits, installer-first order, Attach only with disk compaction to position 1,
media re-enabling, and moving the disk first with media shifted to position 2.
Continue succeeded after each ordinary change. Normal exit flushed the private
draft; complete source/controller/hardware/NIC settings matched except for the
explicit boot positions. Existing VM/job/network observations and source-media
metadata were unchanged. No Preview, Apply, VM creation or boot was attempted.

Root inspected the captured Attach only and disk-first screens. Report and screen
captures are retained under the native stage's `boot-order-tui/` directory and
`build/boot-order-delivery/`. This is live TUI/editing evidence, not guest boot
qualification. The separate network helper authorization remains pending; no
blocked action was retried or bypassed. No publication occurred.
