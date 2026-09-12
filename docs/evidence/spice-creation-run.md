# Private SPICE creation and graphical console integration

New creation forms suggest the host-advertised `spice-unix` profile. Explicit
existing/resumed VNC/none choices remain intact. CLI schema and backend preflight
require supported SPICE plus explicit device policy. The generated private socket
disables clipboard/file transfer and does not add desktop agents, audio, USB
redirection or network exposure. Basic and advanced TUI pages explain the desktop
requirement; plan review names the selected display.

The same exact private SPICE shape is recognized as configuration-only during
cold-source inspection, avoiding an unsupported dependency solely because of the
new display default. Host socket paths, credentials, unknown features and enabled
sharing are still refused; complete source XML remains retained. This is not a
new full backup/restore qualification.

`spice-normalization-native-001` passed on the authorized Fedora 44 VM with
libvirt 12.0.0/QEMU 10.2.2. Fresh diskless/networkless stopped fixture
`3299058c-ffe8-48d6-911f-977730743d71` was defined, observed and undefined. The
exact requested graphics shape remained unchanged. No guest was started and the
prior guest UUID inventory was preserved. This is native adapter feasibility,
not product creation or graphical viewer evidence.

`spice-creation-core-001` passed the integrated Go race suite; vet also passed.
Tests cover advertisement and fresh capability refusal, exact private XML,
legacy declarations, unknown normalization/security attributes, schema policy,
new defaults versus resumed choices, compact form navigation, summary rendering
and cold-source dependency classification. Cached unchanged packages and explicit
opt-in limitations in the suite are not new native support claims.

An independent agent reviewed root schema/service/cold-source integration without
finding an actionable issue. Other agents implemented the backend, TUI choices
and a guarded product creation/viewer recipe. Root retains integration, source
freezing, remote mutation coordination and acceptance tracking.

Native product ISO preparation → creation → start and a separate bounded
graphical viewer recheck are recorded below. Full DEV-04 includes sharing and opt-in features beyond this
slice; all 71 acceptance scenarios remain required and statuses are unchanged.

## First installed product run

Installed source `a1d6cb66b9e93dc653500837d08669617b7cd762`, implementation-tree
digest `66ef8344e6ff90a41bf8b2f7ac330d6be592c422f6b6a19f82e1e0832eda0d2f`.
Package integration, compatible core upgrade and idle coordinator restart passed
(`spice-creation-packages-001`, `spice-creation-upgrade-001`,
`spice-creation-restart-001`). All 17 prior jobs and their journal tables, guest
inventory and source-media records were preserved; new user coordinator PID 87668.

`spice-creation-native-001` remains a **failed** overall fixture. Product ISO
preparation, VM creation, start, 80×24 TUI console selection and the generated BIOS
serial boot marker passed. Creation operation `a9359cac-f831-4a95-8c95-271c5789064e`
produced guest `d9f2f4d6-19da-43b6-b17e-a105b9b8a804`
(`virmill-spice-8c6fbc43309d`). The generated ISO SHA-256 is
`7e3b0b119357789f99fef74a86a499a640e1f0d749083e3587bb4ad5a0ac5979`.
The fixture stopped and retained this guest and its managed volumes. Existing
guests/jobs and supplied media were preserved.

Native diagnostics showed SPICE main/display/input/cursor channels over Unix
sockets and the correct virt-viewer window, but the fixture searched only the
main thread's direct-child list and did not identify the viewer process. The
complete viewer-close check therefore did not run. This is a fixture observation
failure, not passing end-to-end evidence. A bounded recheck will scan all process
threads, bind the same successful creation receipt and source, and use the same
owned stopped guest without replaying preparation or creation.

## Same-guest viewer recheck — passed

`spice-viewer-recheck-native-001` passed against the unchanged installed
`a1d6cb6` CLI/coordinator. Only the Python fixture was corrected; its current
implementation-tree digest is `f13361cc3124534dbe41d732fad2bd61f21abba94f7b804415028d89b52da053`.
Four pure fixture tests passed (`spice-fixture-unit-002`). The original staged
script and failed report were preserved; the corrected script was staged as
`spice_creation_recheck.py`.

The recheck bound the prior successful creation receipt, original source SHA,
installed binary hashes and exact stopped VM identity. It ran new reviewed start
`2f66412c-43ee-4018-8564-0076111ed1eb`, found virt-viewer across the Go process's
threads, and verified real SPICE main/display channels over Unix sockets plus the
correct viewer window. The 1024×768 capture was inspected: it shows the same BIOS
marker after boot from DVD/CD. PNG SHA-256:
`59d3a4d178a6ffe927bc5c8e9ed436455d784db88c4e36233639dcc7abe9c871`.
The viewer exited cleanly and the VM remained running. Reviewed hard stop
`c527d8f9-fab9-45e5-a07b-445aaaea7b76` then returned the owned no-OS fixture to
stopped state. All guest inventory/configuration returned to baseline; existing
jobs, supplied media, generated ISO and the original failed report were preserved.
The stopped guest and its receipt-bound managed disk/media are retained.

This proves actual generated ISO boot and private graphical access on a virtual
desktop using virt-viewer 11.0-18.fc44, Xvfb 21.1.24-1.fc44, libvirt
12.0.0-3.fc44, QEMU 10.2.2-1.fc44 and SPICE server 0.16.0. It does not certify a
physical desktop, arbitrary OS installation, desktop integration opt-ins, VNC
launch or all DEV-04 behavior. Creation submission was through CLI; native TUI
console discovery/selection and deterministic creation-form tests are separately
identified. No release acceptance status was promoted and nothing was published.

| Installed artifact | SHA-256 |
| --- | --- |
| Core RPM | `cdd646c04810b5e2c35c54fe204de5f72890fd36528cada511673476aabaa4b2` |
| `/usr/bin/virmill` | `62794f450534dc531719f6a728672ce2b1aa4d36a03719969914f71cf7565194` |
| `/usr/bin/virmilld` | `e1e6a70d677ad9628a32cf5a0d238939cee8fc71d1e8bcb3b426ce441312a79b` |

Private reports/captures are retained under
`~/virmill-tests/spice-creation-a1d6cb6/{spice-creation,spice-viewer-recheck}` and
ignored local `build/spice-creation-delivery`. The native fixture preservation
checks and record digests are in the append-only ledger.
