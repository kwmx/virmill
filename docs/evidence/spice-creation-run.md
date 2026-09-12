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

Native product ISO preparation → creation → start → graphical console testing
remains pending. Full DEV-04 includes sharing and opt-in features beyond this
slice; all 71 acceptance scenarios remain required and statuses are unchanged.
