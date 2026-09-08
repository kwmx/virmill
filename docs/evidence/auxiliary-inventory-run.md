# Authenticated auxiliary inventory integration

All 71 scenarios remain required and unaccepted. This change implements an
additional metadata inspection prerequisite for cold recovery, not capture or
restore. Frozen development revision `490b88c` is now installed on the disposable
VM after a separately recorded RPM upgrade. The actual coordinator is PID 30897
and its running executable matches the deployment. These unsigned development
packages are not publication artifacts or a qualified 1.0 release.

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

The integrated source was committed as
`490b88cba5bf6e0837e2cb5780e56211c22a7d27`. An isolated checkout has 2,031 tracked
regular inputs, enumerated with clean tracked state before and after. Its complete
[source inventory](environments/source-490b88c.json) has SHA-256
`f0585986ff6693a306a54e86d48db9eecbed5cddca83941d461d3e2bb2727576`.

| Frozen artifact ledger ID | Actual result and limits |
| --- | --- |
| `auxiliary-inventory-repro-001` | Two offline builds of the fixed checkout produced identical three binaries and four RPM/DEB packages. Unsigned development artifacts, not a published release. The historical recorder hashed the parent tree while another native fixture was being added: its `sourceDigest` `934dbf6735ecdbbba7e5e697e4ef6f45537911985a1f296e7572a6b6fbec169d` is that parent snapshot, **not the compiled checkout**. The executed `cwd`, fixed revision, complete source inventory above and artifact hashes identify the actual build. Original evidence is retained. |
| `auxiliary-inventory-packages-001` | Three actual package archive/staged install/private CLI-daemon workflow tests passed against that frozen output. This record explicitly binds `sourceRoot` to the checkout and digest `26e4f8410395a37606d577073d0bdff424610ae04b83df678e61d618ae38eb06`; fixture hashes bind the complete source and both install manifests. No native host storage effect or guest boot. |
| `auxiliary-inventory-cross-001` | Frozen common packages, Go SDK and example compiled for Darwin arm64 and Windows amd64. Not executed on those operating systems. |
| `evidence-source-root-regression-001` | Four generated-repository tests passed for explicit checkout/revision selection, independence from parent edits, changed selected inputs, legacy scope, invalid-source refusal and immutable evidence IDs. This corrects future recording and does not rewrite historical records. |

The recorder now accepts `--source-root` independently of `--cwd`, with default
behavior preserved. A nested Go directory could initially inherit its parent Git
revision; the independent review reproduced that gap. The expanded six-test run
`evidence-source-root-regression-002` failed before correction, and
`evidence-source-root-regression-003` passed after requiring the exact resolved
Git checkout root. Real worktrees with a `.git` file remain supported. Both
records are retained. See the [provenance review](../reviews/evidence-source-root-review.md)
for remaining command/input and historical digest limits. Package qualification used the explicit frozen checkout;
later recorder/fixture development is not represented as installed runtime code.

The lock tests prove cooperation requirements rather than a complete freeze.
OFD readers conflict with swtpm's POSIX writer lock and survive descriptor
duplication, while BSD flock does not exclude that writer. Lock-disabled or
incoming-migration processes can remain alive, an old lock inode can be bypassed
by replacement, and CMD_STOP can retain a process lock. Pinned libvirt v12.0.0
source selects `,lock` through a private swtpm feature bitmap; public domain XML
and capabilities do not attest effective locking. The separate FILE-backend source review and generated native experiment are now
recorded below. Restart exclusion and capture lease lifetime remain open.

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

Native deployment and qualification are recorded separately:

| Ledger ID | Actual result and limits |
| --- | --- |
| `auxiliary-inventory-upgrade-fixture-001` | Eleven pure recipe self-tests passed. No installation inferred from authoring. |
| `auxiliary-inventory-upgrade-native-001` | Two RPMs upgraded on the authorized disposable VM; all three installed binary hashes and actual coordinator PID 30897 matched frozen 490b88c. All 301 journal rows/schema, seven stopped guest XML definitions, source-media metadata and original helper policy/key access metadata were preserved. |
| `auxiliary-helper-activation-native-001` | Existing installed helper/socket activated for the scoped test; actual root helper PID 31125 executable matched the frozen helper SHA256. |
| `auxiliary-inspection-native-fixture-001` | Thirteen pure authored-recipe self-tests passed. Native behavior was still unverified. |
| `auxiliary-inspection-native-001` | **Failed in preflight.** Libvirt refused `domcapabilities` on a read-only connection. No new guest/state or policy replacement occurred. Seven stopped definitions, all journal rows, direct media metadata/selected hashes and supplied-media metadata passed preservation. Final exact volume-XML comparison failed: three old QCOW2 access timestamps changed; saved before/after XML had no other difference. The failed record remains unchanged. |
| `auxiliary-inspection-native-fixture-002` | Fifteen pure tests passed for the separately numbered successor, including fixed capability-query argv and strict access-time-only volume comparison. |
| `auxiliary-inspection-native-002` | **Failed after fixture definition.** The expected original-policy denial returned exit 4 and typed `PERMISSION_DENIED` stdout, but the recipe incorrectly required empty stderr. The executable also prints the matching human-readable error summary. The original policy was never replaced, eight stopped definitions (seven earlier plus the new fixture), all journal rows/schema and media/helper/pool preservation checks passed within documented atime/counter limits. No auxiliary success, state-content read, guest start or capture occurred. |

