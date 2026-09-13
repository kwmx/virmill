# Native auxiliary metadata inspection recipe 001

This is an authored, single-use recipe for the parent operator on the already
authorized disposable host. It has **not been executed natively by its author**.
It supplies SEC-01, SEC-03, UX-01, UX-03 and SNAP-01 prerequisites; no acceptance
scenario is promoted. It exercises the installed CLI, private coordinator,
authenticated host helper, native stopped-domain observer and metadata inventory.
Actual TUI observation remains separate.

The program owns only its new output directory, new never-started domain and new
generated state tree. It conditionally replaces the public helper policy for the
test and restores its original content/access metadata. It never starts a guest,
calls a capture/apply/reconcile operation, installs a package, starts/stops a
service, changes host confinement, or reads existing TPM/NVRAM or private-key
payloads. The public helper-identity command supplies the actor's public key and
fingerprint; the installed coordinator performs its normal key handling.

## Deployment and execution preconditions

The parent must separately review/upload the actual `.py` file, prepare the
helper's existing base actor/key policy, and install/start the frozen software.
The recipe refuses the previously installed `dc2ab1a` runtime. Supplied pins are:

| Item | Expected value |
|---|---|
| Revision | `490b88cba5bf6e0837e2cb5780e56211c22a7d27` |
| Deployment file SHA256 | `5c9945fcfabe2808ff941ef57144f0f7985edba60fe7358b6e18ae2c71bf739c` |
| CLI SHA256 | `9cc64d8aa14e7f5d5992514910364f316d3d12fab784393630d7c160628ac886` |
| Coordinator SHA256 | `44522f6cbe2dd7eb54134038adfee4bd650ced9e4ae5aafe4e042471158cd7aa` |
| Helper SHA256 | `4d67ec32a0aa891d16af237885fe45ba95ee5377829f86ab4761aba89b42917a` |
| Source inventory SHA256 | `f0585986ff6693a306a54e86d48db9eecbed5cddca83941d461d3e2bb2727576` |

The exact deployment file must be at
`<test-vm-home>/virmill-tests/run-65930c6-20260907/packages/490b88c/deployment.json`.
Do not rewrite its historical installation-status field: the supplied byte hash
is authoritative. Its `sourceDigest` and separate `sourceInventorySHA256` are
recorded independently. All three installed binaries must match the manifest.
The coordinator must be the active ordinary-user
`<test-vm-login>-490b88c.service`, with the approved working directory and matching
running `/proc/PID/exe` hash. The system helper service **and** socket must already
be active, with the helper's running executable matching the same deployment.
There is no activation/start fallback in this recipe.

Run only as `<test-vm-login>`, with ordinary host XDG variables still in the invoking
environment. The recipe obtains private Virmill XDG paths from the approved
`environment.json`; it applies those paths only to `/usr/bin/virmill`. `systemctl
--user`, virsh and sudo retain the host environment. The parent must provide an
exclusive administrator policy-edit window and keep other guest/pool/job writers
idle for this disposable run. The switches below record those prerequisites;
they do not acquire a lease or turn a concurrent system into an atomic snapshot.

After parent review, a native invocation has this form, using the exact uploaded
recipe SHA256 reported at authoring handoff:

```text
python3 /ACTUAL/UPLOADED/auxiliary-inspection-native-001.py \
  --revision 490b88cba5bf6e0837e2cb5780e56211c22a7d27 \
  --deployment-sha256 5c9945fcfabe2808ff941ef57144f0f7985edba60fe7358b6e18ae2c71bf739c \
  --binary-sha256 9cc64d8aa14e7f5d5992514910364f316d3d12fab784393630d7c160628ac886 \
  --recipe-sha256 EXACT_UPLOADED_RECIPE_SHA256 \
  --execute-reviewed --exclusive-policy-window
```

