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
sudo is ready. The owner subsequently expanded root storage before the first
installation/test run. This inventory does not certify nested KVM, guest boot, firmware/TPM
or physical hardware behavior.

Required qualification targets remain Fedora + NetworkManager/firewalld/SELinux,
Ubuntu/Debian + Netplan/networkd/AppArmor, Ethernet, Wi-Fi and physical USB.
Exact additional distro versions are **unfrozen pending available test images**.

Required guest/source fixtures: Fedora Linux, Ubuntu/Debian cloud, generic Linux
ISO, VMware-origin multi-disk Linux OVA, non-VMware OVF/OVA, BSD/appliance, and
legitimately obtained Windows UEFI/TPM. Six owner-supplied samples now have recorded
hashes; one Windows OVA passed checksum inspection and one Kali QCOW2 passed
independent preparation/native volume readback. No guest image is certified.
Track architecture, machine/CPU, firmware, controllers, drivers,
provisioning transport and source digest separately for each future run.

The [first disposable-host run](evidence/disposable-first-run.md) adds actual
qemu-driver allocation/upload/refresh/readback for one independent Kali QCOW2.
Definition verification refused unreviewed native device defaults; the guest
remains powered off with a recovery-required operation. Its three locks survived
coordinator SIGKILL and explicit reconciliation without replay. This supplements
synthetic coordinator and native **test-driver XML** fixtures. General XML
normalization, file-label behavior, streams under interruption, firmware
initialization and guest boot remain unqualified.
Unsupported normalization fails reconciliation rather than certifying an altered
definition. See ADR 0006 and `tests/fixtures/creation/README.md` for the boundary.

The [device-policy run](evidence/device-policy-run.md) subsequently confirmed one
explicit Q35/BIOS Kali clone: full native volume verification and definition,
actual TUI authorization, KVM-enabled boot to the graphical login screen, and
graceful shutdown. It had no NICs, USB, UEFI or TPM. This is observed support for
that exact test, not certification of a guest family or completion of any entire
acceptance scenario. The earlier uncertain VM remains stopped and unresolved.

The [boot/media run](evidence/boot-media-run.md) adds actual TUI boot-order editing,
Kali ISO-menu boot, CLI ejection retaining the medium and drive, 3-CPU/3-GiB disk
boot, and native stale-plan/unknown-metadata checks on the same Q35/BIOS guest.
All guests ended stopped. This does not certify a complete installation, adoption,
live resource editing, Virmill console, firmware state or other hardware.
