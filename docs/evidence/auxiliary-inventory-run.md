# Authenticated auxiliary inventory integration

All 71 scenarios remain required and unaccepted. This change implements an
additional metadata inspection prerequisite for cold recovery, not capture or
restore. The previously installed disposable runtime remains `dc2ab1a` until a
separately recorded upgrade; local working-tree build output is not an installed
revision or a publication artifact.

The parent owns architecture, shared contracts, service/helper integration,
failure fixes, generated command reference, release tracking and all remote
execution. No agent used SSH or changed a host service or existing guest.

| Agent | Exclusive deliverable | Acceptance contribution | Integration result |
| --- | --- | --- | --- |
| Nash (`host_prefixes`) | swtpm generated lock probe and review; pinned libvirt lock-selection review; new CLI/TUI auxiliary tests and review | SNAP-01 / SEC-01 prerequisites; UX-01, UX-02, UX-03 | Seven generated swtpm cases executed by parent; 16 owned children reaped. CLI/TUI tests use the actual shared service with synthetic observers. Six top-level tests and 44 subtests passed under race. No native terminal, capture or boot claim. |
| Beauvoir (`pci_inventory`) | Linux metadata-only inventory executor, 116 leaf cases and review; later parent-source integration review | SNAP-01 / SEC-01 / SEC-03 prerequisites | Complete explicit set, policy/native/root/generation/metadata checks and zero-proof failures integrated. Two source findings in parent glue were fixed: late-cancel payload retention and receiver deadline relaxation. Common auxiliary package also compiled for Darwin arm64 and Windows amd64; not executed there. |
| Russell (`plugin_faults`) | Common authorization tests and review; typed private-socket transport tests and review | SEC-01 / SEC-03 / SNAP-01 prerequisites | Old-policy denial, exact authority, signature/revocation and aggregate bounds integrated. Transport tests reproduced a NUL-containing root accepted by the parent validator; the parent fixed canonical/control validation. All ten transport tests subsequently passed with required IPC and race detection. No installed-root authentication claim from the decoder seam. |

The common service requires an ordinary user and a persistent stopped system VM
without managed save or autostart. The helper independently authorizes current
policy and kernel peer, derives explicit state paths from native configuration,
performs repeated bounded metadata observations and checks held/path generations.
Source members use `O_PATH`; only directories are opened for enumeration. The
client refuses any state descriptor, checks the typed identity/binding/root/layout,
and returns no proof on failures. Capture/observe dispatch remains explicitly
unsupported; neither legacy ACL authority nor a capture flag activates it.

Parent evidence recorded against implementation-tree digest
`26e4f8410395a37606d577073d0bdff424610ae04b83df678e61d618ae38eb06`:

| Ledger ID | Actual result and limits |
| --- | --- |
| `auxiliary-inventory-core-001` | Full offline vendored Go race suite passed, with required private IPC, confined plugin conformance and generated-disk tools. 2,436 passing test/subtest records; zero failed records. Two opt-in tests skipped: disposable root ACL integration and host prefix smoke. Those skips are not passes. No existing VM or firmware/TPM content was used. |
| `auxiliary-inventory-vet-001` | Whole repository offline static analysis passed. |
| `auxiliary-swtpm-lock-probe-001` | Installed swtpm 0.10.2, ordinary UID 1000, seven generated local cases passed. Sixteen owned processes reaped and generated tree removed. Log contains exact swtpm/ioctl/libtpms binary hashes. No VM, TPM device, existing state, content read or hardware qualification. |

The lock tests prove cooperation requirements rather than a complete freeze.
OFD readers conflict with swtpm's POSIX writer lock and survive descriptor
duplication, while BSD flock does not exclude that writer. Lock-disabled or
incoming-migration processes can remain alive, an old lock inode can be bypassed
by replacement, and CMD_STOP can retain a process lock. Pinned libvirt v12.0.0
source selects `,lock` through a private swtpm feature bitmap; public domain XML
and capabilities do not attest effective locking. File-backend full source
review, restart exclusion and capture lease lifetime remain open.

The tests also exposed and corrected an aggregate byte-bound inconsistency in
the parent authorization code before its regression was run. No pre-fix failing
execution is claimed for that source finding or the two integration findings.
The NUL transport case has separately documented actual pre/post execution in
the [transport review](../reviews/auxiliary-transport-tests.md).

No dependency version changed. The pinned Go 1.27.1/vendored toolchain and recorded
Fedora environment remain the build inputs. The new policy fields are optional
and old signed request bytes retain their exact shape. This read has no persistent
schema change and no durable coordinator job. A nonce, stable metadata, sealed
object or helper send acknowledgement cannot stand in for captured complete
state, durable publication, recovery without originals or verified guest boot.

Next qualification uses a new, never-started disposable domain and newly
generated fixture bytes for the native helper path. Such a test can qualify
metadata/authentication behavior only; valid firmware/TPM state, complete cold
capture, restic, independent restore, hardware and all other mandatory acceptance
remain required. Existing guests, source media and actual auxiliary payloads
must remain untouched by that fixture.
