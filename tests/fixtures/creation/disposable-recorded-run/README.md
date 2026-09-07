# Recorded disposable-host procedure

These are the exact Python programs sent over SSH on 2026-09-07 for the first
owner-authorized Fedora test run. Their hashes match the `stdinSHA256` fields in
the `native-first-*` evidence entries. They are retained for audit; SSH destination,
credentials, source media, journals and private runtime data are not included.

They reference that run's directory, fixture digests, immutable plan digests and
operation IDs. They are not a general-purpose installer or automatic CI test.
Some deliberately replace test packages or SIGKILL the identified test coordinator.
Execute such actions only in the explicitly authorized disposable environment.
Do not replay them against another host or substitute arbitrary discovered resources.

For a fresh reproduction:

1. Designate a disposable Fedora host with exact recorded dependencies, sufficient
   free storage, unprivileged Virmill access and legitimately supplied media. Record
   source hashes and baseline existing inventory before mutations.
2. Install the checksum-verified development RPMs. Keep the helper disabled. Create
   private XDG state/runtime/cache/data/config directories and a separate test pool.
   Pool setup in this run used virsh as external fixture preparation; it is not
   evidence that Virmill's pool-management workflow is complete.
3. Extract only the selected QCOW2 archive using the bounded, unprivileged
   bubblewrap/7-Zip procedure. Keep originals read-only. Use the documented
   `import prepare-disks` input with explicit hash, format and virtual capacity.
4. Review and apply a newly generated preparation plan. Use its successful operation
   ID to request creation with the chosen pool, explicit hardware and no NICs.
   Generate fresh plans/IDs rather than copying the recorded authorization values.
5. Check actual stage results. On the recorded source revision, native storage
   readback passes and Q35 definition confirmation fails on unreviewed device
   defaults. Preserve the stopped definition and disk; do not force acceptance.
6. Exercise read-only TUI inventory/jobs and uncertainty persistence separately.
   Systemctl uses the login's normal runtime/bus; only Virmill clients get the
   private XDG environment. The recorded SIGKILL test verified no native replay
   after an already-uncertain operation, not a crash during active upload.

See `docs/evidence/disposable-first-run.md` for results and remaining work. No
VM boot or hardware qualification is asserted by replaying the captured XML or
reading these scripts.

The later `device-policy-*` programs and related setup/upgrade/boot/stop programs
record the c7f8b76 run documented in `docs/evidence/device-policy-run.md`. These
include actual creation, boot and graceful shutdown of a separate guest, plus
external QMP/screenshot inspection. The first TUI harness failed on coalesced key
input before any mutation; its corrected retry is retained separately. Old plan
IDs/digests expire and must never be replayed as a fresh authorization.
