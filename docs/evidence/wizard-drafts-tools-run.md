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

All 71 acceptance scenarios remain required. This slice does not promote an
acceptance status or imply full release readiness.
