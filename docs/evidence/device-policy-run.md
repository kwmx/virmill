# Device policy and first guest boot — 2026-09-07

The owner-authorized Fedora 44 test VM ran development revision
`c7f8b76a94e16e973684ad69a52e1be9f8b07bd5`. The same exact native environment and
supplied-media inventory as the [first run](disposable-first-run.md) apply. The
local development host was not used for VM, network, storage or service mutations.

The full race-enabled source suite, static analysis and staged package/private IPC
checks passed from a clean source archive. Two offline builds produced identical
three binaries and four packages. The core RPM SHA-256 was
`425a0c6f5a58e6efc889ba74c0506faa6be115f22235e0e62d7972fb458ae670`.
These remain unsigned, unqualified development artifacts.

A separate pool, `virmill-policy-c7f8b76`, was created as external disposable fixture
setup, without autostart. This is not evidence that Virmill pool creation ships.
The existing successful Kali QCOW2 preparation was reused; no source media was
replaced. New creation made a second independent volume and VM:

- VM: `a19bf9ee-cd7f-4921-baac-39ce1694eb35`, **Virmill policy Kali QCOW2**.
- Creation operation: `28701ac3-325d-4596-8103-38e19f74b7cb`.
- Pool: `eede6ba7-13a9-48d6-8cba-b8611d36513b`.
- Q35 `pc-q35-10.2`, BIOS, two vCPUs, 2048 MiB, one virtio disk, no NICs,
  VNC Unix socket. USB and balloon disabled; iTCO watchdog action `none`.

| Stage | Observed result | Boundary |
| --- | --- | --- |
| Upgrade | RPM verification passed; existing jobs and stopped XML preserved | One Fedora development upgrade, not the full distribution matrix |
| Preview | Explicit versioned device policy, exact resources and acknowledgements shown | No allocation in preview |
| TUI authorization | Real PTY displayed the persisted plan and policy; exact digest accepted one job; UI detached | A first harness navigation failure submitted nothing and remains in the ledger |
| Native creation | Full allocation/upload/readback, definition comparison and ownership catalog succeeded | 15,999,631,360 container bytes; 86,000,000,000 virtual bytes; one supplied disk |
| Guest start | Separate `vm.start` operation succeeded | Running state alone is not guest readiness |
| Boot observation | QMP reported KVM present/enabled; captured screen showed the Kali graphical login prompt | External virsh screenshot and visual inspection; no guest login, reachability or Virmill console workflow qualification |
| Graceful shutdown | Separate `vm.stop` operation succeeded; native state became shut off | No forced shutdown used; guest disk retained |
| Preservation/recovery | Source hashes and unrelated guest XML checked; legacy mismatch still refused | Original uncertain job remains unresolved with its three locks |

The pre-boot verified volume hash was
`8eb9be4a9592ad8242fa4e99fbac2dd2eef474bb93e99f3699d17617c7d93c70`.
Its container may legitimately change after guest boot. The old uncertain volume
and original source remain distinct and must not be replaced with that changed
clone. Captured pre-boot XML is the reproducible regression fixture
`tests/fixtures/creation/qemu12-q35-reviewed.xml`, SHA-256
`a673bc94f8e039591ff1e3a0a2d9e7de2a0f3d2432f9c4074ef3945ccffe0515`.

The generated guest screenshot SHA-256 is
`cd6feb048bdf234aca919d962a71b9d5fcf7d0bf0b332801591c8214f43ccbc6`.
It remains in ignored local test artifacts and on the disposable host; guest media
and wallpaper are not redistributed. The recorded screenshot recipe reproduces
capture with legitimately supplied media. Manual observation does not change
Virmill's `guestBootVerified`, `setupVerified` or `connectivityVerified` result flags:
automated guest verification remains outstanding.

The original uncertain VM `ae630461-91d3-4f07-ad88-e6842c3dc3ea` was never started,
redefined or silently adopted. New policy semantics do not change its old recipe.
A separate safe acceptance/disposition workflow remains an implementation blocker.
A transient result-query UX defect was also observed: during initial validation,
`vm creation result` incorrectly suggests recovery while `operation show` correctly
reports a running job. That correction remains tracked separately.

The installed coordinator is transient user unit `<test-vm-login>-c7f8b76.service`,
with a two-hour runtime bound and the private XDG environment documented in the
first run. Use the normal login runtime/bus for systemctl. All three test-host
VMs are stopped after this run; source media, both created volumes and journals
are retained. No persistent Virmill service was enabled or release published.

All 71 acceptance scenarios remain required and open. Multi-disk OVA creation,
other guests and machine profiles, networking/isolation, USB, firmware/TPM,
active-operation crash boundaries, safe captures/restores and the full release
checklist still require implementation and evidence.

The clean-archive release evaluation `device-policy-release-gate-001` failed as
expected: all 71 mandatory acceptance records and the full ship checklist remain
open. `device-policy-captured-001` passed both exact XML regression fixtures; it
does not itself validate hardware. No release-completion claim was made.
