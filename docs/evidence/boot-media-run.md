# Native boot/media and configuration preservation run

On 2026-09-07 the owner-authorized disposable Fedora 44 host ran development
revision **8a092b4e234abe118de079a99ac2c9e170d44ece**, source digest
`9b9ad3228b343a600b34349081c5815294ed2f5654bbaf0d74cd7919290f4a1e`.
Native package versions remain those in
[the captured environment](environments/disposable-fedora44-20260907.json).
The CLI/daemon RPM was verified before and after installation; the helper stayed
inactive and its socket disabled. Only the transient test coordinator was active.
No local development-host virtualization mutation or publication occurred.

The tested guest was the previously created, independently copied Kali Q35/BIOS
VM `a19bf9ee-cd7f-4921-baac-39ce1694eb35`. It has no NIC, USB or TPM. Its original
2-CPU/2048-MiB definition was recorded in the device-policy run. The supplied ISO
and a new independent, mode-0444 pool copy both have SHA-256
`6dbefacc95e3b556c19c48e8bae39b8b505e2d3a1aba0bfb7ab62b036c3d2ba3`
and size 4,802,531,328 bytes. External virsh attachment prepared a read-only SATA
CD-ROM (`sda`, volume source); this does not qualify Virmill disk attachment.

| Observation | Result and limit |
|---|---|
| Actual TUI `vm boot show` and `vm set` JSON form | Reviewed before/after boot intent and exact digest; CD-ROM first, existing `vda` second. Operation `53df9a52-1de8-4a81-bf29-4ecccf0f4a56` succeeded with native preservation readback. |
| ISO boot | Virmill start `f2802574-e131-4d18-aba5-4c32361afcc6` succeeded. External QMP confirmed KVM enabled; a captured screen showed the Kali BIOS installer menu. No installer choices were entered. |
| Explicit force-off | Reviewed `data-loss-hard-stop`; operation `0c058b7e-8b70-4824-bcbf-60ff5bf44f1d` succeeded. This was explicit, with no automatic graceful-to-force escalation. |
| Combined CLI edit | Operation `bc150fc3-59e5-48cd-919f-b4df591e5c4c` set 3 CPUs/3072 MiB, selected `vda`, and ejected `sda` in one native definition. Empty read-only drive retained, volume→file normalization reviewed explicitly, ISO retained unchanged. |
| Live/persistent observation and refusal | Both boot layers observed while running. Attempting another stopped-only edit returned `UNSUPPORTED_CAPABILITY`, with no new plan/job or persistent change. |
| Boot after ejection | Start `24129b75-0d8f-42e7-9d9d-05c4247e0fbd` succeeded; the Kali graphical login screen was visually inspected. QMP reported 3 CPUs and 3,221,225,472 bytes base memory. Guest login, online resource accounting and reachability were not tested. |
| Graceful shutdown | Operation `4e342132-fc81-42e0-9e84-9ca002a51bd7` succeeded and the guest stopped. The first harness failed by treating `verifying` as terminal; an observation-only follow-up confirmed success without replay. Both records remain in the ledger. |
| External edit after preview | Harmless namespaced metadata was inserted through virsh. Virmill refused the stale plan with `STALE_PLAN` before accepting a job; newer XML stayed intact. This tests observed drift, not atomic exclusion of every external race. |
| Unknown metadata preservation | Fresh CPU edits 3→4→3 succeeded while preserving the injected namespace. Operations `8ff27726-44bc-4b33-af95-3f29669ecc8f` and `8ce5151d-e14b-491c-9568-01b7fa14b621`. Final XML equals the post-external-edit definition byte-for-byte. Adoption itself was not tested. |

Exact QMP resource semantics are documented in the
[QEMU QMP reference](https://www.qemu.org/docs/master/interop/qemu-qmp-ref.html#command-query-memory-size-summary).
The observations above are actual output from this installed QEMU, not inferred
hardware support. Screenshots were captured externally, not through a Virmill
console workflow; they remain ignored local artifacts and are reproducible using
the included capture recipes.

| Captured artifact | SHA-256 |
|---|---|
| Attached fixture XML | `b587370d482485f7cde9c664f690e90c2748a83f530da7f216af1a5483847a36` |
| CD-first XML | `f8f7ee1781d4f800a3fae00ce00669b7b74001ecfb694a8d3f7e0344e0738c7a` |
| Ejected/resized XML | `4403608ec972efdf6f7134786093b8500697c6c802bf4a860ab8698dc037e07b` |
| Final XML including metadata fixture | `e31e20ae14df1e2809c242416ff31416249b8c4d3a14dab74507afb02e0250d2` |
| ISO installer-menu screenshot, 153,610 bytes | `a0ac7f79e93649e5b37b44593954023527c6f9e87589eba1732ee4cd936df6c3` |
| Disk login screenshot, 585,397 bytes | `152bb7cc6bc803230b01a4a499a14b00b7a79cd4330e78d16abefcc1e9972c70` |

Final preservation checks found all three guests stopped. The unrelated guest and
earlier uncertain definition retained their exact XML hashes. Source ISO, source
QCOW2 archive/extract and earlier uncertain volume retained their recorded hashes.
All new jobs succeeded with no remaining locks; the earlier uncertain creation
still retains its three locks. Final guest resources are 3 CPUs/3072 MiB, `vda`
first and an empty read-only `sda`. No source media was removed.

The [captured fixtures and exact procedures](../../tests/fixtures/configuration/README.md)
are associated with append-only `boot-media-*` ledger entries. Local race tests,
static/portable checks, staged artifacts and 1,000 bounded fuzz executions passed;
two complete offline build/package runs produced identical seven artifact hashes.
These do not certify hardware beyond the separately recorded native observations.

Full 1.0 remains blocked: live/advanced configurations, adopted-VM qualification,
secure firmware/TPM capture/restore, physical USB reconnection, routing/isolation,
all import origins, complete installers/consoles/provisioning, active-effect crash
recovery and the remaining mandatory matrix still require implementation or evidence.
No acceptance scenario is promoted to complete by this narrow run. Native plans
also exposed the existing default `requiresDowntime: false` estimate on lifecycle
operations; explicit stop warnings were present, but structured estimates need correction.
