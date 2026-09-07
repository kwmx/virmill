# Release evidence ledger

`ledger.jsonl` is append-only. Entries record command, time, revision/source tree
digest, exact environment, fixture digests, result, evidence class and retained
log hashes. Failed/blocked checks remain visible. Tests against synthetic objects
do not certify actual virtualization, packet flow, firmware, TPM or USB.

`requirements.json` maps all 71 immutable acceptance IDs to implementation,
planned tests, evidence and remaining work. Generated `../implementation-status.md`
is a readable view. No row passes solely because a related unit test passed.
The release check fails while any mandatory row lacks its required evidence.


Latest development checkpoint: [ISO preparation and read-only media](installation-slice.md).
Its generated-file and simulated XML results remain separate from unverified native
storage, installer execution and hardware behavior.
