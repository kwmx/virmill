# Multi-disk import and active upload crash — 2026-09-07

Installed development revision **12d7bba1e5e958324bf6199041359e344c153213**,
source digest `1af76a2288eb7e735633c422dbd6a47b08fb6059e9fb2f52c605b955d5ba6f09`,
passed a bounded two-disk import, native upload interruption, explicit retention,
and fresh KVM disk-read probe on the owner-authorized disposable Fedora host.
This run adds fixture source and evidence; it changes no product runtime or schema.
All 71 complete acceptance cases remain open.

The [source recipe](../../tests/fixtures/import/multidisk-probe/README.md),
[exact executed programs](../../tests/fixtures/import/multidisk-probe/disposable-recorded-run/README.md)
and [observed fixture manifest](../../tests/fixtures/import/multidisk-probe/observed-fixture-20260907.json)
are tracked. Exact installed packages and executable hashes are in the
[environment supplement](environments/disposable-fedora44-multidisk-20260907.json).
It inherits the recorded Fedora 44, libvirt 12.0.0-3.fc44, QEMU 10.2.2-1.fc44,
Python 3.14.7 and nested KVM environment. Binutils was 2.46.1-1.fc44. SELinux stayed
Enforcing; the coordinator and image workers ran as the ordinary test user.

The generated source contains a 16 MiB boot disk and a 3 GiB data disk. Both use
split VMDK; the data disk spans two extent files and includes a 1 GiB generated
payload. A 512-byte original BIOS program reads sector zero of hard disk `0x81`,
checks the marker and prints PASS or FAIL, then halts without disk writes. It is
not a Linux/Windows guest, a VMware-origin appliance, or a readiness test.

The 1,074,472,960-byte OVA SHA-256 is
`1b2b15b998b4879d71cd2e5a1b137253cb41f14808a830ff8ba6ff231c0d88f0`.
Its initial CLI inspection and preview succeeded. The harness then raised
`KeyError: systemID`; the complete failed log remains in the ledger. The actual
field is `review.system.id`. The next program checked that field and applied the
same existing preview through **Jobs → plan show** in a real PTY TUI, avoiding
another preview or accidental apply retry.

The TUI detached while the coordinator extracted every descriptor/extent and ran
confined QEMU conversion, size checks, image checks and content comparisons.
Preparation operation `eac18aa4-edfa-4007-b49d-977f643b016b` succeeded. CLI result
and artifact verification retained the selected system and both disk IDs:

| Disk | Prepared container bytes | Virtual bytes | SHA-256 |
|---|---:|---:|---|
| boot | 393,216 | 16,777,216 | `fe1356a1e613740dc51fb799fc1f559a1f4e311bbcae6b15424c2bd01ec6dfd3` |
| data | 1,074,266,112 | 3,221,225,472 | `35f221cffb8db678c4a657aeb94429efe5cd580022387d339d2cfee055aec8e7` |

An external fixture step created pool `virmill-multidisk-12d7bba`, UUID
`36ab8b28-60d8-4495-9046-6f2f6bfc23b2`, with autostart disabled. That setup does
not qualify Virmill pool creation. The reviewed creation selected Q35 BIOS,
256 MiB RAM, one host-passthrough CPU, two virtio disks in boot order 1/2, explicit
conservative device policy, a local VNC socket and no NICs, UEFI or TPM.

Creation `a3b3f9c8-e474-48eb-a916-60ed534c81cc` was interrupted before definition.
The observer held a pidfd for the known test coordinator and watched its durable
receipt/events plus the new data file. After the boot disk was verified, native
allocated bytes on the data file grew from zero to **4,980,736**. The observer
SIGSTOPped that coordinator, rechecked that the latest event was the second-disk
upload intent with an incomplete receipt, then SIGKILLed only that process.
No production host, libvirt daemon or guest was killed by the crash injection.

The retained data file had the expected reserved size but SHA-256
`584f57166367a5aaf566ddc48c8f3380a74bba8a24255f4f23f56f48eaec70e2`, confirming
incomplete content. The first disk matched its expected hash. Restart changed
the job to `recovery-required`, with its receipt and all three resource locks
retained. Explicit reconciliation and definition resume returned exit 6 because
the full verified-volume set was absent. Volume identity, bytes and timestamps
were unchanged, and no new allocation/upload/definition event appeared. VM UUID
`3cc290fc-5c68-475b-8d43-44989ee02e50` was never defined.

**VMs → vm creation cleanup** then presented explicit retention in the real TUI.
Operation `6065b2c3-650e-4d4a-8d92-cfbb03c8fd56` succeeded after committing pins
for both generations. The original job stayed partial, linked to the child; its
receipt stayed unchanged and its old recipe could not resume. Locks were released,
and no file was deleted. Cleanup returned `complete: true`, `vmCreated: false`.

Fresh creation `8fb83369-b5b5-4ab5-babb-a4668f4e4af9` used new volumes and VM UUID
`b9496482-2eeb-40e1-892b-4e291c108c52`. Both full native readbacks passed before
exact definition and managed ownership were observed. The [captured stopped XML](../../tests/fixtures/import/multidisk-probe/qemu12-q35-defined.xml)
shows `vda`/`vdb` and boot orders 1/2; its SHA-256 is
`7880991288cfbfb9b757678c3c0c0211500fe1fc3f03274a29f84b03e632076e`.

A separately reviewed start reached running state with QMP `query-kvm` reporting
`enabled: true`, `present: true`. Visual inspection of the external screenshot
observed **VIRMILL SECOND DISK PASS**. Screenshot SHA-256 was
`2dc4dd5e8014e0613e7e494abd88f31e9f74f854a340d05feefe52d6f83022a7`.
The binary image stays in ignored local storage and on the test host; its capture
recipe and hash are tracked. External virsh capture does not qualify Virmill console.

The BIOS program has no OS/ACPI shutdown handler. A separate hard-stop plan required
both `host-mutation` and `data-loss-hard-stop`; operation
`8d660da1-1f96-4efa-80f2-f5bf473ab331` succeeded. Both completed disks retained their
pre-boot hashes. The two pinned partial copies, original OVA and prepared artifacts
were unchanged. All four guests ended stopped, all pre-existing XML stayed exact,
and the journal had 20 jobs and zero locks. The transient coordinator remained
available; no persistent service or release publication was enabled.

This supplies real evidence for the exact generated import, disk-order read and
coordinator-upload crash boundary. It does not complete IMP-02's VMware Linux and
multi-NIC scenario, host reboot/power-loss durability, native deletion, managed
storage permissions, OS provisioning, firmware/TPM capture/restore, USB, networking
or the rest of the release matrix. No architecture or locked-scope change was needed.