The retained recipe-002 guest is `50a0583b-3587-495f-90f4-9a42b8ad2fa3`, named
`virmill-auxiliary-fixture-002`. Its exact inactive XML SHA256 is
`fa9966bbb12d425546b444f8094a5a5d046fa2042720a76a6975e810634ecd0c`.
The generated root `/var/lib/virmill-host-helper/auxiliary-fixture-002` contains
dummy code, NVRAM and TPM bytes; they are not valid initialized state. The guest
has no disks or NICs and has never been started. Failed run artifacts and this
new scope are retained, without overwriting earlier evidence or removing guests.

Valid firmware/TPM state, complete cold capture, restic, independent restore,
hardware and all other mandatory acceptance remain required. The native metadata
fixture can qualify only the exact tested observation/authentication behavior.


The corrected and independently reviewed `auxiliary-inspection-native-003`
**passed** all six stages (322 recorded bounded commands) on the installed frozen
build. JSON and NDJSON returned exactly three metadata members totaling 4,128
bytes, with the empty TPM `.lock` separate. Original-policy and root-only-policy
denial, the exact FIFO type refusal, unresolved-source refusal without a guessed
path, restoration of the new explicit XML and final original-policy denial all
passed. No state payload was read or emitted. All 301 journal rows and schema,
eight earlier stopped XML definitions, declared media metadata/selected hashes,
supplied media metadata and prior helper artifacts were preserved. The ninth
new guest `074d9083-1953-4941-b006-9e301bb6d907` remains never started, with XML
SHA256 `7630c6b47171115d4825fac5918749ab805f6272144d16002de6b8b6a2184908`
and dummy state under `/var/lib/virmill-host-helper/auxiliary-fixture-003`.
Original policy bytes/access metadata were restored; its SHA256 remains
`1ec86a37a44cac12e7fa7c16c542ec83e552e348ea2f2cec841597dc999d6ed4`.
The exact native report SHA256 is
`4447e177dd0df0aabfbda022495a0f081c1a23ec5d747c5458105d8207377d8c`.

The [fixture review](../reviews/auxiliary-native-fixture-review.md) identified a
pre-redefine check missing from the authored fixture and a generic FIFO error
assertion that could mask a different refusal. Both were corrected before those
native stages ran. Sixteen pure fixture tests passed. No real concurrent XML
edit was injected, no libvirt compare-and-swap was claimed, and the first two
failed native attempts remain failed. The separate
`auxiliary-native-failure-review-001` binds their exact error records and saved
metadata differences without changing their outcomes.

The swtpm FILE-backend experiment also has retained pre/post evidence:

| Ledger ID | Actual result and limits |
| --- | --- |
| `swtpm-file-lock-self-tests-001` | Twenty-two pure authored-probe tests passed; process, socket and kernel-lock execution were blocked in the self-test seams. |
| `swtpm-file-lock-native-001` | Failed before starting any child because the parent transcribed one dependency-hash digit incorrectly. Scratch was removed. |
| `swtpm-file-lock-native-002` | Failed because the exact refusal parser omitted the installed tool's `swtpm: ` prefix. Three owned children were reaped; scratch was removed. Its original source and report remain unchanged. |
| `swtpm-file-lock-self-tests-002` | Twenty-three pure successor tests passed, including exact replay of that recorded diagnostic and rejection of changed prefix/path/errno text. |
| `swtpm-file-lock-native-003` | All four generated-state FILE cases passed using pinned swtpm 0.10.2 and both recorded library hashes. Nine owned children were reaped and scratch removed. No VM, existing TPM/NVRAM, host service or hardware was used. |

