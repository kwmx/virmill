# Libvirt swtpm lock selection

Date: 2026-09-08. Read-only prerequisite review for SNAP-01, SNAP-02 and SEC-01;
all remain unaccepted. This refines the selection question left by the
[writer-exclusion review](swtpm-cold-writer-exclusion.md). It changes no capture,
helper, lifecycle or authorization contract.

**Fresh inactive domain XML plus fresh domain capabilities cannot prove that
libvirt enabled swtpm storage locking.** In upstream **libvirt v12.0.0**, the
`,lock` suffix depends on a separate internal swtpm feature bitmap. The public
TPM domain-capability output does not expose that bit, and the persistent TPM
declaration has no corresponding field. Even evidence of the suffix establishes
a requested launch policy, not that a lock is presently held or that capture
writers have been excluded.

## Exact suffix predicate

`qemuTPMVirCommandSwtpmAddTPMState` starts with suffix `,lock`. It clears it when
`virTPMSwtpmCapsGet(VIR_TPM_SWTPM_FEATURE_TPMSTATE_OPT_LOCK)` is false. Shared
storage causes a warning in that false branch; it does not change the predicate
or turn omission into a hard failure. The resulting argument is:

| Backend | Feature bit true | Feature bit false |
|---|---|---|
| File | `backend-uri=file://<source>,lock` | `backend-uri=file://<source>` |
| Directory/default | `dir=<source>,mode=0600,lock` | `dir=<source>,mode=0600` |

