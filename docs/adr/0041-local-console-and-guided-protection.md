# ADR 0041: Local console access and guided protection

Status: implemented; native qualification is recorded separately.

The owner requests simple complete guest workflows across supported sources.
The existing unified browser remains the import entry point: metadata detection
covers OVA, ISO and supported QEMU disk formats; an opaque disk does not provide
reliable CPU/RAM/OS metadata. Advanced controls remain explicit and source media
is never modified. This decision does not narrow the 71 mandatory scenarios.

The shared service adds read-only `vm.console.show`. The official libvirt adapter
checks exact VM identity and inspects live configuration, returning named console
choices and reasons without endpoints, passwords or source XML. It refuses
ambiguous graphics, wildcard/remote listeners, SPICE monitor ports and opaque
emulator extensions that could bypass the observed display policy.

Both interfaces use one local console launcher. It repeats identity, running-state
and configuration checks, then invokes fixed arguments of trusted ordinary
`/usr/bin/virsh` or `/usr/bin/virt-viewer`. Virsh is used solely as an interactive
serial transport, never as a management API or parsed inventory. The user chooses
what to type; no guest command is generated or injected. Serial uses `--safe`,
without force or resume. The TUI releases and restores its terminal around the
child process. A console session is not a durable mutation job: guest lifecycle,
configuration, import and provisioning still use plan/authorize/execute/reconcile.
This follows the architecture's explicit host console-launch adapter boundary.

Graphical support initially requires verified virt-viewer 11.0 SPICE behavior.
Private temporary settings disable clipboard sharing; fixed flags disable audio,
USB redirection and resizing. No user preferences are overwritten. The child
receives a limited environment and no runtime tool download occurs. VNC discovery
is provided, but launch is refused: version 11.0's VNC server-cut-text callback
ignores the clipboard preference. This is an explicit remaining support gap.
A plain SSH session with no display gets desktop/serial guidance.

The external viewer reopens the selected UUID after the last observation; libvirt
does not provide an atomic configuration lease covering that handoff. Administrators
must coordinate concurrent changes. No universal all-writer isolation is claimed.
The SPICE control-port refusal prevents the known viewer Quit action from becoming
a VM power operation for supported observed configurations.

Protection now offers dedicated selected-capture backup and fresh-VM restore
forms. They emit existing shared requests using observed active directory pools
and password-file references. Preview/back retains values; no JSON document is
needed for these ordinary actions. Repository recovery from an external backup
receipt, firmware/TPM restore and other unimplemented paths remain explicitly
outside this slice's evidence, not removed from 1.0.

Guest tools keeps supported Linux detection and credential inputs on the basic
screen; desktop packages and custom SSH port are under Advanced. It explains
channel, verified-host-key and guest privilege prerequisites. No unsupported
Windows automation or new distro support is inferred from disk format.

Primary references: [libvirt domain devices](https://www.libvirt.org/formatdomain),
[virsh console](https://www.libvirt.org/manpages/virsh.html#console),
[virt-viewer options](https://gitlab.com/virt-viewer/virt-viewer/-/blob/v11.0/man/virt-viewer.pod),
[SPICE control ports](https://gitlab.com/virt-viewer/virt-viewer/-/blob/v11.0/src/virt-viewer-session-spice.c),
and [viewer clipboard and Quit behavior](https://gitlab.com/virt-viewer/virt-viewer/-/blob/v11.0/src/virt-viewer-app.c).
