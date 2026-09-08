# 0031: Resource workspace and visible action controls

Accepted 2026-09-08 following the owner's rejection of the command-style beta
TUI and explicit requirement for a default full-screen application with action
buttons and access to the complete implemented command set.

The default `virmill` terminal entry opens a resource workspace with observed VM,
pool and job information, searchable tables, details, focusable buttons and
labeled forms. The full catalog is derived directly from `ui.Actions`; no
second service registry or virtualization logic is introduced. Common operations
have typed forms. More complex existing mappings use an explicit parameters-file
field until their complete guided wizard is implemented. The old Model remains
an internal regression harness, not the runtime default or a user command prompt.

The normative TUI contract requires explicit review and labeled confirmation of
exact resources; it does not require typing a hash. The new UI verifies the
returned plan digest, displays complete plan fields, collects each acknowledgement
explicitly, and offers a selected Apply button. It sends the same immutable ID,
digest and acknowledgements to the existing operation engine. Hidden help and
undersized windows cannot submit. Concurrent/stale replies cannot revive canceled
dialogs or unlock a second apply. CLI authorization is unchanged.

All backend actions and missing full-v1 workflows remain in the requirements
tracker. Action catalog reachability does not qualify a full import/creation,
multi-NIC, device or lab wizard. Native terminal tests use the authorized test VM
and preserve guest state; component tests do not certify hardware behavior.
