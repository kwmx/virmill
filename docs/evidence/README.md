# Release evidence ledger

`ledger.jsonl` is append-only. Entries record command, time, revision/source tree
digest, exact environment, fixture digests, result, evidence class and retained
log hashes. Failed/blocked checks remain visible. Tests against synthetic objects
do not certify actual virtualization, packet flow, firmware, TPM or USB.

`requirements.json` maps all 71 immutable acceptance IDs to implementation,
planned tests, evidence and remaining work. Generated `../implementation-status.md`
is a readable view. No row passes solely because a related unit test passed.
The release check fails while any mandatory row lacks its required evidence.


Latest host evidence: [first disposable-host run](disposable-first-run.md). Package
installation/upgrade, real QCOW2 preparation, native storage streaming and OVA
manifest checks were exercised. Creation remains blocked by unreviewed native
device defaults; the new VM is powered off and its uncertain job retains its locks.

Prior development checkpoint: [fixed CPU/RAM configuration](configuration-slice.md).
Its native in-memory preservation tests and synthetic recovery remain separate
from real VM, guest sizing and concurrent external-writer qualification. The prior
[NoCloud checkpoint](nocloud-slice.md) records actual confined seed construction.
