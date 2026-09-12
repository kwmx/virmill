# Resumable setup and native guest-tools integration

Implementation adds private resumable import/creation choices, including advanced
settings, explicit Resume/Start new controls, source re-inspection and a durable
pre-submission marker. Drafts contain no credentials, cached metadata or approved
plans. Conflicts retain the stored document and show an error. Draft unit/race
checks exercise restart, concurrent writers, malformed/future data, canceled
inspection, failed preparation lookup and submission/exit ordering. These checks
do not certify native virtualization or actual terminal behavior.

The Fedora fixture used installed source e53f2e21e0ecf6b0c7a2bd6153f487df12d56236,
CLI SHA-256 `472e4ae0ce295a6bd1c2169c0d5edc5a67086a018ddfab0d5d65dd2daffa57a8`.
It copied the inspected source into a fresh independent disk, removed the agent
only there with the offline package manager, booted the owned guest, verified
its pre-injected SSH host key and confirmed the package was absent.

Native run `guest-tools-fedora-live-001` failed honestly: the guest package apply
receipt completed, but verification refused a changed VM fingerprint. Job
`6761165a-a4fe-46c9-ad28-839490b877a0` remains recovery-required at step 3, with no
verify intent and no retry. Owned VM `f9c6c7b4-0f62-44ae-8445-850dcac6c98b` remains
running for investigation. Existing guest and job inventories were preserved.
The fixture also compared runtime disk/network additions too strictly; corrected
checks use persistent ownership plus runtime identity and exclude only the live
network's numeric connection counter. The first failure is retained unchanged.

Changing only the current QEMU agent target state back to disconnected reproduces
the exact reviewed fingerprint
`69bee1a04c8d5c4e36840600c93c530f4c753f4ed280004638730468feb39024`;
the connected fingerprint is
`03813c16eb93d3b4b45eaaae66b04fd6d0cbd9be7fdf876f3528074299bd5f77`.
ADR 0044 records the narrow, versioned correction. A fresh absent-to-installed
native run is required before claiming the fix works in a real guest.

## Corrected native run and installed TUI

Source `f371fa8978dad4b49795dce0852aca04a1119659` was built offline with pinned
Go 1.27.1 and vendored dependencies. Full Go race tests and vet passed. Package
checks passed all three archive/CLI/installer cases. RPM upgrade and idle daemon
restart preserved the full observed journal, guests and media.

Installed CLI SHA-256:
`58bf8b16362043a9df0693c027083e9569ca6ae621660247ba2463b53deadd02`.
Coordinator SHA-256:
`06d55685cf5d97e5ce61f8ae457215e09338251c1d9567deea56f8fc8daf660a`.
Core RPM SHA-256:
`f419ca37e4e29ff78c62cb6417a3077b073ea9257d87946d30a379f387f8e69c`.

`guest-tools-fedora-live-002` passed on fresh owned VM
`71001e0c-e99d-4e26-a886-0553533c5fa0`. Install operation
`eb0dfc2f-c6af-4639-8143-4171f8c2bd9f` recorded ready → needs-apply → applied →
verified; repeat `731135f3-cfce-4404-97f2-9f8e012a0ecf` recorded ready →
already-configured → skip-apply → verified. Libvirt's independent guest-ping
returned responsive. Installed package is qemu-guest-agent 10.2.2-1.fc44.x86_64,
with systemd 259.5-1.fc44, OpenSSH server 10.2p1-7.fc44, sudo 1.9.17-7.p2.fc44
and NetworkManager 1.56.0-1.fc44. The actual 80×24 guest-tools form opened. The
fixture stopped gracefully; preexisting guests/jobs, source/copy hashes and
network definitions/state were preserved. This proves CLI installation and TUI
form access, not TUI job submission or desktop integration.

`wizard-drafts-tui-native-001` passed three installed 80×24 sessions: edit an
uninspected ISO media name, exit normally, resume that value after restart, and
explicitly start new with the old value absent. Private isolated XDG state kept
the owner's drafts untouched. No source parsing, plan, job or guest mutation ran.
Actual frames exposed a misleading overview footer under the resume modal; a
small follow-up displays the modal's Enter/Tab/Esc controls instead.

All 71 acceptance scenarios remain required. This slice does not promote an
acceptance status or imply full release readiness.
