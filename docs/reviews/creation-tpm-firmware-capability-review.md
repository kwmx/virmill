# Creation TPM and firmware capability review

Review date: 2026-09-07. Source: `ad608076cd928e6c6c270e6442cbc2d8047e6e0c`,
plus the parent's pending warning-only change in
`internal/creating/service_linux.go:251`. This is a bounded source review for
SNAP-01, IMP-07 and REL-03 prerequisites, not an ADR or native qualification.
Only this document was added. No libvirt connection, firmware execution, remote
operation or probe C ABI/GUID investigation was performed for this review.

**Creation preflight establishes advertised TPM device support and a matching
firmware declaration independently. It does not establish that the selected
firmware exposes EFI_TCG2.** `descriptorMatches` never reads `Firmware.TPM`;
the current descriptor test deliberately accepts an empty feature list for a
non-Secure-Boot request while leaving `TPM=true`. No current creation field or
preflight observation binds advertised TPM emulator support to a firmware
protocol result.

The parent reports that the disposable UEFI application booted, but its
`LocateProtocol(EFI_TCG2)` call returned `EFI_NOT_FOUND`
(`0x800000000000000e`). This review did not reproduce that observation. Pending
the separate probe ABI review and runtime diagnosis, it establishes only that
the supplied probe did not obtain that interface. It does not identify a missing
firmware driver, an absent emulator, a probe error or another runtime cause.

## Exact source checks

Line numbers identify the reviewed working tree; symbols identify entrypoints
if subsequent edits move the lines.

| Entrypoint | Current check and its limit |
|---|---|
| [CreationFirmware and CreationTarget](../../internal/domain/creation.go), lines 12 and 72 | Intent supplies mode, exact code/template paths, one shared format, Secure Boot and TPM booleans. Target records capability, emulator and firmware digests. There is no requested TCG2 requirement or observed firmware-protocol result. |
| [validateCreationSpec](../../internal/backend/libvirt/creation_linux.go), line 33 | Requires `x86_64` and explicit bounded machine/CPU/RAM. UEFI requires absolute code/template paths and `raw` or `qcow2`; BIOS rejects UEFI/TPM fields. This validates input shape, not executable firmware behavior. |
| [preflightCreation](../../internal/backend/libvirt/creation_linux.go), line 328 | Calls `GetDomainCapabilities("", architecture, machine, "kvm", 0)`, checks its bounded XML, then uses the returned canonical machine and emulator. It observes an active file pool and VM identity availability. The writable connection is needed by the QEMU capabilities API; preflight performs no allocation or definition. |
| [checkCaps](../../internal/backend/libvirt/creation_linux.go), lines 214, 251 and 265 | Requires KVM, x86_64, nonempty returned machine/emulator and supported CPU/devices. For UEFI, exact code path must be a loader value, loader support must be `yes`, and type must include `pflash`. Secure Boot requests additionally require loader `secure=yes`. TPM requests require `supported=yes`, model `tpm-crb`, backend model `emulator`, and backend version `2.0`. Those TPM enums are checked independently of the chosen loader. |
| [probeCreationDevices](../../internal/backend/libvirt/creation_probes_linux.go), line 29 | Requires a versioned `pc-q35-` or `pc-i440fx-` machine. Hashes the trusted QEMU binary and invokes selected NIC/controller/display/policy models with fixed `-machine none -device MODEL,help` arguments, bounded time/output and fixed environment. It adds no TPM model to that list and does not initialize the selected machine or execute firmware. |
| [matchingFirmwareDescriptor and descriptorMatches](../../internal/backend/libvirt/creation_probes_linux.go), lines 152 and 122 | Finds an exact declared code/template mapping with a compatible architecture/machine and the key policy below. It returns a digest of the first matching descriptor; it does not select a replacement firmware. |
| [firmwareFileDigest and openSystemFile](../../internal/backend/libvirt/creation_linux.go), lines 183 and 204 | Open canonical absolute paths without symlink components, require bounded root-owned regular files without group/other write access, and hash code/template bytes. Preflight combines those hashes with the descriptor digest. These identify observed inputs; they do not prove the loaded firmware's protocol set or auxiliary-state provenance. |
| [Service.Plan and checkTarget](../../internal/creating/service_linux.go), lines 232 and 425 | Planning permits the backend's machine canonicalization and rejects other silent spec changes. Apply validation repeats preflight and compares the complete target, including its digests. No swtpm executable digest/version or firmware TCG2 observation is separately recorded in this target. |
| [creationXML](../../internal/backend/libvirt/creation_xml.go), lines 47 and 146 | Requests the exact read-only pflash loader, explicit `secure=yes/no`, format and NVRAM template. Secure Boot requests add SMM. TPM requests emit `tpm-crb` with an `emulator`, `version=2.0`, `persistent_state=yes` backend. This is device/firmware intent, not a protocol or persistence test. |

