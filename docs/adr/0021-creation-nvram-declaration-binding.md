# ADR 0021 — Durable creation NVRAM declaration binding

Status: implemented with local regression verification; native qualification in progress. No v1 scope change.

The creation matcher historically allowed any absolute NVRAM destination whenever
the reviewed XML delegated destination assignment to libvirt. Lexical validation
now rejects malformed paths, but repeated observation could still accept path B
after path A. The native probe confirms that declaration, materialization and
guest writes are distinct stages. Apply the precedence in ADR 0001 and the
durable observation rules in specification documents 07 and 10.

New UEFI creation recipes will persist `nvramDeclarationVersion: 1`. Existing
recipes without this field keep their original semantics and remain visibly
unbound. BIOS recipes do not acquire auxiliary-state authority. Unsupported
versions fail closed; old receipts are not silently reinterpreted or upgraded.
Explicit operation reconciliation and creation-result reads verify the persisted
plan and recipe digests before invoking observation or claiming completion. This
prevents a changed version field from downgrading the proof requirement. Plan
expiry does not prohibit observing an already accepted uncertain operation.
Creation results reached through a definition-recovery child validate both the
child's immutable recipe and the referenced original creation recipe. The
recovery review exposes the original declaration policy and initialization limit.

For version 1, after exact definition observation and before marking definition
complete, obtain the existing typed cold-state inspection from the backend.
Require the same native resource and fingerprint, stopped state, no managed save
and no autostart. Require the exact reviewed loader/template/formats and a
canonical nonempty assigned NVRAM destination. This is configuration observation;
it opens no auxiliary files and initializes nothing.

Persist one versioned record under the original creation plan with compare-and-put.
Bind plan/input/job/native resource identity, creation binding, firmware-input
digest, declared firmware/NVRAM mapping and the first recorded observation
fingerprint. On subsequent reconciliation, compare the same mapping and creation
identity. Preserve the original observation fingerprint, which is historical
evidence rather than a claim that the whole VM configuration can never change.
Never replace an existing path or conflicting record on retry. Journal failure,
cancellation, changed native observation or missing inspection capability leaves
creation uncertain and retains resources. Reconciliation observes without
replaying definition, allocation or initialization.
Persisted cancellation requests are checked during active observation/publication,
independently of context cancellation. A later explicitly requested reconciliation
may verify the existing effect while preserving that cancellation history.

The record means **first durably recorded declaration**, not the historically
first assigned path or the first creator of a file. If acknowledgement was lost
before any durable record, the earlier path cannot be reconstructed from a later
observation. The shared creation result and plan review expose the binding stage
through CLI and TUI; file initialization/freshness and complete capture remain
explicitly unverified. No bytes, file generation or deletion authority are implied.

Versioned recipe/proof checks and journal reopen/lost-acknowledgement regressions
provide migration evidence without changing the SQLite schema. Native declaration
binding still needs its own disposable-host test. The larger typed initialization
and confidential capture boundaries in ADR 0020 remain required before stronger
fresh-state or recovery claims can be made.
