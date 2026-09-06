# Support matrix — no certified configuration yet

Observed development host: Fedora 44 Workstation, Linux
7.1.13-200.fc44.x86_64. Native version evidence is in `evidence/environment.json`.
The default execution sandbox hides `/dev/kvm`. A later read-only outer-host probe confirmed it exists and UID 1000 has read/write access; it was not opened. No disposable host/VM target has been designated. Presence of host QEMU/libvirt tools
is not permission to modify the host and does not certify a KVM guest.

Required qualification targets remain Fedora + NetworkManager/firewalld/SELinux,
Ubuntu/Debian + Netplan/networkd/AppArmor, Ethernet, Wi-Fi and physical USB.
Exact additional distro versions are **unfrozen pending available test images**.

Required guest/source fixtures: Fedora Linux, Ubuntu/Debian cloud, generic Linux
ISO, VMware-origin multi-disk Linux OVA, non-VMware OVF/OVA, BSD/appliance, and
legitimately obtained Windows UEFI/TPM. No images were supplied, downloaded or
certified. Track architecture, machine/CPU, firmware, controllers, drivers,
provisioning transport and source digest separately for each future run.

