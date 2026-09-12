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
