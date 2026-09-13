# Virmill implementation

Read `virmill-v1-spec/AGENTS.md` and the normative specification before work.
The supplied package stays at its contracted archive root and is preserved for audit.
Implementation decisions and corrections live in `docs/adr/`.

All 71 acceptance scenarios remain required for 1.0. A partial implementation is
development source, never a smaller release. Record evidence at its actual level.
The owner has authorized a remote disposable VM for installing Virmill and running
guest/image tests. Its destination and observed setup are in the ignored
`.virmill-local/test-host.json`; this is session authorization, not permission to
use any discovered host. Preserve supplied source media and unrelated existing
guests. The local development host remains unauthorized for host mutations.
Local generated fixture files under this repository or temporary directories may
be used. Missing access or hardware evidence does not authorize another target.
Never commit test-host names, addresses, home paths or owner media names; the
evidence recorder and `scripts/check-private.py` enforce this (see
`docs/repository-hygiene.md`).

Use `./scripts/go` for the pinned repository-local toolchain. Keep dependencies
exact in go.mod/go.sum and contracts/dependencies.lock.json. Do not download at runtime.
Use official Go libvirt bindings, Cobra, Bubble Tea, SQLite and the shared service.
No shell command strings from user input. No success-returning placeholders.
Update requirements.json, the evidence ledger and generated traceability after work.
