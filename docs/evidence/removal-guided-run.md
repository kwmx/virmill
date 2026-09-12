# Guided retain-disks VM removal

Scope: CORE-02/CORE-05, JOB-02/JOB-03/JOB-04, SEC-05, UX-01/UX-02 partial evidence.
All 71 acceptance scenarios remain mandatory; no status is promoted by this slice.

`removal-guided-core-001` passed the app, native-adapter synthetic tests, shared
UI/CLI, TUI and operations packages with race checking. This covers request and
recipe binding, native-observation refusals, no deletion flags, exact-name preview,
fresh observations, catalog conflicts, last-moment state drift, lost receipts,
replacement domains and recovery without replay. These tests do not validate
hardware or real disk preservation. During integration, initial compilation found
two incorrect field/import references and a copied test field; these were fixed
before this passing recorded run.

`removal-guided-outcome-001` additionally checks the complete TUI package after
adding the removal completion card. Success describes retained files and does not
offer a shortcut to the removed VM.

The source uses existing pinned Go/libvirt/Cobra/Bubble Tea/SQLite dependencies;
no dependency changed. The approved disposable test VM is reachable and reports
passwordless sudo. Runtime package and native evidence are appended after execution.

Agents supplied the bounded backend adapter, confirmation form, service recovery
and ownership tests, independent integration review, and native fixture. Root owns
the service contract, ownership-check correction, integration, all remote mutations,
packaging and release tracking.

First native pass (`removal-guided-cycle-native-001`) failed safely at Preview:
libvirt rejects `DOMAIN_XML_SECURE` on a read-only connection. The new fixture
`f6aaab31-50aa-4c08-9235-0c9d687441f1` remained defined and stopped; no removal job
was submitted. Its disk SHA and unrelated guest/network/recovery/media metadata
were unchanged. The actual 80×24 form displayed the issue and full-issue action.
The adapter correction opens an ordinary writable libvirt connection for the
secure read; inspection still invokes no mutation. Exact absence remains read-only.

Package/integration check `removal-guided-packages-001` passed three tests,
including temporary private IPC and staged install/uninstall preservation.
`removal-guided-upgrade-native-001` and `removal-guided-restart-native-001`
installed 8a53288 and activated its coordinator with all 31 prior jobs,
VM inventory, journal tables and source-media metadata unchanged. The initial
native preview refusal above followed these successful deployment checks.


Corrected runtime `5b743b6dc6d5f7b975856519f3975a2101c3222f` passed
`removal-corrected-packages-001`, `removal-corrected-upgrade-native-001` and
`removal-corrected-restart-native-001`. These verified package/private-IPC checks,
actual core replacement and coordinator restart with 31 prior jobs preserved.

`removal-guided-cycle-native-002` passed using the exact retained fixture after
verifying the original failed report, definition, state and source bytes. No new
guest was created and no uncertain apply was retried. Actual 80×24 TUI navigation,
fresh exact-name confirmation, separate Preview, complete CLI/TUI plan parity,
Back retaining the name and cancel without mutation passed. Root inspected the
captured form and review screens.

- TUI plan: `caa67d0b-780c-4b3f-afd5-aad5855c2993`.
- Applied CLI plan: `040227a5-4373-49e1-ba96-e6f96135e3cb`.
- Successful durable job: `992fb03c-5056-4c0e-99cf-25140d364b4f`.
- Removed test definition: `f6aaab31-50aa-4c08-9235-0c9d687441f1`.
- Retained 393216-byte qcow2 SHA-256:
  `836ebf013b06c13d4e1457450cf42fb043a735ae77b817869613a9ffa9eb19f6`.

Native libvirt 12.0.0/QEMU 10.2.2 readback found the exact UUID absent. Disk bytes
matched before/after; all 18 existing guests, their native snapshots/checkpoints,
networks, recovery sets, 31 prior jobs and source-media metadata were unchanged.
There are now 32 jobs. The fixture was never booted. Its generated disk and source
remain retained. This verifies the BIOS/file-disk definition-only baseline, not
firmware/TPM retention, selected-disk deletion, guest boot or complete CORE-02.

Installed CLI SHA-256:
`468474e5dd935039cabc3773528e6fa9e8f0b84f0bdc7cbd14e17b351648c552`.
Installed coordinator:
`fbd7005d173b825ab6d4653acaa252e07f7a7a6b731847e0ca651463680fa88c`.
Core RPM:
`b0c41816d3fd0585a0199cf77ab6ecd06a95802dc7d07b8b52c656f060911f7e`.

Reports/screens remain in `~/virmill-tests/removal-5b743b6`; the initial failed run
is preserved under `~/virmill-tests/removal-8a53288`. Local unsigned RPM/DEB
packages/manifests are in `build/removal-corrected-delivery/`. Ledger source digests
include the fixture-only continuation added after the runtime freeze; installed
binary hashes bind the exact frozen runtime above. No publication occurred.