The native FILE observations confirm why a cooperating lock is insufficient by
itself. A same-inode initialized-file writer was refused with tested metadata
unchanged, but an explicit-mode start changed 0600 to 0640 before lock refusal,
and a generated empty file grew to 2,240 bytes while the OFD read guard was held.
Replacing the guarded pathname with a different generated inode admitted a new
producer while the old guard remained. Python read no state bytes. These are
actual generated-file effects, not a claim that Virmill can safely capture a
real guest. See the [pinned source review](../reviews/swtpm-file-writer-exclusion.md)
and the immutable [successor probe](../../tests/fixtures/protection/swtpm-file-lock-probe-v2.py).

The separate `auxiliary-inspection-tui-denial-001` **passed** on the same installed
runtime in an actual 80×24 PTY. Three current-screen pages, including page-down
and page-up navigation, reconstructed the exact shared error envelope. CLI JSON,
NDJSON and table outputs agreed; no data or capture claim appeared. All nine
stopped XML definitions, all journal rows/schema, policy bytes/access metadata,
pools, ordinary media metadata and prior files stayed unchanged under the
observer's documented limits. Historical selected disk hashes were retained but
not freshly rehashed, and the ordinary observer did not rehash the root helper's
process executable. Its 13 pure authoring tests are separately recorded in
`auxiliary-inspection-tui-fixture-001`.

The parent-reviewed positive policy window **passed** in
`auxiliary-inspection-tui-positive-001`. Its 13 pure tests cover uncertain grant
and restore replies, concurrent policy changes, exact scope, durable intent and
separate TUI/restoration results. Actual positive observation reconstructed 17
current 80×24 pages and matched CLI JSON/NDJSON/table and native003 metadata,
with fresh typed read correlation. The observer used 142 bounded commands; its
outer policy wrapper used 106. The exact saved metadata-only grant had SHA256
`8f390c2cd5726afa3c5d97d800377eed4f3446bb83e85634209d6d9025301f08`.

The [actual TUI report](environments/auxiliary-tui-positive-490b88c.json) has
SHA256 `e211e6c53c1afe8c2d15699d63fbbff15eb85c89cafc4b238be64646deffd297`.
The separate [policy-window report](environments/auxiliary-tui-policy-window-490b88c.json)
has SHA256 `6797ebef8cece52582d4b170c1b6d67c1e4c8a64e6d244959a9ecf136a37b599`.
Both passed. Original public policy bytes, owner/mode, xattrs and mtime were
restored and verified against the last known generation. No uncertain reply or
concurrent policy edit was injected in the native run; those are pure tests.
All nine stopped XMLs, all 301 journal rows/schema, generated fixture and prior
helper metadata were preserved. The TUI also checked pool/media/file metadata
within its documented limits. The outer wrapper freshly rehashed both actual
running executables before and after, supplementing the ordinary observer's
explicitly historical root-helper executable check. No guest or auxiliary state
payload was read; capture, independent restore and boot remain unverified.

The agents' subsequent exclusive work was integrated as follows:

| Agent | Deliverable and ownership | Observed result |
| --- | --- | --- |
| Nash | Immutable `auxiliary-inspection-tui-001.py` and its recipe; SNAP-01/SEC-01/UX-01/UX-03 prerequisites | 13 pure tests passed. Parent executed actual denial (3 pages) and positive (17 pages), both passed. |
| Beauvoir | Immutable FILE-backend v2 probe and notes; SNAP-01/SEC-03 prerequisites; implicit TPM-source review | 23 pure tests passed. Parent executed all four generated FILE cases successfully; adverse effects keep capture blocked. Source-derived implicit-path findings are not native path qualification. |
| Russell | Immutable positive policy wrapper and notes; SEC-01/SNAP-01/UX-01/UX-03 prerequisites; independent fixture/provenance review | 13 pure tests passed. Parent executed positive TUI and exact conditional policy restoration successfully. No agent used SSH or performed a host mutation. |

`auxiliary-helper-deactivation-native-001` subsequently **passed**. The parent
stopped only the exact helper process/socket it had activated, after independently
reviewed PID, start-time, executable and restored-policy checks and durable
intent. Both units were inactive with helper PID zero on immediate and final
readback. All nine stopped guest XMLs, 301 journal rows/schema, prior helper and
generated fixture metadata and policy generation remained unchanged. Coordinator
PID 30897 remained active with its exact executable and original transient
runtime deadline. No guest, pool, source media or state file was removed.
The comparison is not atomic against external systemd writers; no such writer
was introduced. This qualifies the recorded restoration, not general uninstall.
