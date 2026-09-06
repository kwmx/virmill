# 05 — VM creation, appliance import and export

## Supported entry paths

| Source | v1 behavior | Boundary |
|---|---|---|
| Installation ISO | Create VM, attach media, select hardware/profile, boot installer, preserve install progress | Unattended installation only for explicitly supported recipes; an arbitrary ISO is not automatically installed. |
| Cloud image | Verify provenance/digest, copy or clone into managed storage, generate compatible NoCloud seed, boot and monitor provisioning | The image must support the expected cloud-init/guest setup path. |
| Existing disk | Inspect actual format and backing dependencies, map disk order/controllers, copy by default, define VM | A disk alone may not identify firmware/guest configuration; request/record assumptions. |
| OVA/OVF | Safely inspect descriptor and all referenced disks, map virtual hardware/networks, convert using the appropriate path, stage and register | OVA is packaging, not proof of universal boot compatibility. |
| Native export | Verify manifest and members, map local resources, restore or clone identity intentionally | No automatic attachment to the source host's LAN or devices. |

Required disk formats include qcow2, raw, VMDK, VDI, VHD and VHDX when the installed supported QEMU build exposes their format drivers. Maintain fixture tests for the advertised variants, including split VMDK and multiple disks. Detect encrypted, corrupt or unsupported layouts before registration.

`qemu-img` converts disk formats; it is not a guest-driver/bootloader conversion solution. `virt-v2v` can modify supported guests for KVM, and its VMware OVA input documentation specifically restricts that path to OVAs exported by VMware vSphere. Therefore, do not pass every OVA to that path and call the importer complete. [S12] [S13]

## Import state machine

`selected → inspecting → planned → staging → converting → validating → defining → optional-start → readiness-check → completed/partial/recovery-required`.

Inspection is bounded and side-effect-free with respect to the source. File uploads/downloads and extraction into staging are explicit jobs. Keep expensive immutable inspection results cached by source digest, not just filename.

### 1. Establish the source

Open local files without following unexpected symlinks. Record path identity, size, modification data and SHA-256. URL downloads require explicit initiation, TLS validation, final-origin review after redirects, size bounds and a checksum/signature when available. Never execute an appliance's included scripts on the host.

Original files remain untouched. Copy is the default. A separately approved `use-in-place` path requires exclusive-usage checks, clear ownership semantics and an explicit warning that the guest will modify the disk. It cannot be selected silently to save storage.

### 2. Validate archive boundaries

Reject absolute paths, `..` traversal, symlink/hardlink escapes, device nodes, sparse/decompression bombs, duplicate/conflicting member names and unexpected volume counts. Resolve OVF file references strictly within the staged source. Disable XML external entities/network resolution and bound nesting, elements and descriptor size.

Default guardrails: 16 MiB descriptor size, 10,000 archive members, maximum ordinary directory depth 32, and an unpack budget derived from declared data plus configured free-space limits. Do not recursively unpack nested archives in the baseline; a nested archive must be separately selected as a new source. Administrators can raise reasonable limits after a preview; path and entity safety cannot be disabled.

Appliance manifests commonly provide integrity metadata, not necessarily publisher authenticity. Verify provided checksums, report weak algorithms, and distinguish checksum-valid from signature-trusted. Missing signatures must not become a false trust badge.

### 3. Inspect and classify

Detect descriptor vendor, guest/architecture hints, disks and controller order, NIC networks/MACs, CPU/RAM, firmware hints, removable media, and appliance property requirements. Confidence levels are `detected`, `inferred` and `user-specified`. Preserve the source descriptor and an import report.

Multi-VM OVF collections must be enumerated. Support selecting individual members or producing a reviewed lab plan; never silently import only the first VM and call the full appliance imported. Unsupported OVF environment/property injection must be shown before conversion, with a guest-specific manual setup task where appropriate.

### 4. Select a conversion strategy

