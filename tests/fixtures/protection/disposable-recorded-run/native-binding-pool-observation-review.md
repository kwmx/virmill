# Native-002 pool assertion and supplemental observation review

The final native-002 failure is a fixture comparison error. The parent readback
shows that only two pool space counters changed; every other XML byte stayed
identical. The existing creation operation had already reconciled successfully.
Preserve the failed native-002 report and verify that same operation through a
separate read-only observation. No new apply, guest, definition or reconciliation
is needed.

The parent supplied these exact saved-command observations:

| Field, bytes | command-047 | command-388 | Difference |
| --- | ---: | ---: | ---: |
| capacity | 272029974528 | 272029974528 | 0 |
| allocation | 145265324032 | 145318100992 | +52776960 |
| available | 126764650496 | 126711873536 | -52776960 |

Both pairs sum to the same capacity. The parent confirmed single direct
`unit='bytes'` leaves and unchanged type/name/UUID/source/target/path/permissions/
SELinux label and remaining XML bytes. These are parent-operated native
observations; this reviewer made no remote call.

Libvirt defines allocation as pool storage usage, available as space for new
volumes and capacity as total storage capacity. Usage can include metadata
overhead, and pool metadata can be cached. These values cannot serve as immutable
configuration bytes across volume creation. The observed delta is not a
measurement attributable solely to the copied probe disk.
[Official storage XML documentation](https://libvirt.org/formatstorage.html#storage-pool-general-metadata).

The native API describes pool information as volatile, and the pinned official
Go bindings expose `StoragePoolInfo.Capacity`, `.Allocation`, `.Available` and
`StoragePool.GetXMLDesc`. A complete XML description is not a promise that its
space measurements remain constant.
[Official storage API documentation](https://libvirt.org/html/libvirt-libvirt-storage.html#virStoragePoolGetInfo).

The faulty assertion is line 821 of `nvram-binding-native-002.py`: it compares
the complete post-creation active pool XML with the pre-creation XML. Immediately
before it, lines 809–820 require the same job to be `succeeded`, the original
receipt with only `defined` changed to true, a complete declaration-bound result
with initialization false, restored exact A, preserved older guest/media
observations and unchanged retained new-disk identity/content and pool volume
inventory. The failure skips the final runtime recheck at line 822 and the
recipe's final success-report update. The supplemental observation must perform
that fresh runtime check; it must not reinterpret the original failed report as
passed.

The reviewed existing identities are:

| Item | Identity |
| --- | --- |
| Installed revision | `dc2ab1a7af8c023c1485d36d0e858a65264ace3e` |
| Deployment manifest SHA256 | `c0b4ef70f038291e4f4803e7934e660b740654c2e2e660bc3784cb86939914f9` |
| Original native-002 recipe SHA256 | `ca1d9bb9c64612071d71c3d8ccdc414d81cd31bc60aea08a200102d8c8720f50` |
| Plan | `2bd38ffb-6931-493b-bd01-5b56c68ff574` |
| Operation | `8cafa2bf-f148-4189-bf93-34340f1c432a` |
| VM | `666c692d-da0e-4119-9554-727c4af3c751` |
| Restored A XML SHA256 | `12ecd7f3a77ebcaf3b5942a1497bc0bbb32ce5546a3fa156f90ba7e24d432164` |

The parent reports 32 jobs, zero locks, zero triggers, seven stopped definitions,
and the original six guest XML/journal/disk observations preserved. Saved
`reconcile-restored-A-response.json` and `result-complete.json` describe this
operation as succeeded and declaration-bound, with a defined receipt and
initialization false. Current `Engine.Reconcile` deliberately rejects already
succeeded jobs; invoking it again would be an invalid request, not additional
recovery evidence.

The dependency-ready observation is implemented in the new, separately reviewed
[TUI-003 recipe](nvram-binding-tui-003.py) and described in its
[README](nvram-binding-tui-003.md). The parent uploads that actual source file and
passes the same full installed revision/deployment digest; its `__file__` hash
records the separate test-tool identity. It performs this bounded sequence:

1. Accept only the exact known native-002 failure, final stage, four earlier
   passing checks, identity tuple and preservation/lock/trigger assertions.
   Preserve the failed raw report and its digest in the new report.
2. Validate saved command-047/388 as the exact successful read-only pool calls.
   Parse one byte-valued leaf per statistic with integer bounds and the observed
   sum. Mask only allocation/available numeric text for comparison, keeping
   capacity and every other byte exact. Reject all unrelated differences,
   duplicate/nested statistics and malformed XML. Independently compare fresh
   pool XML before and after the UI observation; record current counters without
   requiring them to freeze or attributing their changes to one disk.
3. Verify the installed CLI/daemon and active coordinator PID executable against
   the deployment and native report. Read `operation show`, existing `plan show`
   and `vm creation result`; require the same completed job, immutable plan,
   original binding bytes/path/fingerprint and explicit false initialization and
   guest qualification flags.
4. Compare all current CLI formats and every current installed TUI page using
   the original bounded TUI observer. Send only read-form/navigation/detach keys.
5. Require exact before/after journal rows and all seven stopped XML/state
   observations, plus zero current journal triggers. Read-only SQLite checks
   neither remove a trigger nor clear locks. Keep original large-disk metadata
   and selected EFI hash evidence at their reported limits.

The supplemental report may pass only its own read-only confirmation. Native-002
remains a failed fixture, TUI-002 remains unexecuted, and native-001/002/TUI-001/002
remain immutable. This changes no application architecture, database contract,
runtime semantics or acceptance threshold. Initialization, complete capture,
independent recovery, guest boot and native method-call counts remain unverified.

Authoring validation: 15 local synthetic tests passed for TUI-003, including the
original ten terminal/child/environment tests and five pool/known-failure
validators with adversarial subcases. Native execution and ledger interpretation
belong to the parent. This review does not promote IMP-07, SNAP-01, JOB-02, UX-03
or release acceptance.