The normal emulator command builder invokes this helper. TPM version, model,
`persistent_state`, profile, encryption and firmware do not select the suffix.
The shared-storage migration option uses the separate
`VIR_TPM_SWTPM_FEATURE_CMDARG_MIGRATION` bit and can request deferred/released
locking; it must not be conflated with option support.
[v12.0.0 qemu_tpm.c](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_tpm.c#L673):
helper 673–700, predicate 680, normal invocation 863, migration branch 893–918.

Omission means backend defaults apply; it does not mean “locking disabled” for
every backend. The swtpm 0.10.2 manual documents directory locking enabled by
default, file locking disabled by default, and explicit `lock=false`. Those
defaults cannot authenticate an unknown producer or another dependency version.
[Pinned swtpm manual](https://github.com/stefanberger/swtpm/blob/v0.10.2/man/man8/swtpm.pod).

## Where the private bit comes from

The following trace is in
[v12.0.0 virtpm.c](https://github.com/libvirt/libvirt/blob/v12.0.0/src/util/virtpm.c):

| Lines | Mechanism |
|---|---|
| 39–47 | Maps the JSON feature string `tpmstate-opt-lock` to the enum bit. |
| 125–134, 255–268 | Constructs `<selected-swtpm> socket --print-capabilities`; it does not parse a version string. |
| 278–326 | Finds `swtpm`, `swtpm_setup`, and `swtpm_ioctl` in daemon PATH. Rechecks cached paths/capabilities on stat failure or changed `st_mtime`, without content hashing. |
| 330–355 | Under a mutex, lazily populates a process-global bitmap and returns its bit. Initialization/missing-bitmap failure returns false. |
| 195–253 | Parses features; ignores unknown names. Nonzero command exit returns an empty bitmap. Malformed JSON reports an error but returns the potentially accumulated bitmap. |

Thus the exact condition is a bit lookup in this cache. A shell's current PATH,
an installed package version, or an independently run query does not by itself
observe the daemon's cached selection or a previous launch. The enum is internal:
[virtpm.h](https://github.com/libvirt/libvirt/blob/v12.0.0/src/util/virtpm.h#L31),
lines 31–40 and 57–58, declares it separately from domain-capability types.

## What fresh domain capabilities actually say

`virQEMUCapsFillDomainDeviceTPMCaps` fills frontend models from QEMU device
capabilities. It advertises emulator/external backends when the swtpm tools are
available and QEMU supports the emulator device. Its version enumeration comes
from **swtpm_setup** features `tpm-1.2` and `tpm-2.0`. That code does not request
`VIR_TPM_SWTPM_FEATURE_TPMSTATE_OPT_LOCK`.
[v12.0.0 qemu_capabilities.c](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_capabilities.c#L6846),
lines 6846–6884.

The public TPM result has only `supported`, `model`, `backendModel`, and
`backendVersion`; its formatter emits those three enumerations. There is no lock
policy, selected swtpm path/hash, launch identity, feature-query receipt, or held
lock owner in this result.
[TPM capability structure](https://github.com/libvirt/libvirt/blob/v12.0.0/src/conf/domain_capabilities.h#L125),
lines 125–131;
[TPM capability formatter](https://github.com/libvirt/libvirt/blob/v12.0.0/src/conf/domain_capabilities.c#L642),
lines 642–652.

A concrete static counterexample follows: two swtpm feature responses can differ
only in `tpmstate-opt-lock`, while QEMU device capabilities, tool availability,
and swtpm_setup version features are identical. They produce the same public TPM
domain capabilities but different suffix decisions. This is an inference from
the separate code paths, not an executed native test. Repeating the public query
does not add the missing observation.

## What inactive XML actually says

`virDomainTPMEmulatorDef` stores version, transport, source type/path, log/debug,
secret reference, persistence, PCR banks and profile. It has no lock-selection
member.
[v12.0.0 domain_conf.h](https://github.com/libvirt/libvirt/blob/v12.0.0/src/conf/domain_conf.h#L1516),
lines 1516–1532.

`virDomainTPMDefFormat` emits those supported declaration fields. It emits the
storage source only when its type differs from `DEFAULT`; there is no serialization
of the private swtpm capability bit or selected launch suffix. A fresh explicit
directory source improves location evidence but does not establish its writer
policy. `persistent_state='yes'` records persistence behavior, not exclusion.
[v12.0.0 domain_conf.c](https://github.com/libvirt/libvirt/blob/v12.0.0/src/conf/domain_conf.c#L26008),
lines 26008–26100.

This matches the installed `libvirt-libs-12.0.0-3.fc44.x86_64` domain schema read
locally: the emulator backend permits persistence, debug, encryption, PCR banks,
source and profile, without a locking attribute. The domain-capabilities schema
is generic for enum names, so the formatter—not that generic grammar—is the
evidence for which TPM enumerations libvirt actually emits. No XML or capabilities
request was made against a running daemon in this review.

## Evidence the capture decision still needs

| Available observation | Supported conclusion | Remaining limit |
|---|---|---|
| Inactive XML and public TPM capabilities | Declared emulator/source and advertised device/version support | Does not prove the suffix or lock ownership |
| Exact dependency identity plus a strict feature query from its verified executable | That executable advertises the option at that observation | Does not reproduce the daemon's cached bit, effective environment or historical command |
| Independently bound native launch record/arguments | The identified run requested `,lock`, if present | Historical intent; producer lifetime and lock object must still be checked |
| Existing lock object or a PID-shaped diagnostic | A candidate object/owner to investigate | Neither existence nor absence proves exclusion or VM association |
| Held interoperating guard on the independently resolved inode | Cooperative exclusion against the supported writer policy | Does not prevent path replacement, noncooperating writers or native restart/setup effects |

These are proposed evidence distinctions, not a new helper API. The parent-owned
capture boundary must choose how to authenticate the executable/run and prove
effective cooperation while retaining stopped-state, restart prevention, approved
root resolution, guard lifetime and pre/post membership/generation checks. Do not
infer the private bit from a successful VM boot, firmware TPM protocol response,
profile name or declaration fingerprint.

Refuse to claim supported exclusion for external/pass-through TPM, unknown
producer versions/options, failed or malformed provenance, unresolved implicit
paths, ambiguous daemon/executable selection, changed dependencies, unqualified
block/shared storage, or a lost/replaced guard. A parsed version or declaration
cannot silently replace those checks. The earlier experiment remains dependency
evidence only; this review does not repeat it or promote its result to a complete
cold capture. SNAP-02's live graph-transition/failure requirements are especially
not established by a swtpm option-selection trace.

## Retrieval and verification record

Official web retrieval returned some cache misses. A bounded read-only fetch of
the following tagged primary text files was then permitted after a network-only
sandbox exception. Each response was limited to 1 MiB, or 3 MB for the later
formatter/source batch, with a 15-second request timeout. Downloaded text was
decoded and selected source lines printed; no downloaded code was executed or
installed. The source line references above count the actual fetched bytes;
browser-rendered excerpts can collapse blank lines.

| File under libvirt v12.0.0 | SHA256 |
|---|---|
| `src/qemu/qemu_tpm.c` | `592da09853e5acb221aecdb6cefa055b9f024417a25de1fc5aa85906973e5bbc` |
| `src/util/virtpm.c` | `72e19b07aaf4cb1495b5f60a754054c4d1e1a1506cff16e1345ee040470053f9` |
| `src/util/virtpm.h` | `ff0bc4391af768c1c9cfca5410dfae9ec294ae5b10eb863ee6de7d316fca68b7` |
| `src/qemu/qemu_capabilities.c` | `842cfec6421b0a12602a6a377528ce3234aed7f88aa2f271cf0c7115e4f79ea2` |
| `src/conf/domain_capabilities.c` | `5e56cb8df761ff0fcf0e7f84b9ccff815497f7d43505496c41dd9f9622cf0544` |
| `src/conf/domain_capabilities.h` | `2926e1391d0ffe4e28ae63949866ef0a3ee1aa14ffc330573f8240fcb8af07df` |
| `src/conf/domain_conf.h` | `0f1793d767b02dfaf08b85706abdad5f2b6e1b038bc5aac1ad804911b25ce537` |
| `src/conf/domain_conf.c` | `ecc031d97305cee05d2cd259083565273911fea6d42f0c22738b45fd863d5742` |

Read eight pinned source files and the installed schema/acceptance text. Ran no
runtime probe, guest operation, libvirt API, host-service action, SSH or TPM byte
read. This document is the sole edited file. Documentation whitespace/link-target
checks passed; there are no new runtime test passes, skips or acceptance results.
