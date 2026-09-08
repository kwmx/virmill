# swtpm FILE backend writer exclusion

Date: 2026-09-08. Source review and unexecuted native-probe deliverable for
SNAP-01 and SEC-03 prerequisites. No acceptance promotion or capture architecture
change. [ADR 0020](../adr/0020-cold-recovery-boundary.md) and
[ADR 0022](../adr/0022-authenticated-auxiliary-inventory.md) retain their current
boundaries.

**In swtpm v0.10.2 the FILE backend locks the state inode itself, but lock refusal
does not imply that opening or preparing that inode had no prior effects.** The
exact source now fills the missing file-backend implementation review recorded
in [the earlier investigation](swtpm-cold-writer-exclusion.md). Its native results
remain historical evidence from a different fixture; they were not rerun here.

The reviewed primary tag is `stefanberger/swtpm` **v0.10.2**, release commit
`32b5a0575991a73398de9749d63cafd847008af2`. Tagged files were read through the
read-only GitHub connector after web cache misses. No downloaded code was
executed. The table records connector-reported Git blob IDs, not SHA256 hashes
or installed dependency attestations.

| Source under the tag | Git blob ID |
|---|---|
| `src/swtpm/swtpm_nvstore_linear_file.c` | `cb03e03f10d80cab6a1dfc3292d217b4beac3837` |
| `src/swtpm/swtpm_nvstore_linear.c` | `dad5551b62f9039006ccb7b445baeb278115d02d` |
| `src/swtpm/swtpm_nvstore.c` | `725057cb48c82712762869cbd9730bb1f7834f50` |
| `src/swtpm/tpmstate.c` | `043c7b6f2f5415bfd11544bac4d6026eefcd5951` |
| `src/swtpm/tpmlib.c` | `367fdb95fe8395ba0681badbdc00e277004573fe` |
| `src/swtpm/common.c` | `dbe6e9adf49f36c907e0dc2030bc6095e9db8402` |

**Exact object, policy and lifetime**

`SWTPM_NVRAM_LinearFile_Lock` calls `DoOpenURI`, then requests a nonblocking
POSIX `F_SETLK` write lock on `mmap_state.fd`, using `SEEK_SET`, start zero and
length zero. This covers the whole state file, including growth. It retries the
requested number of times with 10 ms delays. A failure invokes cleanup; unlock
uses `F_UNLCK` on that same descriptor. Cleanup flushes/unmaps an existing mapping
and closes the descriptor. There is no separate FILE-backend `.lock` object.
[Tagged file backend:324](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/swtpm_nvstore_linear_file.c#L324),
[cleanup:253](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/swtpm_nvstore_linear_file.c#L253).

The common dispatcher selects linear storage for `file://`, delegates lock and
unlock, and returns success without locking when the configured policy disables
it. The manual specifies FILE locking disabled by default and explicit `lock`
or `lock=true` to enable it; `lock=false` disables it. Command-line handling stores
that parsed policy in `tpmstate`. No lock assertion can be inferred from a
successful high-level return without its effective policy.
[Tagged dispatcher:175](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/swtpm_nvstore.c#L175),
[locking:210](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/swtpm_nvstore.c#L210),
[option handling:739](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/common.c#L739),
[pinned manual](https://raw.githubusercontent.com/stefanberger/swtpm/v0.10.2/man/man8/swtpm.pod).

Libvirt's private capability-dependent `,lock` choice is already traced in the
[lock-selection review](libvirt-swtpm-lock-selection.md). This work does not
reclassify public XML or domain capabilities as proof of that private choice.
Stopped process/lock lifetime, disabled locking, migration and POSIX/OFD/BSD
interoperability retain the earlier review's limits.

**File-specific effects before locking**

`DoOpenURI` strips `file://` and opens `O_RDWR|O_CREAT`; it does not set
`O_NOFOLLOW`, and its subsequent explicit-mode `fchmod` occurs before locking.
`Open` then maps the file read/write with `MAP_SHARED`. A regular file shorter
than the linear header is enlarged first. These producer operations are not
safe read-only path validation, and a cooperative lock cannot enforce them.
[Tagged file open:157](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/swtpm_nvstore_linear_file.c#L157),
[mapping:45](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/swtpm_nvstore_linear_file.c#L45).

`SWTPM_NVRAM_Prepare_Linear` invokes the file open operation and initializes and
flushes a new linear header when the magic value does not match. That preparation
is dispatched by `SWTPM_NVRAM_Init`. `tpmlib_start` calls `TPMLIB_MainInit` before
its subsequent `SWTPM_NVRAM_Lock_Storage(0)` request. Thus a refused start must not
be classified as an effect-free attempted writer solely from the lock error.
The probe measures the resulting mode/size metadata on newly generated fixtures;
it does not establish byte invariance for valid files, nor all possible libtpms
initialization paths.
[Tagged linear preparation:227](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/swtpm_nvstore_linear.c#L227),
[tagged startup:276](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/tpmlib.c#L276).

The path is not a permanent lock identity: renaming away a guarded state file and
installing another inode makes a later producer open a different object. This is
a source-derived expected failure mode, to be exercised by the new native case.
The original inode may remain correctly guarded while the pathname no longer
refers to it. Existing metadata inventory's rooted identity/generation checks
remain necessary, as does independent prevention of restarts and setup effects.
No lock test alone supplies either guarantee.

The source also permits a block-device FILE URI, and mapping/resizing has
different branches for it. This investigation deliberately qualifies no block
device, network/shared filesystem, encrypted state or arbitrary linear image.
It adds no trusted parser for TPM payloads and does not weaken existing symlink,
hardlink, owner, metadata or source-path refusals.

**Authored probe and evidence level**

The new [standalone probe](../../tests/fixtures/protection/swtpm-file-lock-probe.py)
and [operator notes](../../tests/fixtures/protection/swtpm-file-lock-probe.md)
define four FILE-specific cases: initialized-file exclusion as calibration;
explicit-mode changes before refusal; short-file growth before refusal; and
state-file replacement while the old OFD guard remains held. Directory
experiments, STOP/dup-close/crash lifetime matrices and disabled-lock cases are
not repeated. The file-state lock and one same-inode refusal are required setup
checks for the new effects, not a separate claim of new directory evidence.

Pure self-tests ran with all subprocess creation, socket creation and kernel
`fcntl` operations blocked: **22 tests passed, 0 failures, 0 skips**. They test
fixed source/argument policy, exact binary-version and lock-diagnostic parsing,
combined output/report limits, partial reads, cancellation/deadlines, unsupported
locking errors, the write-conflict query needed to observe an OFD read guard,
descriptor ownership, setup-failure cleanup and bounded own-child termination.
Documentation whitespace and local link-target checks passed. There were no
native process/IPC experiments, libvirt calls, guest operations, escalations,
existing-state accesses or ledger edits in this assignment.

A future parent-operated native run would establish only the recorded behavior
of the exact installed executable and generated files on the recorded local
filesystem/kernel. It requires the fixed ordinary-user Linux x86_64 environment,
three reviewed dependency hashes, supported private Unix IPC and available
TPM 2 FILE functionality. Its adverse-effect cases intentionally fail if the
predicted observation is absent or ambiguous; such failure must be preserved and
reviewed, not generalized into safety.

Even an eventual passing native report would leave real-VM source mapping,
historical TPM identity, producer provenance, every-writer exclusion, confidential
content capture, all-member completeness, helper grant/lifetime behavior,
independent restore and guest recovery unverified. All capture/guest/hardware
success flags remain false. SNAP-01 and SEC-03 are prerequisite mappings only;
their acceptance requirements remain open.
