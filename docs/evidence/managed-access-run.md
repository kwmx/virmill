# Managed-volume access development evidence

ADR 0018 implements the explicit read-grant/restoration slice. The signed helper was installed from clean revision `b7fe053` and exercised
against an actual root-owned mode-0600 managed disk on the authorized disposable
Fedora 44 host. Both CLI/TUI grant and restoration cycles passed. This is one
native configuration; the complete permissions and recovery matrix remains open.

Recorded development checks preserve their own source digest and scope:

- `storage-access-acl-development-001`: initial test expectation used the wrong
  post-grant mask mode; fixed from 0651 to 0671. The ACL calculation did not change.
- `storage-access-acl-sandbox-001`: the outer coding namespace maps only UID 1000;
  Linux rejected a generated-file ACL naming unmapped UID 65534. This is an actual
  environment limitation, not passing ACL evidence.
- `storage-access-acl-kernel-001`: ordinary-user temporary-file run outside that
  namespace passed grant/restoration, unchanged content and refusal tests.
- `storage-access-service-fixture-001`: fixture directory lacked the journal's
  required mode 0700. The fixture was corrected; permission enforcement remains.
- `storage-access-recovery-context-001`: lost-acknowledgement test found that
  explicit engine reconciliation omitted the accepted job ID. The engine now
  supplies that original context. The historical entry accidentally labels one
  association `OPS-01`; the canonical tracker maps this failure to **JOB-02**.
  No requirement was added or renamed and the original ledger is preserved.
- `storage-access-peer-kernel-001`: actual ordinary-user Unix socket peer/group
  snapshot and private PKCS8 key tests passed. Disposable-root tests were explicitly
  skipped here. `SO_PEERGROUPS` avoids `/proc` PID races and NSS group guesses.
- `storage-access-core-001` and `storage-access-static-001`: full race-enabled
  core suite and static analysis passed at their recorded development source.
- `helper-access-root-tests-001`: the authorized disposable host ran real root-owned
  temporary-file ACL/journal tests for grant, restore, missing completion record,
  stale metadata, simulated native-state drift and changed-ACL refusal. Native VM
  inventory was synthetic. Its exact binary hash is retained, but the dirty source
  digest was not captured at compilation; it is not immutable-source qualification.

Subsequent changes bind a fresh ctime for reverse-order grant restoration, verify
that the observed path still resolves to the held file, validate private root
journal/policy directories, and budget bounded metadata records. The clean build repeated the relevant checks: `storage-access-clean-core-001`,
`storage-access-clean-static-001`, `storage-access-cross-001`,
`storage-access-artifacts-001` and `storage-access-repro-001` passed. Two clean
builds produced identical hashes for all three binaries and four unsigned
development packages. Local builds have not changed VM, network,
storage-service or host policy configuration.

The source adds no dependency versions: Go/modules remain pinned in the lock file
and vendor tree. The helper package now depends on the native libvirt library for
its independent VM/pool/volume checks. The disposable setup observed OpenSSL
`3.5.8-1.fc44.x86_64`, libvirt libraries `12.0.0-3.fc44.x86_64` and ACL tools
`2.4.0-1.fc44.x86_64`. OpenSSL is only an optional key-setup tool.

No complete acceptance row is promoted by these checks. Firmware/TPM capture and
restore, USB, network packet/isolation behavior, full storage workflows, complete
permissions/recovery coverage and all remaining mandatory v1 work stay required.

The native runtime source digest is
`c7071d1aaaf146058986ec06b887d8d59f8e33072dbec44cfb4596bcccf617f0`.
The ledger binds exact packages, fixture programs, logs and execution environment.
The `helper-access-upgrade-001` run preserved 20 previous jobs and all four stopped
VM definitions. `helper-access-setup-001` first observed default permission denial,
then installed the approved actor/root/public-key policy and a socket group drop-in.
No private signing-key contents entered the repository or logs.

`helper-access-native-grant-001` granted actor read access to the probe's `vda`
without adding write access or changing disk bytes. The initial TUI revoke harness
failed because it expected an invented marker; its failure remains in
`helper-access-native-tui-revoke-001`. It created a preview, not an applied job.
`helper-access-native-tui-revoke-resume-001` reopened and applied that same plan,
restoring the exact original ACL, ownership and mode and removing actor access.
There was no fallback chmod or blind replay. The second cycle,
`helper-access-native-tui-grant-cli-revoke-001`, passed TUI grant and CLI revoke.
All four accepted operations completed and released their locks.

`helper-access-root-tests-clean-001` repeated six real root temporary-file
ACL/journal cases from the immutable build, including missing completion and
nested grant restoration. Its native VM inventory is synthetic, explicitly
separate from the actual managed-volume cycles.
`helper-access-native-auth-preserve-001` verified actual kernel-peer mismatch,
other-UID socket denial, forged signature and generic root-action refusal with no
new root records. Four VM definitions/stopped states, four multidisk pool files
and the generated OVA remained unchanged. The final journal held 24 terminal jobs
and no resource locks. The helper service and socket were stopped and never enabled;
private keys, policy and root recovery records were retained on the disposable VM.
SELinux remained enforcing. Supplied source media and unrelated guests were not
modified by this slice.