The descriptor rules are specifically:

* Inventory is `/usr/share/qemu/firmware/*.json`, at most 256 paths. Descriptor
  opens reject symlink components; files must be root-owned regular files without
  group/other write access, with an initial size check of at most 64 KiB and a
  reader limited to 64 KiB plus one byte. Invalid wire JSON or
  a decode failure aborts the search. `wire.Validate` checks JSON ambiguity and
  syntax; the subsequent `json.Unmarshal` projects known fields and ignores
  unmodeled fields.
* Mapping device is `flash`; mode is absent/empty or `split`. Executable and
  NVRAM-template filenames match the requested strings exactly. Both descriptor
  formats must equal the single requested `Firmware.Format`; mixed formats are
  unsupported by this contract.
* A target architecture must equal `x86_64`, and at least one machine pattern
  must match the canonical machine using Go `filepath.Match`. Invalid patterns
  do not match. For the requested `pc-q35-10.2`, a compatible descriptor pattern
  is still only a declared target match.
* Secure Boot requires all three features `secure-boot`, `enrolled-keys` and
  `requires-smm`. Non-Secure-Boot requires absence of `enrolled-keys` and
  `requires-smm`, but permits `secure-boot`. These are the implementation's key
  policy predicates; they neither test TPM support nor establish TCG2 absence.
* The projection does not read `interface-types`, and other feature strings have
  no semantic effect. There is no TPM, measured-boot or TCG2 predicate. A raw
  descriptor digest does not turn ignored metadata into a checked capability.
* The loader capability check does not separately require `secure=no` for the
  false case, or inspect loader format enums. Format compatibility is checked
  against the selected descriptor. Neither observation supplies the missing
  firmware-protocol evidence.

The parent's recorded inventory was subsequently read from
`.virmill-local/logs/cold-probe-firmware-inventory-001.log`; no new native query
was made. It identifies the selected 2 MiB raw no-Secure-Boot pair and an installed
4 MiB qcow2 no-Secure-Boot pair with the same feature list: `acpi-s3`, `amd-sev`,
`amd-sev-es`, `verbose-dynamic`. Neither list declares TPM functionality. Their
equal feature lists do not prove equal firmware implementations or protocol
behavior. The record reports clean package verification and unchanged firmware
selection; package integrity is not firmware TPM qualification.

The recorded `40-edk2-ovmf-4m-qcow2-x64-sb.json` and
`41-edk2-ovmf-2m-raw-x64-sb.json` pair Secure-Boot-capable code with an empty
varstore, declaring `secure-boot` and `requires-smm` but no `enrolled-keys`.
**Neither pair is accepted by the current boolean policy:** `SecureBoot=false`
rejects `requires-smm`, and `SecureBoot=true` requires the absent `enrolled-keys`.
In general `secure-boot` alone is permitted for false, so the rejection is caused
by SMM coupling, not by secure-capable code alone. XML generation likewise adds
SMM only for true. Removing the descriptor rejection alone would therefore not
implement a reviewed SMM-enabled, unenrolled firmware profile. This is a
compatibility limitation of the current policy, not evidence that either pair
would fix the reported TCG2 lookup failure.

## Evidence and acceptance boundary

