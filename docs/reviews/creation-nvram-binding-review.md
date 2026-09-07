# Creation NVRAM first-path binding review

Review date: 2026-09-07. Scope: read-only source and existing local synthetic
tests, contributing prerequisites for SNAP-01, IMP-07 and JOB-02. This is a
recommendation for the architecture owner, not an ADR, implementation or native
qualification. HEAD was `44a46b6222a601661eb993ba905b63d2396a2154`; reviewed working
files also included the pending firmware-normalization change. No guest,
privileged filesystem, firmware or TPM operation was performed.

The creation observer does **not bind the first assigned NVRAM path**. It accepts
another absolute-looking path on every reconciliation, and neither its receipt
nor ownership record contains an NVRAM file identity. Recording the first
accepted native declaration is the smallest useful correction, but it cannot
prove that the file is fresh, owned by this operation or safe to open. Those
claims need a bounded native filesystem adapter and a durable initialization
boundary before first start. Tightening the XML string comparison alone cannot
supply that proof.

## Source entrypoints and current evidence

Line references identify the reviewed working tree; symbols remain the stable
entrypoints if neighboring work moves lines.

| Entrypoint | Current behavior relevant to the first path |
|---|---|
| [CreationFirmware / CreationTarget](../../internal/domain/creation.go:12) | Firmware intent names code, template, format and policy. There is no target NVRAM path, root identity, initialization receipt or instance generation. `CreationTarget.FirmwareDigest` covers selected firmware inputs, not a per-VM NVRAM file. |
| [preflightCreation](../../internal/backend/libvirt/creation_linux.go:328) and [firmwareFileDigest](../../internal/backend/libvirt/creation_linux.go:183) | Recheck the target environment, hash code/template and bind the matching descriptor. System-file reads reject symlink components and require bounded root-owned ordinary firmware files without group/other writes. This checks source firmware inputs; it never observes the newly assigned NVRAM destination. |
| [creationXML](../../internal/backend/libvirt/creation_xml.go:37) | Renders `<nvram template="…" templateFormat="…" format="…"/>` without destination text. Assignment is left to libvirt. |
| [DefineCreatedVM](../../internal/backend/libvirt/creation_linux.go:797) | Rechecks absent UUID/name, networks and allocated disk/media identities, then calls `DomainDefineXMLFlags(..., DOMAIN_DEFINE_VALIDATE)`. The returned VM observation is not a destination-file receipt. |
| [matchesNodeAt](../../internal/backend/libvirt/creation_xml.go:265) and [matchesCreationPolicy](../../internal/backend/libvirt/creation_xml.go:398) | Empty wanted NVRAM text accepts any trimmed observed text beginning with `/`. This is a lexical wildcard, not canonical-path, root, no-symlink or filesystem verification. The pending firmware normalization deliberately preserves this rule. |
| [ObserveCreatedVM](../../internal/backend/libvirt/creation_linux.go:831) | Rebuilds the same empty-path XML, compares persistent XML, and checks networks and disk/media volumes. It does not call a file-identity observer for NVRAM, compare a prior assigned path, or require a stopped/no-managed-save/no-autostart lifecycle for this match. |
| [Receipt](../../internal/creating/service_linux.go:57), [Execute](../../internal/creating/service_linux.go:649), [Reconcile](../../internal/creating/service_linux.go:726) | Persist verified volume progress and define intent. Both definition results are discarded by the coordinator; successful observation sets only `Defined`. Recovery repeats observation against the original empty-path recipe and can set `Defined` after a lost acknowledgement. |
| [resumeHandler.Execute / Reconcile](../../internal/creating/recovery_linux.go:209) | Reverify retained disks, persist recovery intent, define once, then delegate reconciliation to the original creation service. There is no separate NVRAM initialization or binding recovery step. |
| [recordOwnership / Ownership](../../internal/creating/ownership_linux.go:23) | Store VM/job identity, metadata binding and created disk/media volumes. Catalog `managed` means the native UUID/creation metadata agree with a durable record; it does not prove NVRAM ownership or freshness. |
| [vmHandler.Validate](../../internal/app/service.go:411) | Generic start validates current VM fingerprint and stopped state. It does not consume an NVRAM initialization proof. A fresh XML fingerprint cannot detect same-path file replacement. |

[Cold firmware inspection](../../internal/backend/libvirt/cold_state_xml.go:305)
already provides a bounded declaration parser with stricter path syntax. It does
not inspect filesystem objects. [Creation acceptance](../../internal/backend/libvirt/creation_acceptance_linux.go:24)
explicitly refuses UEFI/TPM auxiliary-state acceptance, and
[cleanup](../../internal/backend/libvirt/creation_cleanup_linux.go:533)
refuses unaccounted auxiliary storage. Neither should be weakened to bypass this
gap.

## What is and is not established

