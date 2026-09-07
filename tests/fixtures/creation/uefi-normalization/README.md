# Recorded non-secure UEFI creation normalization

`observed.xml` is the complete native persistent XML supplied by the owner after
a disposable-host creation mismatch on source revision
`44a46b6222a601661eb993ba905b63d2396a2154`. `reviewed-plan.json` is the supplied
reviewed creation plan. Both were copied byte-for-byte from
`.virmill-local/cold-probe-mismatch.xml` and
`.virmill-local/cold-probe-creation-plan.json`; no sanitization, reformatting or
field removal was needed or performed. The supplied files contain configuration
and public identifiers, not firmware/TPM state bytes or secret values.

| Captured artifact | SHA-256 |
|---|---|
| `observed.xml` | `36a51eefeb3f4b48f46f1308668b1e4c3cef3b19c03ba8b9fd7da6b70c61c500` |
| `reviewed-plan.json` | `bb0bd8d8d4c3a82cf4f292500152cbd4bda153d110a481c5c2a0b09de542b8aa` |

The plan ID is `a7232967-796b-4526-9423-e51a9b4bf9d2`, created at
`2026-09-07T19:33:20.266428859Z`. The VM UUID is
`f78674f3-bf3a-43e5-81f9-4283e2472024`. The owner reported creation job
`c809898c-e1be-45e2-b31d-644ad7bc75f0` as `recovery-required`, with the VM stopped,
disk verified and three resource locks retained at this failure. These are
failure-provenance facts, not a statement of the job's current state. Replaying
the fixtures does not reconcile that job or release its locks.

The test reproduces the renderer's target/volume inputs from the captured plan.
It independently derives the creation binding with
`operations.Digest([]string{planID, inputDigest})`, obtaining
`3fde7525e4b89e4acd513aa61dc7340841adc72c8ef10298252fe9ed319b6059`.
The byte hashes are checked before each replay.

The unchanged prior matcher rejects the complete observed XML. Removing only
`os/@firmware='efi'` and the two-member `os/firmware` feature block makes the
prior matcher accept it under the existing reviewed device policy. Removing
either addition alone does not. This isolates the sole previously unmatched
normalization in this capture. The full-capture regression was observed failing
with `RECOVERY_REQUIRED` before the matcher change, then passed afterward.

The captured XML validates with the locally installed
`libvirt-libs-12.0.0-3.fc44.x86_64` schema:

```sh
xmllint --nonet --noout --relaxng /usr/share/libvirt/schemas/domain.rng tests/fixtures/creation/uefi-normalization/observed.xml
```

Local schema validation is distinct from the captured host's runtime versions;
the two supplied files do not encode those package versions. Native execution
and recovery evidence remain in the owner's separate ledger. The matching
qcow2 test is a generated metadata variant; this captured native cohort used
raw firmware. No test in this file opens the recorded firmware or NVRAM paths,
contacts a backend, initializes TPM/NVRAM, starts a guest or performs recovery.
