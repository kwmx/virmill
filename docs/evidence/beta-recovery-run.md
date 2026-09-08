# Encrypted recovery and beta artifacts — 2026-09-08

The frozen beta revision `b78cc23457198bcb842cb8dc125915667b5eba7c` completed
independent encrypted repository recovery on the authorized Fedora 44 test VM.
The exact source digest, executable hashes and commands are in the ledger.
This evidence contributes to BAK-02 and BAK-06; it is not complete backup or
firmware/TPM qualification.

`beta-recovery-native-002` successfully initialized a new restic repository,
backed up the complete synthetic two-disk BIOS capture, independently restored
and verified every member, and checked all repository data. Its next fixture
profile had a Unix socket pathname over Linux's 107-byte limit and failed
before recovery. The failed record remains unchanged.

`beta-recovery-native-003` used the same verified binaries and retained
repository. Its shorter, entirely new profile began with an empty journal.
Through actual CLI/daemon plans and durable jobs it:

- Recovered the capture from the encrypted repository without the original DB.
- Verified the externally supplied capture UUID and complete manifest hash.
- Refused a second recovery over the existing destination.
- Created a new disconnected BIOS VM with two independent volumes, then booted it.
- Stopped only that synthetic guest and verified all prior definitions and the
  original capture bytes remained unchanged.

The recovered VM is `f5d205f0-db1b-4db0-b232-cf17376de137`; its capture recovery
operation is `82d0116b-458d-40c4-bd0c-b22dc4cd8ccf`, VM restore operation
`bee342fb-2ce6-43d3-ae49-1bae41f3f641`, and start operation
`a61bf1e6-0096-414e-b474-e25d6d46d204`.
The screenshot was retrieved and visually inspected: **VIRMILL BIOS MULTIDISK
PROBE** and **VIRMILL SECOND DISK PASS** are visible in the
[actual recovered display](images/encrypted-recovery-second-disk.png).
The automated fixture keeps `bootMarkerVerified=false` because it does not read
screen text; the visual observation is separate evidence.

The retained repository is in the private `beta-recovery-b78cc23-002` test root;
the fresh journal, recovered capture, logs and screenshot are in
`beta-recovery-b78cc23-003`. Both are below the authorized user's `virmill-tests`
directory. Credential bytes remain only on that VM and are not in the ledger.
New guest volumes remain in the existing disposable test pool. No source media
was overwritten or transferred outside the authorized host/workspace.

The earlier `beta-recovery-native-001` discovered that actual restic creates its
repository config with mode 0400. The service incorrectly required 0600 for
that non-secret config. The correction accepts 0400 or 0600 while retaining
strict 0600 for the credential. Original uncertain intent and resources were
retained; the correction did not replay that operation.

`beta-recovery-race-001` at the preceding `d039af5` revision passed 4,593 Go test
records (784 top-level tests), with zero failures and 25 skipped opt-in probes.
`beta-recovery-vet-001` passed. The config-mode correction passed focused tests.
`beta-recovery-fix-reproducibility-001` then reproduced all three executables and
four DEB/RPM packages at `b78cc23` twice with identical hashes.
`beta-recovery-fix-artifacts-001` passed package/archive metadata and staged
install/uninstall preservation checks; its optional IPC case was skipped.
These are scoped checks, not a complete native distro install matrix.
