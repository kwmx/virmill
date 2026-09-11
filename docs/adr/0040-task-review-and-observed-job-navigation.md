# ADR 0040: Complete TUI tasks with readable review and observed progress

Status: implemented; native workflow evidence tracked separately.

The owner requires a usable beta with simple ordinary tasks and accessible expert
controls. This refines presentation and task integration without changing the
mandatory 1.0 scope or granting release publication authority.

Plans now lead with the action, relevant requested values, exact targets, storage
needs and all warnings. Complete technical identities and review remain available
below. Confirmation explains stable acknowledgement IDs; the submitted IDs and
plan digest are unchanged. Review Back restores guided and expert form inputs.
An uncertain apply response preserves its exact review and idempotency key;
a deliberate retry cannot acquire a new operation identity simply because a
reply was lost. No automatic retry is introduced.

Jobs and an open job view refresh every three seconds through read-only shared
service calls, with at most one pending request of each kind. Stale replies cannot
replace a different resource page. Simple status text distinguishes completed,
partial, canceled and uncertain outcomes; underlying details remain available.
No synthetic progress percentages, automatic reconciliation or inferred guest
readiness are introduced. Existing preparation-to-creation handoff remains intact.

Create VM and Import share one source browser. Required firmware is selectable
with basic VM settings; full hardware controls remain in Advanced. Navigation
validates the current step, preserves later unfinished choices, and keeps focused
controls and help visible in supported terminal sizes.

A dedicated boot/installer form reads vm.boot.get before offering changes. It
uses observed disk targets and NIC identities, requires a stopped persistent VM,
and emits the existing shared vm.plan set operation for next-boot changes. It
cannot delete installer files, modify saved-state guests, introduce unknown
selectors or replace unknown XML. CLI vm boot set is an explicit alias of that
same shared operation. Native readback, guest boot and media installation remain
separate verification levels.

The beta audit still identifies missing complete console/installer access and
ordinary protection forms. This ADR does not rename those gaps as completed
features or waive any of the 71 acceptance scenarios.
