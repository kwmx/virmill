# swtpm cold writer exclusion

Date: 2026-09-08. SNAP-01 and BAK-01 dependency investigation under
[ADR 0020](../adr/0020-cold-recovery-boundary.md). This builds on the
[native state-resolution review](native-tpm-state-resolution.md); it neither
resolves an existing VM's implicit path nor changes the capture contract.

**A Linux OFD read lock on the exact swtpm lock object can exclude its cooperating
writer. It cannot establish that no producer exists, prevent a native restart,
or prove that every initialization path is mutation-free before locking.** Keep
the native stopped-state and restart-prevention boundary, verified source
identity, complete membership, and generation rechecks. A successful lock attempt
alone is insufficient authority to publish a complete TPM set.

## Reviewed versions and source boundary

Upstream review is pinned to swtpm **v0.10.2**, whose release page identifies
commit **32b5a0575991a73398de9749d63cafd847008af2**.
[Upstream release](https://github.com/stefanberger/swtpm/releases/tag/v0.10.2),
[release commit](https://github.com/stefanberger/swtpm/commit/32b5a0575991a73398de9749d63cafd847008af2).
The local generated-state experiment observed these installed packages:

| Component | Observed version |
|---|---|
| swtpm, swtpm-libs, swtpm-tools | 0.10.2-1.fc44.x86_64 |
| libtpms | 0.10.2-3.fc44.x86_64 |
| Linux | 7.1.13-200.fc44.x86_64 |
| Probe interpreter | Python 3.14.6 |
| Effective UID | 1000 |

Public installed binary SHA256 identities:

```text
/usr/bin/swtpm
bebe173c8305178c21c6ae7cb75ee43eca5405fa384d6eef422d99dd21671e18
/usr/bin/swtpm_ioctl
9e053690c8d074b68a05a24892b6793ca7aef9ce895413e484c60686e04a49ee
/usr/lib64/swtpm/libswtpm_libtpms.so.0.0.0
5ac6156459945433c289263ab36a893304a569c169cc08c201e61d99aba10c25
/usr/lib64/libtpms.so.0.10.2
d6f88b903d975e106b01e36e100806fcdee237dc89b91a67bfc27705f37f349b
```

`rpm -V swtpm libtpms` reported ownership/group differences in this execution
environment; it did not report content-digest differences. This is not a claim
of an entirely clean package verification or of the disposable remote host's
installed bytes. No dependencies were installed, upgraded, or downloaded.

The directory backend, common dispatcher, startup, main loop and control-channel
source were retrieved from official tagged URLs. Repeated official GitHub/raw
requests for `swtpm_nvstore_linear.c`, `swtpm_nvstore_linear_file.c` and
`tpmstate.c` returned cache misses. **File-backend lock type/object/lifetime below
are installed-runtime observations, corroborated by the official option manual;
its complete prepare/open/mmap/flush implementation was not source-reviewed.**
Do not promote that missing review into a supported block-device, shared-storage,
or initialization guarantee. No third-party source was substituted.

## What the producer actually locks

For directory storage, `SWTPM_NVRAM_Lock_Dir` constructs `.lock` beneath the
selected backend directory. It opens that object with
`O_WRONLY|O_CREAT|O_TRUNC|O_NOFOLLOW`, requested mode `0660`, and takes
`fcntl(F_SETLK)` with `F_WRLCK`, `SEEK_SET`, start zero and length zero
(through end of file, including growth). A static `lock_fd` holds it; repeat
calls succeed while that descriptor is already held. Failed attempts consume
the specified retries with 10 ms sleeps. `SWTPM_NVRAM_Unlock_Dir` closes the
descriptor and resets it to -1; it does not unlink the object. This lock contains
no PID-file protocol. The final-component nofollow flag does not validate every
ancestor. Truncation precedes acquisition, so refusal can still change lock-file
metadata. [Tagged directory backend](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/swtpm_nvstore_dir.c),
`SWTPM_NVRAM_Lock_Dir`, `SWTPM_NVRAM_Unlock_Dir`.

| Selected backend | Default | Exact cooperating lock observed locally |
|---|---|---|
| `dir=...` / `backend-uri=dir://...` | Enabled | POSIX whole-file write lock on `.lock`; tested with `dir=...` |
| `backend-uri=file://...` | Disabled | With `lock=true`, POSIX whole-file write lock on the state file itself |

The manual explicitly permits `lock=false` and requires opt-in for the file
backend. A block-device URI is documented, but was not exercised here.
[swtpm v0.10.2 manual](https://raw.githubusercontent.com/stefanberger/swtpm/v0.10.2/man/man8/swtpm.pod),
`--tpmstate`.

The common dispatcher selects directory or linear operations from the URI.
`SWTPM_NVRAM_Lock_Storage` returns success without taking a lock when locking is
disabled, returns `TPM_RETRY` before backend initialization, and otherwise delegates
to the backend. A successful high-level return must therefore be interpreted
with the actual policy and lifecycle.
[Tagged dispatcher](https://github.com/stefanberger/swtpm/blob/v0.10.2/src/swtpm/swtpm_nvstore.c),
`SWTPM_NVRAM_Init`, `SWTPM_NVRAM_Lock_Storage`, `SWTPM_NVRAM_Unlock`.

Libvirt 12.0.0's `qemuTPMVirCommandSwtpmAddTPMState` appends `,lock` for both
backend types when `VIR_TPM_SWTPM_FEATURE_TPMSTATE_OPT_LOCK` is available. Without
that capability, it omits the parameter. This source rule is useful provenance
to verify against the effective launch; it is not proof of a particular native
process's arguments or absence of overrides.
[Tagged libvirt launch builder](https://raw.githubusercontent.com/libvirt/libvirt/v12.0.0/src/qemu/qemu_tpm.c#L627).

## Lifetime and why process absence is separate

`tpmlib_start` invokes `TPMLIB_MainInit` before requesting storage locking and
then handles volatile-state deletion. Consequently startup failure due to a lock
does not, by itself, prove that all preceding library/backend preparation had
no effects. The probe checks unchanged ordinary state-member metadata for the
tested conflicting starts of existing generated state; it does not inspect state
contents or establish that result for every format/profile/version.
[Tagged startup implementation](https://raw.githubusercontent.com/stefanberger/swtpm/v0.10.2/src/swtpm/tpmlib.c#L256).

The control channel's `CMD_STOP` terminates the TPM library without terminating
the process. In both tested backends its writer lock stayed held until process
exit. The savestate-return path calls `mainloop_unlock_nvram`, so a live process
can also release its storage lock. `CMD_LOCK_STORAGE` actively asks the producer
to acquire locking; it does not lend a guard to a capture client. State-blob
commands can create/delete volatile state and are unsuitable as passive lock
queries. None was issued by this probe.
[Tagged control channel](https://raw.githubusercontent.com/stefanberger/swtpm/v0.10.2/src/swtpm/ctrlchannel.c),
`ctrlchannel_return_state`, `CMD_STOP`, `CMD_LOCK_STORAGE`.

`mainloop_unlock_nvram` clears its internal locked flag and installs retry policy.
The ordinary command loop invokes `mainloop_ensure_locked_storage` before command
processing. Incoming migration can defer acquisition; the generated experiment
observed an incoming producer responding to its capability query while the
capture-style guard was held. Outgoing release and incoming reacquisition are
protocol behavior, not evidence that the process exited.
[Tagged main loop](https://raw.githubusercontent.com/stefanberger/swtpm/v0.10.2/src/swtpm/mainloop.c#L82),
[migration option manual](https://raw.githubusercontent.com/stefanberger/swtpm/v0.10.2/man/man8/swtpm.pod).

## Recommended guard and observation boundary

Traditional POSIX record locks belong to the process. Closing **any** descriptor
for that inode releases that process's locks; fork does not inherit them.
Linux OFD locks instead belong to the open file description, survive duplicates,
and release on its last close. OFD read locks conflict with POSIX write locks.
BSD `flock` is a different mechanism on the tested local filesystem: the probe
acquired it while swtpm held its write lock.
[Linux record-lock semantics](https://man7.org/linux/man-pages/man2/fcntl_locking.2.html).

The following is a proposed helper design, not an implemented capture endpoint:

1. Begin with the independently verified VM-to-source mapping and operation-bound
   lease from ADR 0020. Confirm stopped native state and exclude starts, reset,
   setup, migration, and other auxiliary writers for the entire window. A journal
   lock only excludes Virmill participants; it does not constrain every libvirt
   client. Ambiguous effective locking policy remains unresolved.
2. Resolve the approved source through held directory descriptors. Pin the lock
   inode and its parent association without following links. Require an existing
   single-link regular object; refuse missing/aliased/special objects. Do not
   create or truncate `.lock` in a read-only capture helper. For the directory
   case it is synchronization metadata, classified explicitly in the inventory.
3. Open the existing lock object `O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC` beneath
   that held root. Take a nonblocking `F_OFD_SETLK/F_RDLCK` over `[0, EOF)`.
   `EAGAIN`/`EACCES` means busy; unsupported locking/filesystem behavior, malformed
   metadata, cancellation and all other errors fail visibly. A bounded retry may
   wait for independently expected shutdown; never kill an existing producer or
   remove its lock object as a capture fallback.
4. Keep that guard through enumeration, sealed copying and final membership/
   generation/native-state checks. Validate that the named lock still identifies
   the held inode. The probe demonstrates that replacing `.lock` lets a new
   producer acquire a different inode despite the original guard. Pinning alone
   does not stop such directory changes; unexpected replacement invalidates the
   entire capture.
5. `F_GETLK` on the pinned object can diagnose a conflicting POSIX owner's PID,
   but is a transient observation. Binding a PID to an authorized swtpm instance
   requires process generation/executable/transport evidence from the native
   resolver. A missing PID/socket, empty lock file, old mtime, or unlocked object
   is not producer-absence evidence. Keep the acquired guard, do not merely query
   it and close the descriptor.
6. Retain the guard in the helper until the complete sealed set and final checks
   exist. If the architecture transfers a live guard, pass the **same OFD** and
   retain recipient custody; do not explicitly unlock it during sender cleanup.
   A passed POSIX-locked descriptor would not transfer process-owned exclusion.
   The probe tests `dup`/close lifetime, not an end-to-end helper SCM_RIGHTS lease;
   authenticated transfer, cancellation and helper-death tests remain required.

When the helper returns only sealed immutable snapshots, it can release its
source guard after final verification: the sealed bytes no longer depend on
source mutability. The coordinator still must bind the entire capture cut and
must not publish a complete set if its other source observations became invalid.
Arbitrary writers, disabling locking, path substitution, remote filesystems and
host-administrator actions are outside a cooperative lock's enforcement.

## Reproducible local probe and actual results

The authored [probe](../../tests/fixtures/protection/swtpm-lock-probe/probe.py)
uses only original generated state in a fresh private `/tmp/vml-swtpm-*` tree.
It accepts no source path. It refuses root and non-Linux/non-x86_64 environments,
pins the reported emulator version, bounds commands and generated files, uses
argument arrays, and reaps only its own children. It does not call libvirt, SSH,
host services, TCP, `/dev/tpm`, or guest lifecycle APIs. It reads public binaries,
its own diagnostics, and file metadata; it does not read/hash/export TPM state
bytes. The subprocess itself necessarily generates/loads its disposable state.

Commands actually used:

```text
rpm -q swtpm swtpm-libs swtpm-tools libtpms
swtpm --version
swtpm socket --help
swtpm_ioctl --help
python3 tests/fixtures/protection/swtpm-lock-probe/probe.py
```

Each normal producer uses this argument shape, with every path generated by the
recipe under its new private tree:

```text
/usr/bin/swtpm socket --tpm2 --tpmstate <generated-backend-argument>
  --ctrl type=unixio,path=<private>/c,mode=0600
  --server type=unixio,path=<private>/s,mode=0600
  --flags not-need-init,startup-clear
```

The incoming case omits `--flags` and supplies `--migration incoming`. The only
control operations are the fixed capability query and
`/usr/bin/swtpm_ioctl --unix <private>/c --stop`. Graceful termination and
intentional `SIGKILL` apply only to generated probe processes. No TPM state-blob
or TPM NV command is issued by the recipe.

The initial sandbox-only Unix bind failed with `EPERM`; no swtpm process started
in that prerequisite check. The narrow private-IPC escalation was then approved.
The final native dependency run passed **7 cases, 0 failures, 0 skips**, spawning
and reaping **16 owned swtpm processes** in 0.148 seconds of reported command time:

| Case | Observed result |
|---|---|
| Directory default locking | Correct POSIX owner/range; OFD conflict; second producer refused; `--stop` retains lock; exit/crash releases it; stale inode retained |
| File explicit locking | Same lock and lifetime assertions, on the state file itself |
| OFD descriptor lifetime, within both cases | Competing starts refused before and after closing the original duplicate; admitted after final close |
| Directory `lock=false` | Producer alive with no conflicting lock |
| File default policy | Producer alive with no conflicting lock |
| Incoming migration | Producer alive and capability query succeeds while OFD guard is held |
| Directory lock substitution | New producer admitted on replacement inode; generation mismatch detected |
| POSIX duplicate close | Closing the duplicate releases the process-owned guard and permits producer startup |

OFD descriptor lifetime is a subcase, not an eighth top-level case. Tested refused
starts left state-member metadata unchanged; `.lock` metadata is intentionally
separate because producer open/truncate precedes lock acquisition. All temporary
files were removed after child cleanup. No production files or acceptance ledger
were edited by this investigation.

This establishes local dependency interoperability and concrete failure modes.
It is not hardware, complete capture, restore, boot, remote-host, encrypted-state,
block-device or network-filesystem qualification. The next dependency-ready work
is a native-identity-bound directory guard with restart prevention and membership
rechecks, followed by failure/cancellation/transfer tests. Complete file-backend
source review and equivalent native qualification remain prerequisites before
claiming that backend as a supported capture path.
