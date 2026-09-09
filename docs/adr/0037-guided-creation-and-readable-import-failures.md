# ADR 0037: Guided VM creation and actionable import failures

Status: implemented software; native evidence recorded separately.

The locked specification requires editable detected hardware, explicit local mappings,
clear failures and a file explorer. The earlier preparation UI exposed raw byte
counts and stopped before VM setup; its plain JSON creation escape hatch did not
satisfy those requirements. That implementation gap does not narrow v1 scope.

Both interfaces now share read-only `vm.creation.options` and `import.sources`.
The backend observes KVM machine versions, CPU modes/models, vCPU capacity, physical
host RAM, disk buses, graphics and matching firmware descriptors. Observations are
not grants or guarantees: existing plan/apply checks remain authoritative. No
machine version, firmware path, pool or network is taken from a testing machine.

The TUI hands successful preparation to native hardware controls. Completed sources
can be selected again through Create VM after reconnecting. CPU and RAM come from
unambiguous source metadata; absent values have labeled suggestions, while invalid
or unknown values require entry. Original disk and NIC mappings remain explicit;
no source NIC is silently removed and no network is selected automatically. Firmware
requires an explicit choice. Creation still produces a powered-off VM, with starting
and guest-boot verification separate.

The current handoff uses two reviewed operations: image preparation, then creation.
A visible CPU/RAM/VM-settings action also allows hardware configuration before
preparation; selections are bound to the exact preparation request and retained
for the resulting source operation. Automatic persistence of incomplete wizard
edits across TUI restarts remains a gap against the complete specification. In-session
review/back and failed-submission recovery retain edits; reusable settings can be
exported privately. Completed source receipts and accepted operation plans remain
durable. This decision does not claim the complete creation acceptance scenario.

Resource normalization retains original quantity and allocation-unit strings.
Disk capacity without an allocation-unit attribute means bytes under
[DMTF DSP0243 2.1.0, section 9.1](https://www.dmtf.org/sites/default/files/standards/documents/DSP0243_2.1.0.pdf).
VirtualBox's legacy `MegaBytes` RAM spelling means binary MiB only for an explicitly
identified VirtualBox hardware section, matching its
[OVF reader](https://github.com/VirtualBox/virtualbox/blob/main/src/VBox/Main/xml/ovfreader.cpp).
Unknown units and overflow never become guessed capacities. Optional receipt fields
remain compatible with earlier prepared-import receipts.

Insufficient-space failures include required, available and missing GiB, exact byte
details for clients, and recovery guidance. A source-capacity limit cannot be reduced
below a known declared disk size to evade the check. The explorer creates a named
child folder only after explicit Enter, with 0700 permissions, no overwrite and held
nofollow directory descriptors. It begins at the user's home when no location is
selected; test-image directories are never product defaults.

Native correction: the libvirt firmware enum describes autoselection. The BIOS
choice also recognizes QEMU's default ROM on positively observed x86_64 KVM PC
machines with OS/ROM support; it never invents a firmware pathname. See the
[libvirt firmware capability documentation](https://libvirt.org/formatdomaincaps.html#guest-firmware).
Guest boot remains separately verified.
