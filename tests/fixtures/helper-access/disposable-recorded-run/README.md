# Disposable helper access recorded run

These are the exact Python stdin programs executed against the owner-authorized
disposable VM. The SSH destination is intentionally absent. They retain immutable
run paths and artifact hashes and are not an automatic replay harness. Preserve
existing guests, supplied media and operation journals. The first root test uses
generated temporary files and synthetic VM inventory; it proves no hypervisor
behavior. Its development binary lacked an archived source digest, so later clean
build qualification is required.

The later `*-clean-001` root fixture and actual native CLI/TUI access cycles used
immutable `b7fe053` artifacts. The failed first TUI revoke attempt is deliberately
retained alongside the same-plan resume. Never rerun these mutation programs as
a batch. Resolve existing plan/job state and explicitly choose new isolated
fixtures for a new qualification run. See the release ledger for each attempt.
