# Managed-volume access development evidence

ADR 0018 implements the explicit read-grant/restoration slice. The original
root-owned mode-0600 disk limitation remains a qualification item until the new
helper is installed and exercised against actual native mapping.

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
journal/policy directories, and budget bounded metadata records. The final clean
build must repeat the relevant checks. Local builds have not changed VM, network,
storage-service or host policy configuration.

The source adds no dependency versions: Go/modules remain pinned in the lock file
and vendor tree. The helper package now depends on the native libvirt library for
its independent VM/pool/volume checks. The disposable setup observed OpenSSL
`3.5.8-1.fc44.x86_64`, libvirt libraries `12.0.0-3.fc44.x86_64` and ACL tools
`2.4.0-1.fc44.x86_64`. OpenSSL is only an optional key-setup tool.

No complete acceptance row is promoted by these checks. Firmware/TPM capture and
restore, USB, network packet/isolation behavior, full storage workflows, complete
permissions/recovery coverage and all remaining mandatory v1 work stay required.
