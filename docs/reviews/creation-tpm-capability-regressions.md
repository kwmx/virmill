# Creation TPM capability regressions

Date: 2026-09-07. Source baseline:
`ad608076cd928e6c6c270e6442cbc2d8047e6e0c`, with concurrent parent-owned changes
outside these two new files. This work adds tests and documentation for IMP-07,
SNAP-01 and REL-03 prerequisites. It changes no production predicate, persisted
contract, firmware selection or device mapping, and closes no native acceptance
scenario.

The new [creation_tpm_capabilities_test.go](../../internal/backend/libvirt/creation_tpm_capabilities_test.go)
addresses the coverage gap identified in the
[firmware capability review](creation-tpm-firmware-capability-review.md): the
older combined unsupported UEFI/TPM case fails its loader check before reaching
the TPM predicate. The new fixture supplies the exact accepted pflash loader and
all other requested CPU, disk, network and graphics capabilities first.

| Test | Regression covered |
|---|---|
| `TestCreationTPMCapabilitiesRequireEachAdvertisedClaim` | Nine cases remove support, deny it, mark it unknown, remove or replace `tpm-crb`, remove or replace the emulator backend, or remove or replace version `2.0`. Each must return `UNSUPPORTED_CAPABILITY`. The same altered capabilities must pass with only the requested TPM flag disabled, proving another gate did not mask the TPM failure. The unaltered complete tuple must pass first. |
| `TestCreationTPMCapabilitiesAcceptDeclarationsWithoutRuntimeEvidence` | The complete synthetic TPM tuple and an exact non-Secure-Boot descriptor with an empty feature list both pass their respective predicates. These inputs contain no runtime protocol evidence; the test does not open their synthetic paths or perform a firmware lookup. This documents what predicate success means, without inventing a runtime-proof field or stronger contract. |
| `TestCreationTPMDescriptorRejectsUnenrolledSMMForBothPolicies` | Four cases cover raw/qcow2 and both SecureBoot values. The recorded 40/41 feature combination includes `secure-boot` and `requires-smm`, but no `enrolled-keys`. Both boolean policies reject it. Removing only `requires-smm` for false, or adding only `enrolled-keys` for true, makes the unchanged mapping match and isolates the policy failure. |

The descriptor tests use synthetic code/template names and the recorded feature
combination. They do not certify those installed firmware files. The paths and
capabilities are test inputs, never runtime mocks. No libvirt connection, QEMU
command, native domain operation, firmware execution, TPM command or auxiliary
state access occurs in the selected tests. No dependency version changed or
download was required.

Executed with the pinned repository toolchain and vendored dependencies:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -tags libvirt_dlopen -race ./internal/backend/libvirt -run '^TestCreationTPM(Capabilities|Descriptor)' -count=1 -v
GOPROXY=off GOSUMDB=off ./scripts/go vet -mod=vendor -tags libvirt_dlopen ./internal/backend/libvirt
```

The targeted race run passed **3 top-level tests and 13 subtests**: 16 test PASS
records, **0 failures and 0 skips**. Package vet passed. Other package tests,
including installed-QEMU and native simulated-domain tests, were not selected;
their absence is not a pass or a skip in this run.

For IMP-07, passing capability predicates remains distinct from definition,
start, guest boot and reachability. EFI_TCG2 discovery and TPM behavior remain
unverified by these tests. For SNAP-01, TPM/NVRAM identity preservation, complete
cold capture, revert and source-branch safety require the separate native
workflow. REL-03 receives a documented, reproducible test boundary here; this is
not a claim that all documentation/help validation passed. Native qualification
and any new firmware-policy contract remain with their owners.
