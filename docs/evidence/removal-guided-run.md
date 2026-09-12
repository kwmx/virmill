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
