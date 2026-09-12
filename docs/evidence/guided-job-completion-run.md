# Guided job completion and guest-tools TUI

This UI-only slice connects verified completed VM jobs to fresh VM details,
checks guest state before SSH setup fields, and keeps prose words readable.
CLI/shared services and durable recipes are unchanged. See ADR 0047.

`guided-job-core-001` passed the integrated Go race suite with required IPC
fixtures enabled. Unchanged cached packages are regression evidence, not fresh
native qualifications. Vet passed. Independent review found and corrected late
navigation replies and keyboard focus shifts while result buttons arrive.
`guided-job-ui-002` records the final TUI race regression run after those fixes.
`guided-job-fixture-unit-001` passed three pure tests for the native fixture's
review/result predicates; these do not certify guest behavior.

The native fixture is scoped to the retained owned Fedora guest. It verifies an
idempotent repeat through the actual 80×24 TUI, then opens its exact VM from the
completed job. It must preserve original media, guests, jobs, network state and
credential references; only its own reviewed start/graceful stop are allowed.
The prior successful package installation remains separate evidence. Other guest
profiles and full 1.0 qualification remain open; all 71 scenarios are required.

## Installed build

Source `11721e6e59a21112ec26f737461cf497d20c5cf3`, implementation-tree digest
`98dd1774a04046a4af15ffb97c37bb500d6dc52794dc71263bb54a655a3cb8f5`.
Final native-fixture revision passed four pure predicate tests
(`guided-job-fixture-unit-002`). `guided-job-packages-001` passed archive and
staged install/uninstall checks but skipped the explicit IPC opt-in;
`guided-job-packages-ipc-001` enabled it and passed all three package checks.

`guided-job-upgrade-001` and `guided-job-restart-001` passed on the authorized
Fedora 44 VM. The new ordinary-user coordinator PID is 89933. All 23 prior jobs,
their journal tables, guest inventory and supplied media records were preserved.

Installed SHA-256:

- CLI: `ead5f881dede473b9eac72161ab0ed90bf65911b931796bdfa88c5f1e32cb02e`
- Coordinator: `9c116505b2773e5a5f9c6cd9a37a0931527e15516e4400b92cabc8b6c5a80d0b`
- Core RPM: `9c5e4482c10e57099c3665dba1320d498ce63a3aaff3fdf976852c1827745fff`

Unsigned beta packages remain local; nothing was published. The helper package
was built and inspected locally but the remote helper was not replaced.

## Actual guest-tools TUI — passed

`guest-tools-tui-native-001` passed against the installed binaries above. The
80×24 TUI first opened stopped guest `71001e0c-e99d-4e26-a886-0553533c5fa0`
(`virmill-tools-71001e0c`), explained its state and offered Preview start VM
without showing premature SSH fields. Viewing this screen changed no guest or job.

Reviewed start `44a672c8-38d6-4152-9968-a1fd523cc71e` booted only that fixture.
Actual TUI field entry, full plan review, three acknowledgements and Apply created
`f2a05fe5-1cb3-4375-95cf-992da8c076f9`. It completed readiness, already-configured,
skip-apply and verified stages. No package installation was repeated. Native
QEMU guest-agent ping passed. The TUI showed Guest setup completed and Open VM;
selecting it returned to the exact owned Fedora guest UUID and fresh resources.
Reviewed graceful stop `13b21f45-4ae2-43df-b182-b72554bc6704` restored its stopped
state. All guests, prior jobs, source media, network state and credential file
references were preserved. Four predicate tests are separate from this native run.

Root inspected the recorded stopped, installation, review, completion and VM
screens. The completed result and state guidance were correct; review warnings
still used a separate hard-wrap formatter, splitting words despite the general
prose improvement. The formatter now uses the same word-aware wrapping. `guided-review-ui-001`
passed the complete TUI race suite after this correction; vet passed. Tests cover
actual guest recipe warnings across 80×24 review pages and full identity/digest
retention. Native recheck of the packaged correction follows separately.
