# Release evidence ledger

`ledger.jsonl` is append-only. Entries record command, time, revision/source tree
digest, exact environment, fixture digests, result, evidence class and retained
log hashes. Failed/blocked checks remain visible. Tests against synthetic objects
do not certify actual virtualization, packet flow, firmware, TPM or USB.

`requirements.json` maps all 71 immutable acceptance IDs to implementation,
planned tests, evidence and remaining work. Generated `../implementation-status.md`
is a readable view. No row passes solely because a related unit test passed.
The release check fails while any mandatory row lacks its required evidence.


Latest native evidence: [firmware reconciliation and TPM/NVRAM persistence](cold-fixes-native-run.md).
A distinct 4 MiB firmware probe reported SEEDED then PRESERVED across two KVM
boots; the retained 2 MiB probe failed TCG2 lookup. CLI/TUI inspection continues
to report unresolved TPM storage. Complete capture and independent restore remain
unverified. Failed observer attempts and both firmware outcomes are retained.

Earlier native evidence: [multi-disk import and active upload crash](multidisk-crash-run.md).
The generated split-VMDK probe passed preparation, an actual interrupted native
upload, TUI retention, fresh two-disk creation and a KVM second-disk read. It does
not qualify an OS family or the full recovery matrix. The same installed build
previously passed [retained creation acceptance](creation-acceptance-run.md).
The original uncertain definition was separately accepted with reviewed devices
and guarded byte verification. Its original job remains partial, linked to the
successful acceptance; all inherited locks were released after confirmation.
Earlier [creation/cleanup result readback](result-progress-run.md) remains recorded.
The [boot/media run](boot-media-run.md) records actual CLI/TUI configuration,
ISO-menu and disk-login boots, retained-media ejection and XML preservation.
The [device-policy run](device-policy-run.md) records one successful Q35/BIOS
creation. These observations do not complete the mandatory workflow matrix.

The [first disposable-host run](disposable-first-run.md) records package
installation, QCOW2 preparation, native storage streaming and OVA manifest checks.
Its earlier uncertain creation remains powered off. The separate acceptance run
resolved its ownership without rewriting its original recipe or incomplete receipt.

Prior development checkpoint: [fixed CPU/RAM configuration](configuration-slice.md).
Its native in-memory preservation tests and synthetic recovery remain separate
from real VM, guest sizing and concurrent external-writer qualification. The prior
[NoCloud checkpoint](nocloud-slice.md) records actual confined seed construction.

Latest managed access: [signed-helper CLI/TUI native cycles](managed-access-run.md).
Parallel discovery and plugin work: [integration record](discovery-plugin-run.md).
