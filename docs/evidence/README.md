# Release evidence ledger

`ledger.jsonl` is append-only. Entries record command, time, revision/source tree
digest, exact environment, fixture digests, result, evidence class and retained
log hashes. Failed/blocked checks remain visible. Tests against synthetic objects
do not certify actual virtualization, packet flow, firmware, TPM or USB.

The recorder's historical `sourceDigest` covers the implementation trees `cmd`,
`internal`, `sdk`, `schemas`, `contracts`, `scripts`, `tests`, `packaging` and the
root Go module files, excluding interpreter caches. It does **not** hash every
repository file: docs, examples and vendor sources are outside that digest.
New entries label this unchanged algorithm as `implementation-tree-v1`. Frozen
builds use an exact Git checkout; package file manifests and artifact digests bind
their additional inputs. A working-tree digest or embedded HEAD alone is not a
complete release source attestation.

New recorder calls validate every supplied acceptance ID against the preserved
71-row specification before running a command or writing evidence. Unknown or
duplicate IDs fail immediately. Historical attribution mistakes remain visible
with explicit corrections; the recorder never promotes an acceptance status.

When a command runs in an isolated source checkout, pass both `--cwd` and
`--source-root` to `scripts/record-evidence.py`. The latter binds the selected
checkout's revision and implementation digest while logs still go into this
repository's ledger. Its omission intentionally retains the historical recorder
root scope. Do not interpret an older `--cwd` value alone as changing that scope.
The auxiliary inventory record documents one such provenance correction without
overwriting the original log or digest.

`requirements.json` maps all 71 immutable acceptance IDs to implementation,
planned tests, evidence and remaining work. Generated `../implementation-status.md`
is a readable view. No row passes solely because a related unit test passed.
The release check fails while any mandatory row lacks its required evidence.


Latest native evidence: [firmware reconciliation and TPM/NVRAM persistence](cold-fixes-native-run.md).
A distinct 4 MiB firmware probe reported SEEDED then PRESERVED across two KVM
boots; the retained 2 MiB probe failed TCG2 lookup. CLI/TUI inspection continues
to report unresolved TPM storage. Complete capture and independent restore remain
unverified. Failed observer attempts and both firmware outcomes are retained.

Current integration: [durable creation declaration binding](nvram-binding-run.md),
with legacy compatibility, cancellation and immutable recovery-result checks.
The [authenticated auxiliary inventory](auxiliary-inventory-run.md) adds explicit
policy and filesystem member metadata through CLI/TUI; complete capture remains
unimplemented and unqualified.

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

Latest functional additions: [reboot, policy previews and TUI search](lifecycle-policy-search-run.md).
The copied-guest native reboot passed with a durable event receipt and preserved
source disk/prior definitions. The new copy required forced-stop cleanup; no
guest readiness claim follows. Package/private CLI/actual PTY checks and local
QEMU fixture allocation failures are recorded separately.

Managed network creation: [owned network run](owned-network-run.md). Native
definition/filter/activation results, packet-fixture failures, exact cleanup,
reproducible artifacts and remaining network qualification are recorded separately.

Protected profiles: [implementation and native results](protected-network-run.md)
and [artifact/fixture hashes](protected-network-artifacts.json). Services-only
NAT/lab and guest-only namespace packet suites passed after two recorded fixture
corrections. Strict active-zone preservation remains failed; no full network or
real-guest support claim is accepted from these scoped results.

EXT-02 simulated provider acceptance: [shared CLI/TUI conformance](provider-conformance-run.md).

Latest frozen integration: [CLI streaming, terminal forms and cross builds](interface-integration-run.md).
EXT-02 and REL-02 are accepted at their specified simulated/compile evidence levels.

Automatic IPv4 selection: [frozen integration and native evidence](automatic-allocation-run.md).
