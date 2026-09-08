# Reboot, policy preview and TUI search

All 71 acceptance scenarios remain required. This work adds functioning commands
and navigation through the existing shared service and durable operation engine.

| Owner | Delivered implementation | Acceptance mapping |
| --- | --- | --- |
| Beauvoir | Native selected-domain event reboot adapter; eight focused race-tested cases | CORE-02, JOB-01 |
| Russell | BackupPolicy semantic validation and bounded timezone/DST occurrence preview; ten focused race-tested functions | BAK-05, UX-01, UX-03 |
| Nash | TUI action search, remembered selection, Unicode and compact paging; 12 new tests plus six Unicode subtests | UX-02 |
| Parent | Shared contracts/service, durable reboot receipt/recovery, CLI/TUI parity, bounded declaration reads, integration and release evidence | CORE-02, BAK-05, JOB-01, UX-01, UX-03, REL-03 |

The parent reviewed each contribution and executed the shared service/UI tests.
Reboot tests include actual SQLite reopen and receipt recovery, duplicate request
suppression, stale plans, missing acknowledgement, invalid or lost event receipts
and retained uncertainty locks. Provider seams in these tests do not exercise a
real guest. Review corrected the public plan idempotency enum and added a final
reconciliation cancellation check. An initial test fixture omitted the required
private journal directory mode; it was corrected before the passing runs.

`lifecycle-policy-search-core-001` ran the full offline race suite with required
private IPC, confined plugins and generated disk tooling. The new feature tests
passed, but the suite failed two QEMU fixture paths with `io_uring` memory
allocation errors. A resource-limited rerun, `lifecycle-policy-search-core-002`,
also failed generated QEMU paths. These are retained failures, not passing full
suite evidence. `lifecycle-policy-search-vet-001` passed whole-repository static
analysis. New code did not change the image adapters; the local native fixture
environment remains an unresolved qualification limitation.

The native reboot recipe uses a new disk copy and isolated coordinator on the
authorized disposable host. Its actual execution is recorded below when done.
Persistent backup scheduling/capture, native reboot qualification and the full
terminal/lifecycle matrices are not inferred from component tests.

The frozen source is `1c541750870768b43496dec33f5c04441374ae0b`, with 2,149
tracked inputs verified unchanged after both builds and all package checks.
`lifecycle-policy-search-repro-001` passed two same-environment offline builds:
three binaries and four unsigned development RPM/DEB packages were identical.
The CLI reference and completions were regenerated before freezing this source.

`lifecycle-policy-search-artifacts-001` passed actual archive metadata, staged
install/uninstall preservation and private-daemon CLI integration. It additionally
executed the new policy validation and preview commands and an actual 80x24 PTY
search flow: section selection, filter entry, select-only Enter, clearing and
no-results feedback. No TUI mutation was submitted. This run deliberately omitted
the optional generated QEMU paths; their preceding failures remain unresolved
local environment evidence. The externally hashed integration fixture is newer
than the frozen binary source, explicitly recorded in its ledger entry.

`lifecycle-policy-search-documents-001` passed eight stage/observer tests: 206
payloads, 117 Markdown files, 177 resolved local links, zero missing targets and
exact essential plugin protocol bytes. `lifecycle-policy-search-cross-001` passed
the declared common-package/SDK/sample-plugin cross-compilation targets.

`graceful-reboot-native-001` **passed** on a new independent copy of the stopped
Kali fixture disk. The ordinary-user isolated coordinator started the new guest,
applied its reviewed graceful reboot, observed the selected-domain event and
persisted the operation-bound receipt. The request completed in roughly 1.2
seconds; guest readiness remains false. The parent independently checked all
119 retained command records and all nine preexisting XML hashes. Source disk
SHA-256 remained exact. See the [native report](environments/graceful-reboot-1c54175.json)
and [preservation digest record](environments/graceful-reboot-preservation-1c54175.json).

Cleanup requested graceful shutdown, then force-stopped only the newly copied
disposable UUID after its 60-second timeout. That guest, disk and isolated journal
remain retained; the guest and its test coordinator are stopped. Existing system
packages remain at the previous 490b88c installation. This is one native reboot
transition, not qualification of graceful stop, guest services, the complete
lifecycle matrix or any complete acceptance scenario.

`generated-disk-remote-001` passed the generated-image and cold-file native test
programs on the disposable host. Both race-instrumented test executables were
compiled from frozen 1c54175 and their remote hashes matched the recorded local
hashes. The two programs cover real confined format conversion, missing/escaping
extents, backing chains, metadata, empty images and QEMU write-lock lifetime.
They create their own temporary files, never read supplied media or mutate guests,
and provide software/file integration evidence. This resolves execution of those
checks in the separate environment; the two local whole-suite failures remain
failed and do not become a passing same-environment suite.

The release gate `lifecycle-policy-search-release-gate-001` failed as expected:
all 71 complete acceptance scenarios and the ship checklist remain open. The
tracker now has 52 scenarios in progress and 19 not implemented, with no accepted,
hardware-qualified or released full scenario. Current workspace binaries,
RPM/DEB artifacts and staging files match the frozen 1c54175 hashes/modes; the
previous installed system packages and historical deployment record are retained.