The parent supplied this observation: the probe is stopped; XML assigns
`/var/lib/libvirt/qemu/nvram/Virmill UEFI TPM cold probe_VARS.fd`; a pre-first-boot
`sudo test -e` returned false. This review did not repeat it. That predicate is
not a no-follow `lstat` or descriptor-bound observation: a dangling symlink can
also return false, and inaccessible/unresolved components are not a proof of
absence. Root policy, component identities, genuine final-component ENOENT,
ownership, link count and freshness remain unqualified. The reported directory
is evidence for this probe only, not a general default or approved root.

Libvirt documents NVRAM as a per-domain file and describes copying the template
at domain startup. Thus an assigned XML path and an unmaterialized file before
first boot are distinct stages; definition success need not imply an existing
NVRAM inode. This does not establish how the installed backend handles an
existing file, aliases or races. [Official guest-firmware documentation](https://libvirt.org/formatdomain.html#guest-firmware).

Three different identities must stay separate:

* The code/template source identity checked during creation preflight.
* The destination declaration: connection, VM UUID, exact persistent NVRAM
  path and formats, linked to the creation plan/job and native XML observation.
* The materialized instance: held inode generation, metadata and provenance of
  its fresh creation or explicitly reviewed reuse. UID/GID, permissions and
  security-label policy are separate from a generation string.

The current slash-prefix rule also allows `/` or paths containing `..` by static
inspection; these examples were not separately executed. Even replacing it with
canonical-path validation would still accept a different safe-looking file on
every observation. A hash equal to the template does not prove fresh ownership:
an older unrelated file can contain identical bytes. Birth time after a wall-clock
timestamp does not prove this operation created it either.

## Smallest dependency-ready correction and options

First add an explicitly versioned, durable **NVRAM declaration binding**, separate
from generic matcher success. It should bind the original plan/job/native UUID,
connection, reviewed firmware/template selection, exact canonical assigned path,
formats and relevant observed XML identity. Save the first accepted observation
with compare-and-put semantics; later observation must compare that exact path
and binding. Never let a new path replace an existing binding on retry. Unknown
or older proof versions must not become a fresh-state certificate, and v1
receipts must not silently acquire stronger meanings.

This record needs an explicit distinction between a declaration with unresolved
file status, independently observed absence under an approved root, and a
materialized file with verified provenance. Absence has no inode generation;
do not fabricate a zero identity or route it through `coldfiles.Open`, which
requires an existing complete identity. Missing privileges/capability stay
visible. A declaration alone can describe definition progress, but cannot clear
a fresh-NVRAM readiness gate or authorize capture, deletion or restore.

The smallest complete extension for the currently assigned-path workflow is a
typed, authorized **initialize-and-bind** boundary before first start. A host
adapter must independently resolve the root policy and native mapping, pin root
and parent directories, classify the final component without following it or
opening it for bytes, and recheck stopped native state. It must distinguish
ENOENT from aliases, special files and all other failures. Where the operation
creates state, persist intent before exclusive no-replace publication from the
held reviewed template; record the resulting instance identity and durable
completion, with required native permissions/labels. Recheck the native mapping
and held file before declaring initialization verified. An existing unexpected
file is retained and requires explicit review, even if its contents match.

This is a proposal within the bounded managed-storage/auxiliary-state helper
families in [document 10](../../virmill-v1-spec/docs/10-jobs-security-and-recovery.md:11),
not authorization for generic root file access or an assumption that such a
method already exists. [ADR 0020](../../docs/adr/0020-cold-recovery-boundary.md:12)
requires independent native mapping, approved roots, fresh observations and
intent/observation at each effect. NVRAM bytes must not enter JSON or journals.

| Option | Consequence |
|---|---|
| Bind the assigned declaration now; retain unresolved file status | Smallest independent implementation slice. Stops later XML path drift once persisted. Does not by itself prove first-assignment provenance, file safety or freshness; stronger completion/start claims remain gated. |
| Initialize that assigned path through the typed boundary above | Keeps native path assignment while adding a reviewed post-define, pre-start effect. Requires durable handling of definition-without-state and publication-before-acknowledgement. This is the minimum complete route for the observed pending file. |
| Allocate a dedicated managed NVRAM artifact before definition and render its exact path | Larger contract/renderer change, but destination intent and exclusive-creation receipt exist before define. Requires independently registered root policy, template input binding and native qualification; do not derive a pathname from a guessed libvirt convention. |

If native libvirt initialization is retained instead of managed publication,
its installed no-reuse/no-overwrite behavior and recoverable provenance must be
independently qualified. A pre-start absence check followed by a pathname-based
start is not an atomic claim of exclusive creation. Existing QEMU guards and
leases address cooperative writers only; root control and external-writer limits
must remain explicit. No first-boot or auxiliary-byte operation should be hidden
inside read-only reconciliation.

## Failure and recovery boundaries

| Boundary | Required treatment |
|---|---|
| Define may have succeeded, acknowledgement or path-binding save was lost | Query the intended UUID and compare exact configuration. Persist a declaration observation only to the level recoverable from evidence. Without a durable earlier binding, do not claim to know the historically first assigned path or first file creator. Never replay define merely because the binding is absent. |
| Path assigned, genuine absence observed, but initialization never ran | Keep a pending declaration/initialization intent. No file generation exists. Recheck root, native state and absence before a separately authorized creation step. |
| Existing file, dangling symlink, hard link, special file or inaccessible root | Refuse automatic fresh-state adoption or replacement. Retain the VM, disks and candidate state; report the exact unsupported/uncertain category without exposing bytes. |
| File publication succeeds before its acknowledgement is durable | Recover against durable intent and independently verifiable artifact identity. If provenance is insufficient, retain it as uncertain; do not overwrite, recreate, reset or delete to obtain a clean result. |
| XML path, root/parent binding or file generation changes | Fail stale/recovery-required. Do not rebind to the replacement. Metadata equality or equal bytes alone cannot excuse a replaced inode. |
| Guest starts or another lifecycle change occurs during checks | Invalidate the stopped-file proof and retain resources. A later stopped observation cannot prove the VM never ran. |
| First boot legitimately changes NVRAM contents | Do not compare the original template hash forever or reset state to restore equality. Record initialization provenance separately from current capture metadata; a later cold capture needs its own fresh plan and pre/post-copy identity checks. |
| Cancellation, journal failure, unsupported identity fields or denied access | Preserve partial resources and locks; return no verified initialization/success proof. Definition, start, guest boot and recovery identity remain separate stages under IMP-07. |

## Required regressions and evidence limits

1. Bind path A, then reconcile path B with otherwise identical XML: refuse B;
   repeated A is idempotent. Include lost acknowledgement before and after binding,
   conflicting compare-and-put, journal reopen and version downgrade tests.
2. Validate empty, relative, root-only, noncanonical, control-bearing, duplicate,
   foreign and structured NVRAM declarations without broadening accepted formats.
   A valid but unverified path must never set a file-freshness flag.
3. In temporary generated directories, distinguish real absence from dangling
   and component symlinks, hard links, FIFO/socket/device substitutions, inaccessible
   parents, root replacement and mount boundaries. Device/mount/permission cases
   require an actually available safe capability; report blocked cases honestly.
4. Exercise initial ordinary-file observation, same-path generation replacement,
   changed metadata, unsupported statx fields, owner/permission-policy mismatch,
   exact template input replacement and no FD leaks. Classify type through O_PATH
   before a readable open. Existing `fileidentity` is reusable for generation;
   it does not itself enforce an approved root or UID/GID/label policy.
5. Inject failure/cancellation at intent, exclusive create/publication, flush,
   identity recording and final native recheck. Assert no overwrite, blind retry,
   automatic reset/deletion, success proof or release of unresolved resources.
6. On the authorized disposable native backend only, qualify when the assigned
   file appears, its root/ownership/label semantics, existing-path refusal,
   first-start races and crash recovery. Then test identity preservation through
   stopped copy/restore. Neither an XML fixture nor first-boot marker alone
   satisfies SNAP-01.

Existing checks actually executed, with the pinned toolchain and no downloads:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -race -tags libvirt_dlopen ./internal/backend/libvirt ./internal/creating -run '^(TestCreationFirmwareNormalizationPreservesExistingNVRAMPathRule|TestLostDefinitionAcknowledgementReconcilesWithoutReplay|TestManagedOwnershipRequiresBothJournalAndNativeIdentity)$' -count=1 -v
```

All three passed without skips. The first explicitly asserts acceptance of
`/fixture/unverified-other-state.fd`; its pass documents the gap, not safety.
The latter two use a deterministic backend that compares target/binding and does
not model NVRAM files. No new runtime tests were written in this review.

Reviewed-file SHA-256 identifiers:

```text
7632dcf372881d77a2daac8684220ef3eb47c0c57cb326c65f6d96e2a1268e6f  internal/backend/libvirt/creation_xml.go
3392a3d3b1c1c7f75e651ea029ab4e3eea31216aa0ba40f1436f07b017a6b6da  internal/backend/libvirt/creation_linux.go
608452b26b35f911654ad9550dd196316172e63cff6ee7f0d40e0135b8081933  internal/creating/service_linux.go
5bd622f218b10af57e592094d30eb31b88a99bf644a60dbae12f0b497939a7a1  internal/creating/recovery_linux.go
705a50646f11feb4be77d1d9c0ae7e8b138d50fe62501ba9d7df9ab151c850b4  internal/creating/ownership_linux.go
2480d876bb2c1c85440e3d76382202732fa6f5920a679affcd5a53df3d83e262  internal/domain/creation.go
e3c008ed67a885f786bdd358e2f203ac92d66b928903732f4b8f03668134eccb  docs/adr/0020-cold-recovery-boundary.md
```