The shared review reports `startsVM=false` and `guestBootVerified=false`
([Service.Review](../../internal/creating/service_linux.go), line 290). The
creation completion predicate is verified independent volumes plus the exact
persistent definition. The pending warning explicitly leaves initial NVRAM
assignment, fresh auxiliary-state initialization and encrypted-guest recovery
unqualified. The separate [NVRAM binding review](creation-nvram-binding-review.md)
describes that identity gap; a TCG2 lookup result cannot resolve it.

For IMP-07, definition, start and reaching this UEFI application remain distinct
from obtaining TCG2, using TPM commands, guest setup and reachability. For
SNAP-01, even a successful protocol lookup would not prove preserved TPM/NVRAM
identity, a complete cold capture, revert, or source-branch safety. REL-03 is
supported here by making the documented limit explicit; this review does not
claim that all documentation/help validation or any acceptance scenario passed.

The following existing pure tests were executed in the sandbox:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -tags libvirt_dlopen ./internal/backend/libvirt -run '^(TestCreationCapabilitiesRequirePositiveSupport|TestFirmwareDescriptorRequiresExactMappingAndKeyPolicy)$' -count=1 -v
```

Result: **2 passed, 0 failed, 0 skipped**. They exercise synthetic metadata only.
The capability test's combined unadvertised UEFI/TPM case fails at the missing
loader check first; it does not independently cover each TPM enum rejection.
The descriptor test directly demonstrates the empty-features/TPM-enabled
acceptance described above. Installed-QEMU metadata and native simulated-domain
tests were not selected. No test here certifies firmware or TPM behavior.

## Smallest dependency-ready options

1. Preserve the current stage boundary immediately. Report the supplied probe's
   TCG2 result as unavailable/unresolved while retaining the independently
   observed boot stage. Do not infer firmware TPM functionality from successful
   preflight, XML definition, or device-help output. No matcher relaxation or
   silent switch of firmware, Secure Boot policy, CRB/TIS model or auxiliary
   state follows from this failure.
2. After the independent ABI review and the parent's exact firmware/package
   inventory, qualify the specific code/template digests, descriptor, canonical
   machine, architecture, QEMU/libvirt/swtpm versions and TPM device tuple with
   a validated disposable probe. Protocol discovery and a checked TPM capability
   operation can establish that particular runtime prerequisite. TPM state
   persistence and recovery then need separate before/after identity tests under
   the cold-capture contract; do not reuse or reset uncertain existing state to
   manufacture a passing result.
3. If a profile must promise firmware TCG2, the architecture owner should define
   an explicit requirement and a separately evidenced qualification result.
   Missing positive evidence can then produce an actionable unsupported or
   unqualified outcome before that profile is authorized. Preserve the meaning
   of old persisted plans: `Firmware.TPM=true` currently requests the emulator
   device, and must not silently acquire a stronger protocol guarantee. A new
   capability policy needs an explicit compatibility/version decision. Do not
   invent a descriptor feature name as a substitute for runtime evidence.
4. If the separate inventory/probe work motivates support for the installed
   40/41 empty-varstore pairs, first define explicit SMM, firmware capability and
   key-enrollment intent in a reviewed versioned policy. Update preflight and XML
   together under that contract. Keep existing guests and persisted plans on
   their reviewed selections. This option broadens supported firmware intent;
   it supplies no TPM protocol guarantee by itself.

The smallest useful regression fixture is an otherwise valid synthetic
`domainCapabilities` document with the exact pflash loader plus all four TPM
claims. Remove each TPM claim separately and require rejection, so the loader
cannot mask the TPM branches. Pair that fixture with a matching non-Secure-Boot
descriptor with empty features and a TPM-enabled spec: under today's contract
both predicates pass and the review must still claim no firmware protocol proof.
When an explicit qualification contract exists, extend the same pair with
missing, failed and exact-tuple positive evidence. Those tests validate the
contract boundary; only the separate native run can qualify the tuple.

For the optional SMM policy extension, the smallest additional fixture is the
recorded 40/41 feature set with its exact code/unenrolled-template mapping: assert
today's rejection for both booleans, then require the new explicit policy and
corresponding SMM XML before accepting it. No behavior change is proposed as an
unversioned relaxation of the existing predicate.