This is an invocation template, not a command executed during authoring. The
program validates `__file__`; do not execute it through stdin. The fixed output
directory is the disposable root's `auxiliary-inspection-native-001`, created
exclusively. Any previous directory or fixture resource means stop and review,
not reuse, remove or retry it. Commands have bounded output and 30-second waits;
the main sequence has a 20-minute budget and final observation a five-minute
budget. There are ceilings of 2048 commands, 2 MiB per child response and 32 MiB
aggregate child output. Only the recipe's own command child may be terminated
on a timeout.

## Sequence and preservation

Before mutation, capture every existing stopped persistent domain's exact
inactive XML, requiring autostart disabled and no managed save. Snapshot every
coordinator table row and SQLite schema in a read transaction, with zero locks
and zero triggers required. No journal writes, new plans or jobs are expected.
Inventory the existing helper journal and prior disposable files by metadata;
neither inventory reads those files' payloads.

Directory-pool inventories use the actual bounded C-locale `Name Path` virsh
table, not the unsupported `vol-list --name` option. Unknown table layouts,
duplicate names/paths, unsupported pool types and ambiguous storage paths stop
preflight. Pool configuration and volume XML remain exact. Only numeric pool
allocation/available values may drift together; capacity stays fixed and their
sum must equal it. This accounts for filesystem counters when new fixture files
are created elsewhere on the same filesystem.

All disks/media declared by those existing domains receive regular-file,
generation, size and access metadata checks. The original public EFI media and
already declared tiny copied EFI probe disks additionally receive bounded byte
hashes. Older large image and other prior-file payloads are **not** rehashed.
The preservation claim is explicitly metadata plus those selected hashes, not a
byte-complete revalidation of all existing media.

The program then creates this exact new root, without reusing any prior tree:

```text
/var/lib/virmill-host-helper/auxiliary-fixture-001/   root-owned, mode 0700
  code.fd                                         new dummy code bytes
  nvram.fd                                        4096 original zero bytes
  tpm/                                            observed qemu UID/GID, mode 0700
    .lock                                         empty control file
    empty.state                                   empty ordinary TPM member
    ordinary.state                                32 original generated bytes
```

Generated files have the observed `getent passwd/group qemu` ownership and mode
0600. This account observation selects the fixture's owners; it is not a claim
that a TPM producer has run. No existing auxiliary source path is guessed.

A fresh UUID and fixed new name `virmill-auxiliary-fixture-001` are saved before
defining a KVM `x86_64`/`pc-q35-10.2` fixture with no disks or NICs. Its manual raw
loader/NVRAM and emulator TPM2 directory source explicitly reference only the
new tree. `virsh define --validate` is fixture setup, not a Virmill creation
operation. The guest is never started. The dummy bytes are deliberately **not
valid firmware/TPM state** and establish no initialization, hardware or boot
evidence. If native libvirt cannot preserve this explicit source declaration, the
recipe retains the failure and stops; it never falls back to a default TPM path.

Expected observations are:

1. Original policy denies the new root. A policy with only the new root mapping
   and unchanged existing actor/key permissions still denies auxiliary access.
2. One additional grant for the exact actor/key/new UUID/root and observed state
   UID/GID permits metadata inspection. It has `maxMembers=3`, `maxBytes=8192`,
   and `allowCapture=false`; all older policy entries remain unchanged.
3. JSON and NDJSON return the same native fingerprint/layout and exact three
   payload members, totaling 4128 bytes. `.lock` is separate empty control
   metadata. Each request has a fresh correlation nonce. Capture, independent
   restore and guest-boot flags remain false and no artifact exists.
4. A newly created FIFO under this new TPM directory causes an explicit refusal
   with no inventory and no block. Only that exact FIFO is removed after its
   expected refusal and unchanged generation are checked.
5. Removing the TPM source declaration from only this new stopped definition
   causes unresolved-source refusal. The original new XML is then restored
   exactly and metadata inspection repeated. The original generated regular
   members stay unchanged; creating/removing the FIFO necessarily changes its
   parent directory's timestamps, which are recorded rather than called equal.
6. Restore the original public policy and observe denial again. Recheck the
   runtime, all old journal/guest/pool/file/media evidence, and the new stopped
   definition. Keep the fixture resources for parent review even on success.

