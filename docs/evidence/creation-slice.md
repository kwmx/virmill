# Creation and definition-recovery evidence

Implemented source: `a05aabb`; private-daemon integration check and guide correction:
`de5743ab955cdc047f0b010af6c8e025043b107a`. The latter revision is embedded in the
development artifacts. No release has been published.

| Check | Recorded result | Scope |
|---|---|---|
| [creation-core-001](logs/creation-core-001.log) | passed, with explicit skips | Complete Go race suite; synthetic creation/recovery phases, source authority, schema compatibility, managed identity, transaction rollback, inherited-lock cancellation and migration; native test-driver XML and API guards |
| [creation-metadata-001](logs/creation-metadata-001.log) | passed | Installed QEMU help output and OVMF code/template/descriptor hashes only; ordinary host namespace preserves root ownership checks |
| [creation-static-001](logs/creation-static-001.log) | passed | Go vet against the vendored dependencies |
| [creation-portable-001](logs/creation-portable-001.log) | passed | Common packages and example/plugin SDK cross-builds; not a non-Linux hypervisor |
| [creation-build-001](logs/creation-build-001.log) | passed | Two offline builds produce identical three binaries and four unsigned development RPM/DEB packages |
| [creation-artifacts-001](logs/creation-artifacts-001.log) | passed | Staged installer/uninstaller preservation, archive metadata, temporary private daemon, real confined generated-image preparation, creation source validation and deliberate runtime test-URI refusal |
| [creation-release-gate-001](logs/creation-release-gate-001.log) | blocked as required | Complete checklist and all 71 acceptance records still lack full release qualification |

The core run used source digest
`f8970b02140945961de28292c891a7b3f8a43abb417b2d39f7a9db4cb8fc76d0`.
The remaining checks used
`307864dad74516ddd87e99cec14b7356d8e0c26afc99c7ed49e7d566086b161d`.
Only the integration test changed executable/test source between those revisions.
The append-only ledger stores exact commands, dates, environment references,
fixture hashes and log hashes. Skipped IPC/confinement paths in the default-sandbox
core run are not passed evidence; the explicitly enabled private-daemon artifact
check separately exercises its stated IPC and generated-file conversion paths.

No test here successfully allocates or transfers a native qemu storage volume,
registers a real domain, creates NVRAM/TPM state, boots a guest, tests its drivers,
routes packets or proves isolation. Native simulated storage is an in-memory
libvirt test-driver object, not a disk. The example BIOS/UEFI domain XML is validated
by the simulated driver only. Read-only firmware descriptors do not establish that
Secure Boot enforcement or enrollment works in a guest.

Partial/unverified native volume cleanup, all required creation entry paths,
complete hardware forms and the rest of the mandatory workflow suite remain
implementation work. The actual storage, normalization, security-label, firmware,
TPM and guest matrix remains blocked on an authorized disposable environment and
legitimate source media. All 71 release acceptance records remain unaccepted.
