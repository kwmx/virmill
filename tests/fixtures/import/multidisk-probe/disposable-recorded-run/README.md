# Recorded disposable multi-disk run

These are the exact Python stdin programs executed over strict, non-forwarding
SSH on the owner-authorized disposable Fedora VM on 2026-09-07. SSH destination,
keys, raw images, screenshots and the private coordinator environment are omitted.
See [the evidence report](../../../../../docs/evidence/multidisk-crash-run.md).

This is an audit recipe, not a command to run unchanged. UUIDs, paths, digests,
installed-binary hashes and plan expiration times identify the historical run.
Generate fresh fixtures and fresh review plans in a newly authorized environment.
Never replay the recorded apply programs against another host or an existing job.

Execution order:

1. `multidisk-prerequisites.py`: installed tools, free space and stopped XML.
2. `build-multidisk-probe.py`: verify the adjacent generator's exact source hashes
   in the host's private recipe directory, then generate a new OVA as UID 1000.
3. `multidisk-inspect-plan.py`: successful native inspection and preview, followed
   by a harness `KeyError` on `review.systemID`. It applied nothing. The unmodified
   failure is retained; the next program checks `review.system.id` correctly.
4. `multidisk-import-tui.py`: review and apply that existing preview in a real PTY,
   detach, then observe success and verify the published artifacts with the CLI.
5. `multidisk-pool-setup.py`: external isolated pool fixture; it does not test
   Virmill pool creation. Autostart stays disabled; existing guests are preserved.
6. `multidisk-create-plan.py`: explicit two-disk, no-NIC Q35/BIOS creation preview.
7. `multidisk-active-upload-crash.py`: start the actual creation, observe durable
   upload intent and increasing native allocation, SIGSTOP the pidfd-held test
   coordinator, recheck the incomplete boundary and SIGKILL only that process.
   It hashes retained copies, then restarts the same transient user unit. If the
   boundary is missed, it does not claim a crash or blindly replay creation.
8. `multidisk-recovery-refusals.py`: observe recovery-required state, unchanged
   receipt/files/locks, explicit reconcile refusal, and incomplete-set resume
   refusal. It also verifies that no definition was created.
9. `multidisk-retain-tui.py`: actual TUI retention review and approval; check
   durable pins, partial parent, successful child, lock release and closed recipe.
10. `multidisk-fresh-plan.py` and `multidisk-fresh-create.py`: new identity/volumes
    from the same successful preparation, full readback and define-last success.
11. `multidisk-start-capture.py`: separately reviewed start, external QMP KVM
    observation and screenshot. The image was visually inspected afterward.
12. `multidisk-stop-preserve.py`: explicit reviewed hard-stop for the halted BIOS
    program, then complete source/copy/pin/XML preservation checks.

Only generated files in the new private source directory and the newly owned
pool are mutation targets. The crash affects the ordinary-user coordinator, not
the host, libvirt daemon or unrelated guests. No file or pool deletion occurs.
Hard stop is explicit because this probe has no OS/ACPI shutdown handler. One
SIGKILL boundary is not a power-loss, filesystem-durability or full fault matrix.
