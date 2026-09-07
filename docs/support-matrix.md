# Support matrix — no certified configuration yet

Observed development host: Fedora 44 Workstation, Linux
7.1.13-200.fc44.x86_64. Native version evidence is in `evidence/environment.json`.
The default execution sandbox hides `/dev/kvm`. A later read-only outer-host probe
confirmed it exists and UID 1000 has read/write access; it was not opened. The local
development host is not authorized for host mutations.

On 2026-09-07 the owner designated a remote disposable Fedora 44 VM for Virmill
installation and guest tests. Read-only SSH inspection found KVM virtualization,
an exposed `/dev/kvm`, working libvirt inventory and the same installed versions
of libvirt 12.0.0, QEMU 10.2.2, edk2-ovmf 20260812, swtpm 0.10.2, bubblewrap 0.12.0
and xorriso 1.5.8. Exact package versions and connection details are retained in
ignored `.virmill-local/test-host.json`. Its root filesystem was full and sudo
required a password at intake; a subsequent read-only check confirmed passwordless
sudo is ready. This inventory does not certify nested KVM, guest boot, firmware/TPM
or physical hardware behavior.

Required qualification targets remain Fedora + NetworkManager/firewalld/SELinux,
Ubuntu/Debian + Netplan/networkd/AppArmor, Ethernet, Wi-Fi and physical USB.
Exact additional distro versions are **unfrozen pending available test images**.

Required guest/source fixtures: Fedora Linux, Ubuntu/Debian cloud, generic Linux
ISO, VMware-origin multi-disk Linux OVA, non-VMware OVF/OVA, BSD/appliance, and
legitimately obtained Windows UEFI/TPM. One owner-supplied OVA was found on the
remote VM; its content and digest are not yet verified. No guest image is certified.
Track architecture, machine/CPU, firmware, controllers, drivers,
provisioning transport and source digest separately for each future run.

The creation adapter has synthetic coordinator and native **test-driver XML**
evidence, plus read-only installed QEMU device-help metadata. Native qemu-driver
volume allocation/upload/refresh/readback, XML normalization, file labels, streams
under interruption, firmware initialization and guest boot are not verified.
Unsupported normalization fails reconciliation rather than certifying an altered
definition. See ADR 0006 and `tests/fixtures/creation/README.md` for the boundary.
