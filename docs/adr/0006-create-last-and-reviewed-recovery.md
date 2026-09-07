# ADR 0006 — Define-last creation and reviewed recovery

Status: accepted implementation decisions under documents 03, 05, 11, 12 and 14.
ADR 0007 supersedes the partial-disposition and schema-2 migration paragraphs
below; they describe the state when this creation slice was introduced.
This does not reduce the locked 1.0 scope or certify a host/guest combination.

The first VM creation adapter consumes a successful local `import.prepare`
operation. A public `PreparedImport` manifest alone cannot establish that image
parsing ran under confinement: approval compares the current artifact with its
immutable coordinator receipt, actor and completed operation. All prepared disks
and original NICs require explicit mappings. CPU, memory, machine, firmware,
controller buses, boot order, clock, graphics, network UUIDs and link state are
reviewed. Clone identity generates a fresh UUID and locally administered unicast
MACs; it does not generalize identities or credentials inside guest disks.

Preflight uses native libvirt domain capabilities and active existing file pools
and networks. Pool capacity is checked separately from the configuration digest,
because allocation changes capacity accounting. No pool/network is created or
activated implicitly. The selected machine resolves to an explicit version.
Libvirt's [interface capability report](https://libvirt.org/formatdomaincaps.html)
does not enumerate NIC models. Fixed argument, bounded
[`-device MODEL,help` queries](https://www.qemu.org/docs/master/system/invocation.html)
against the root-owned installed emulator supply device metadata. These queries
do not initialize a VM and are not guest-driver or boot tests.

The libvirt Secure Boot loader flag does not prove keys are enrolled in the
selected template. UEFI therefore pins the code/template hashes and a root-owned
system QEMU firmware descriptor that matches both paths, formats, machine and key
policy. Secure Boot requires the descriptor's secure-boot, enrolled-keys and SMM
features; the nonsecure profile refuses enrolled-key/SMM substitution. Files must
have no symlink components and no group/other write access. TPM is an explicitly
advertised emulator 2.0/CRB device with persistent state. This path creates fresh
auxiliary state for a clone; it is not encrypted-guest recovery or TPM migration.

The native [volume upload API](https://libvirt.org/html/libvirt-libvirt-storage.html#virStorageVolUpload)
transfers the file container bytes, not its logical guest sectors. Allocation
therefore creates a new raw file of the prepared qcow2 file's physical length;
upload writes the complete independently converted qcow2 container. Completion
requires stream finish, native qcow2 capacity/backing metadata and a full native
download hash matching the prepared bytes. Every volume is checked for domain
references before upload/readback. Actual qemu storage-driver refresh, streaming,
labeling, cancellation and durability remain integration qualification blockers.
Libvirt's [data-access implementation](https://github.com/libvirt/libvirt/blob/master/src/libvirt-storage.c)
requires a non-read-only connection for downloads as well as uploads; volume and
stream must share that connection. Native simulated-storage tests confirm these
API guards. Readback runs only inside the authorized creation/recovery job and
does not request upload or other storage mutation.

The journal records every allocation intent and returned identity, upload phase,
verification and definition intent. Only after every new independent disk verifies
does the backend define a powered-off domain with validation enabled. Reconciliation
checks the metadata binding, explicit configuration, volume identities and selected
network identities/configuration. Unknown semantic XML additions are refused;
only explicitly understood normalization is allowed. Real qemu-driver normalization
still requires an authorized test environment. Existing/adopted XML is never
reconstructed by this generator. Managed ownership requires both the committed
creation catalog and the domain's matching namespaced identity; a name alone does
not adopt a VM. Ownership is not proof of current hardware or guest readiness.

Failures retain originals and newly allocated resources. An incomplete upload is
never blindly resumed. A complete verified set whose definition failed can use a
fresh `vm creation resume` plan. It inherits every original lock atomically,
rechecks the unchanged target and source, hashes all retained volumes again, commits
a new verification receipt and defines the original intended VM. It never allocates
or uploads. Its parent remains `partial`, links to the recovery operation and is
never relabeled successful. Cancellation or validation failure retains inherited
locks. An acknowledgement lost after definition is resolved by observation.
Partial-volume cleanup/disposition remains a separate required adapter; this path
does not delete resources or unlock uncertain partial storage.

SQLite schema version 2 is a semantic downgrade barrier for inherited locks. On
opening schema 1, the coordinator writes and flushes a private consistent
`journal.db.pre-v2-UUID.db` backup before committing version 2. Existing job,
plan, event, receipt and lock records remain unchanged. A schema-1 binary must
refuse the newer database; otherwise its old cancellation rule could release a
recovery job's inherited uncertain resources. Versioned creation/verification/
ownership JSON records reject future versions or unknown fields. Migration,
transaction rollback and cancellation are tested with generated local fixtures.

No native allocation, domain registration, firmware initialization, TPM creation,
guest start or host networking was executed on the discovered development host.
All mandatory creation sources, full forms, guest adaptation, partial-resource
disposition and real-backend evidence remain part of the unchanged release scope.
