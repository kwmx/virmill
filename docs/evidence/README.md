# Release evidence ledger

`ledger.jsonl` is append-only. Entries record command, time, revision/source tree
digest, exact environment, fixture digests, result, evidence class and retained
log hashes. Failed/blocked checks remain visible. Tests against synthetic objects
do not certify actual virtualization, packet flow, firmware, TPM or USB.

`requirements.json` maps all 71 immutable acceptance IDs to implementation,
planned tests, evidence and remaining work. Generated `../implementation-status.md`
is a readable view. No row passes solely because a related unit test passed.
The release check fails while any mandatory row lacks its required evidence.


Latest development checkpoint: [identity-bound NoCloud seeds](nocloud-slice.md).
Its actual confined seed generation and synthetic recovery results remain separate
from unverified cloud-init execution, guest routing, native storage and hardware.
