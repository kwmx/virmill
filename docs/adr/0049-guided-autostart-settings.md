# ADR 0049: Guided automatic startup settings

Status: implemented; scoped verification recorded separately.

The existing vm.autostart durable operation is mandatory under CORE-02, but its
TUI entry required an optional settings file to supply a required boolean. The
shared service also allowed malformed values to reach a durable job before the
native adapter rejected them. This conflicts with early validation and ordinary
TUI controls in specification 04; the preserved package remains unchanged.

VMs → More → Change VM automatic startup now performs a fresh inventory.get for
the selected exact resource before opening a toggle. Current and requested values
are separate. Preview is an explicit button; Back keeps edits and no-op requests
are explained locally. System versus user-session libvirt startup is distinguished.
No immediate guest power change is implied.

The shared service requires exactly one boolean enabled field before planning
and revalidates boolean/persistent-domain requirements before execution. Existing
same-value CLI requests remain idempotent. The frozen review now includes current
and requested autostart plus VM name; review and apply bind to the full observed
VM fingerprint, including autostart. No new operation version, journal migration,
privilege, native mutation API, or configuration reconstruction was introduced.
The native effect remains libvirt SetAutostart and readback remains GetAutostart.
Historical malformed recipes cannot be executed through validation.

Tests distinguish simulated service recovery from actual native toggle/readback.
Neither validates automatic guest boot on a host or session restart. Full CORE-02
and all 71 scenarios remain required; this decision does not shrink v1 scope.