- **Native-compatible disk:** staged copy/format conversion plus explicit virtual hardware mapping.
- **Supported VMware vSphere OVA guest:** virt-v2v conversion path, with a machine-readable invocation/result adapter and sandboxed source access.
- **Other OVA/OVF:** parse descriptor, extract disk set, convert supported disk formats with qemu-img, map controllers/firmware conservatively, and use explicit supported guest adaptation only where available.
- **Unsupported guest/boot/controller combination:** report the actual limitation and available alternatives; preserve the source and inspection output. Do not “fix” it by random firmware/controller changes.

Set disk source formats explicitly. Inspect the entire backing chain in a confined environment; reject references to host files outside the approved source. Disable network-capable block protocols for untrusted disk inspection. Conversion runs unprivileged with bounded resources and no arbitrary host filesystem access.

### 5. Map resources and review

Map every input disk to a target volume and controller position. Map every NIC to a selected network; default to disconnected or an explicitly chosen safe lab/NAT profile for untrusted appliances. Preserve MACs only when explicitly requested; normal clones/import copies get new MACs and VM UUIDs.

The review must name changes in firmware, machine type, CPU model, controller, NIC model, boot order, virtual disk size and guest preparation. Windows-compatible profiles include the necessary supported firmware/TPM options, but no license activation or universal old-guest compatibility is implied.

For sources with existing guest identity or disk encryption, separate `clone` from `restore` semantics. New TPM state can make an encrypted guest unbootable; do not apply template identity rules to a recovery restore.

### 6. Stage, validate, commit

Calculate temporary and final space requirements, including source copy, converted disks, sparse expansion risk and a safety margin. Conversion output stays under a unique job staging directory. Verify completed files with safe offline checks, expected virtual sizes, backing references and checksums. Hashes prove artifact identity, not guest bootability.

Create final managed volumes without overwriting existing files. Publish final paths only after completed writes and durability checks. Define the VM last. If registration fails, keep clearly labeled staged artifacts with a cleanup/retry plan; do not leave an apparently successful inventory entry with missing disks.

Do not parse human-oriented percentage text without a documented adapter. Prefer tool-supported structured or stable machine-readable modes; otherwise report phase rather than fabricated progress.

### 7. Boot and verify honestly

After optional start, check hypervisor state, guest-agent readiness, known supported provisioning completion and explicit network/service health gates. A ping response alone does not prove the intended guest booted or setup finished. Use per-import identity tokens or the expected guest-agent/SSH host identity when available.

Report separately: artifacts created; VM defined; VM started; guest boot confirmed; guest setup completed; requested connectivity verified. Unknown readiness is a valid truthful result, not a reason to claim full verification.

## Creation defaults

Prefer virtio devices only where the selected guest profile supports them. Use conservative compatibility profiles for unknown/older appliances. Persist machine type/version and firmware selection; do not automatically change them on a later application upgrade. CPU host-passthrough is a local-performance choice, while a portable CPU profile is an explicit option.

Disk expansion changes virtual block capacity only. Filesystem/partition growth is a separate supported guest action and must not be claimed merely because the virtual disk grew. Cloud-init can perform appropriate first-boot tasks on compatible images; NoCloud supplies user-data, metadata and network configuration. [S14]

## Export

Mandatory native VM/template/lab export is a versioned manifest plus complete independent artifacts, with checksums and provenance. A full export must flatten or include all required backing files. Offer identity-preserving recovery and identity-regenerating distribution modes explicitly.

OVF/OVA export may be provided for a documented portable subset, but universal round-trip compatibility is not a v1 release claim. Exporting a qcow2 or VMDK disk alone is never labeled a complete VM backup. Exclude secret values unless the user chooses an encrypted recovery export and understands its contents.

## Failure/retry cases

Handle full disk, source mutation, missing VMDK extent, unsupported OVF properties, converter crash, canceled extraction, backend definition failure, boot timeout, absent guest agent and missing installation media. Safe retries reuse verified immutable artifacts; partial conversion output is not blindly resumed. Every failure leaves the original intact and an intelligible operation report.
