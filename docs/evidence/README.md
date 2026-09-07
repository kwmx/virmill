# Release evidence ledger

`ledger.jsonl` is append-only. Entries record command, time, revision/source tree
digest, exact environment, fixture digests, result, evidence class and retained
log hashes. Failed/blocked checks remain visible. Tests against synthetic objects
do not certify actual virtualization, packet flow, firmware, TPM or USB.

`requirements.json` maps all 71 immutable acceptance IDs to implementation,
planned tests, evidence and remaining work. Generated `../implementation-status.md`
is a readable view. No row passes solely because a related unit test passed.
The release check fails while any mandatory row lacks its required evidence.


Latest installed build: [creation/cleanup result readback](result-progress-run.md).
The [boot/media run](boot-media-run.md) records actual CLI/TUI configuration,
ISO-menu and disk-login boots, retained-media ejection and XML preservation.
The [device-policy run](device-policy-run.md) records one successful Q35/BIOS
creation. These observations do not complete the mandatory workflow matrix.

The [first disposable-host run](disposable-first-run.md) records package
installation, QCOW2 preparation, native storage streaming and OVA manifest checks.
Its earlier uncertain creation remains powered off with three retained locks;
safe reviewed acceptance or disposition is still outstanding. The later successful
creation did not change that operation's semantics or remove its locks.

Prior development checkpoint: [fixed CPU/RAM configuration](configuration-slice.md).
Its native in-memory preservation tests and synthetic recovery remain separate
from real VM, guest sizing and concurrent external-writer qualification. The prior
[NoCloud checkpoint](nocloud-slice.md) records actual confined seed construction.
