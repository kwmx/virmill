# Virmill implementation

Read `virmill-v1-spec/AGENTS.md` and the normative specification before work.
The supplied package stays at its contracted archive root and is preserved for audit.
Implementation decisions and corrections live in `docs/adr/`.

All 71 acceptance scenarios remain required for 1.0. A partial implementation is
development source, never a smaller release. Record evidence at its actual level.
No disposable host has been authorized. Local generated fixture files under this
repository or temporary directories may be used; never mutate discovered VMs,
host networks, services, block devices, firmware or USB without owner approval.

Use `./scripts/go` for the pinned repository-local toolchain. Keep dependencies
exact in go.mod/go.sum and contracts/dependencies.lock.json. Do not download at runtime.
Use official Go libvirt bindings, Cobra, Bubble Tea, SQLite and the shared service.
No shell command strings from user input. No success-returning placeholders.
Update requirements.json, the evidence ledger and generated traceability after work.