A deterministic stale-fingerprint native request cannot be submitted through
the current public inspection command: it always derives a fresh fingerprint.
This recipe does not race a domain editor, read a signing key, manufacture a
signed helper request, or claim that synthetic stale-request coverage is native
evidence. That scenario remains separately unexecuted.

## Public policy replacement and failure retention

The public policy backup is bounded to 16 KiB and saved in the private output
directory with its mode, UID/GID, inode/timestamps and all bounded xattrs,
including any POSIX ACL and SELinux label. Each replacement uses a new exclusive
same-directory temporary file, applies and verifies the original access metadata
and xattrs explicitly, fsyncs it, rechecks the old policy and atomically renames
the new file into place. Same-directory creation alone is never treated as label
preservation. New inode/ctime values are recorded; they cannot be restored by
atomic replacement.

Comparison includes exact content, inode/change identity, mode/UID/GID, size,
mtime and all xattrs. Only atime is excluded because authorized public-policy
reads can update it. State-member comparisons have no corresponding exception.
Linux rename does not provide an expected-generation compare-and-swap: a hostile
or uncooperative administrator could write in the last compare/rename window.
The exclusive policy-edit window is therefore a real execution prerequisite,
not a property proven by this recipe.

On any unexpected failure, preserve the new definition/state/output. There is
no automatic undefine, state removal, native XML restoration, inspection retry,
or service change. The only failure cleanup attempted is original-policy
restoration **when the last installed policy acknowledgement is known and its
current content/metadata still match**. A concurrent policy edit or lost
publication acknowledgement stops automatic restoration. The report then names
the backup and retained scope for the parent to compare manually; it does not
force the old policy over an administrator's newer policy. Temporary policy files
from a failed replacement are retained at
`/etc/virmill/.auxiliary-fixture-001-{root-only,grant,restore}` as applicable.

The final report is `auxiliary-inspection-native-001/report.json`. Individual
`command-NNN.json` records preserve bounded stdout/stderr, arguments and failure
state; the large fixed administration program is represented by its source hash.
Other artifacts include original public policy/identity, baseline journal/guest/
pool/file metadata, exact native XML, scoped policy reviews/receipts, positive
JSON/NDJSON records and each refusal. Independent preservation classes are still
observed after a failure in another class. A failed run remains
`uncertain-preserve-resources`; successful checks cannot override its failures.

Any eventual removal is a separate parent-reviewed operation after confirming
the exact newly generated UUID, stopped state, native XML, fixture-root identity,
no users/references and original policy restoration. No cleanup command is
automatically printed or executed.

## Authoring verification

Run locally only:

```sh
python3 tests/fixtures/protection/disposable-recorded-run/auxiliary-inspection-native-001.py --self-test
```

The self-tests cover strict JSON and UUID/table parsing, allowed pool counters
and refused malformed/changed configuration, exact policy scope and metadata
comparison, machine error envelopes, exact inventory/no-proof claims, generated
XML scope, host/private XDG separation, uncertain policy acknowledgement
retention and execution-pin guards. They compile the embedded root-administration
program without executing it. No local root, IPC, swtpm, libvirt or host operation
is performed by the self-tests. Native policy labels, explicit TPM-source
normalization, special-file refusal and installed helper behavior remain pending
the parent's separately recorded native run.

On 2026-09-08, all 13 authoring self-tests passed with zero failures and zero
skips. The outer Python and embedded administration source compiled, and UTF-8,
final-newline and trailing-whitespace checks passed. These are local synthetic
authoring checks; no native run result is recorded here.

Parent integration before first execution: the frozen 490b88c adapter returns
`INVALID_INPUT` (exit 2) when the native TPM source path is absent. The fixture
asserts that exact code and its native-path diagnostic; it does not accept an
unrelated malformed user request as the expected refusal. Supplied `~/images`
metadata is included alongside the prior run tree, and typed CLI refusals require
the documented exact exit code. These authoring corrections precede all native
execution.
