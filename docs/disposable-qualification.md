# Disposable qualification plan — not authorized or executed

Before any destructive integration run, the owner must designate the disposable
host and storage roots, explicit libvirt connection, VM/network ownership marker,
physical devices and independent console. No such target is currently authorized.
Tests must positively match those declarations and fail closed before mutation.

The planned real-system matrix is retained in the 71-row requirements tracker.
Gate 1 needs actual system/session discovery and events; adopted XML preservation;
VMware and generic multi-disk/multi-NIC import; USB serial/port replug and mounted
storage refusal; Ethernet/Wi-Fi, NetworkManager and Netplan checkpoint rollback;
and cold multi-disk UEFI/TPM recovery. Record native/guest versions, CPU, firmware,
source hashes, exact command, expected predicate, observed result and preserved logs.

For destructive restore drills, generated/owned fixtures must be independently
recoverable before the original fixture artifacts are deliberately removed. The
owner reviews those exact resources. Never delete a discovered VM or disk merely
because its display name resembles a fixture. Never label sequential disk copies
or live swtpm-file copies a complete capture.

No boot media is bundled. Legitimate guest media and exact tested versions must be
supplied or explicitly approved for retrieval. The proprietary Windows fixture is
built locally and not redistributed. The synthetic OVA unit fixture contains text
payloads; it tests packaging/metadata only and cannot boot.

An unavailable host can block hardware evidence, but it cannot excuse the remaining
unimplemented workflows. Continue code, fixture and simulated tests independently.
