# Native TPM state resolution

Date: 2026-09-07. Scope: read-only bindings, installed schema and official
upstream documentation/source review for SNAP-01 and BAK-01 prerequisites.
This document proposes boundaries for the architecture owner; it changes no
runtime contract and does not qualify a capture or restore.

**The reviewed public libvirt interface does not provide a complete TPM state
file inventory. Changing dump-XML flags does not expose an implicit emulator
storage path in upstream libvirt 12.0.0.** A verified explicit source declaration
can identify a storage location, but determining all current members and their
exclusive association with a stopped VM still needs a host observation boundary.

The parent reports that the separate 4 MiB qcow2 non-Secure-Boot probe reported
`SEEDED` and then `PRESERVED`, while stopped native XML contains emulator TPM
`persistent_state=yes` and `profile name=default-v1` without a source path. Those
guest observations support the tested marker's persistence across those boots.
They do not identify its host files or prove complete independent recovery.
This review did not repeat the native run or read TPM state bytes.

## Installed interfaces and their limits

The repository pins `libvirt.org/go/libvirt v1.12007.0` in
[go.mod](../../go.mod) and
[the dependency lock](../../contracts/dependencies.lock.json). The locally
installed schema belongs to `libvirt-libs-12.0.0-3.fc44.x86_64`, observed with
`rpm -qf /usr/share/libvirt/schemas/domaincommon.rng`. The package result is for
the development host's schema, not an independent observation of the remote
daemon or its downstream patches. Upstream source analysis below is pinned to
the `v12.0.0` tag.

| Installed Go entrypoint | What it exposes |
|---|---|
| [Domain.GetXMLDesc](../../vendor/libvirt.org/go/libvirt/domain.go), line 1358; `DomainXMLFlags`, line 323 | XML as a string. Flags are `SECURE`, `INACTIVE`, `UPDATE_CPU`, `MIGRATABLE`. There is no public `STATUS`, TPM-path, or complete-files flag. The binding has no context parameter and no structured TPM inventory result. |
| [Domain.GetMetadata](../../vendor/libvirt.org/go/libvirt/domain.go), line 1554 | Reads a selected metadata category/namespace. This is not an internal TPM-storage lookup. A future application record can preserve previously verified provenance; merely writing a path into metadata would not verify it. |
| [Domain statistics types](../../vendor/libvirt.org/go/libvirt/domain.go), line 816; [Connect.ListAllStoragePools](../../vendor/libvirt.org/go/libvirt/connect.go), line 1770 | No TPM-storage statistics category or domain-to-TPM-file enumeration appears in these bindings. Pool inventory is not a complete inventory of auxiliary state outside registered volumes. |
| [Domain.QemuMonitorCommand](../../vendor/libvirt.org/go/libvirt/qemu.go), line 85 | Generic QMP transport, not a TPM storage API. Its availability does not supply a complete host-state mapping. No monitor command was issued here. |
| [Connect.DomainXMLToNative](../../vendor/libvirt.org/go/libvirt/connect.go), line 2396 | Converts supplied XML to a native representation; it is not observation of a particular running emulator's effective arguments or existing file membership. No conversion was executed. |
| [DOMAIN_UNDEFINE_TPM / KEEP_TPM](../../vendor/libvirt.org/go/libvirt/domain.go), lines 137–138 | Lifecycle mutation flags, not inventory or export operations. They are unsuitable for resolving an unknown path. |

No specialized TPM state locator/enumerator was found in the pinned Go API.
That is a finding about this inspected interface, not a claim about every future
libvirt version. Existing string-based XML methods can carry explicit-source
XML; adding that declaration does not require a new Go binding version.

## Why alternate XML flags do not solve the omission

The public API defines `INACTIVE` as next-boot persistent configuration; without
it, active-domain XML describes the running configuration. `UPDATE_CPU` changes
CPU presentation. `SECURE` includes sensitive configuration and is rejected on
read-only connections. `MIGRATABLE` adjusts XML for migration, can remove or add
compatibility details, is also rejected on read-only connections, and may return
XML unsuitable for ordinary schema validation. None promises a TPM file list.
[Official GetXMLDesc API](https://libvirt.org/html/libvirt-libvirt-domain.html#virDomainGetXMLDesc).

The upstream reason is specific. `qemuTPMEmulatorInitPaths` fills a missing
`source_path` from driver configuration and VM identity without changing the
default source type. Its command builder uses that resolved path for swtpm's
`--tpmstate` argument. The profile-update path can persist the profile name
independently of source declaration. Thus seeing `default-v1` does not establish
that the path is public.
[libvirt 12.0.0 qemu_tpm.c](https://raw.githubusercontent.com/libvirt/libvirt/v12.0.0/src/qemu/qemu_tpm.c),
`qemuTPMEmulatorInitPaths`, `qemuTPMVirCommandSwtpmAddTPMState`,
`qemuTPMEmulatorUpdateProfileName`.

`virDomainTPMDefFormat` emits emulator `<source>` only when `source_type` differs
from `DEFAULT`; its predicate does not depend on SECURE, INACTIVE or MIGRATABLE.
Public XML flags are converted into separate internal format flags, with no
public STATUS mapping. Consequently the reviewed implementation continues to
omit an implicit source even when its internal path is known.
[libvirt 12.0.0 domain_conf.c](https://raw.githubusercontent.com/libvirt/libvirt/v12.0.0/src/conf/domain_conf.c),
`virDomainTPMDefFormat` and `virDomainDefFormatConvertXMLFlags`.

QEMU's private TPM status formatter writes a shared-storage migration capability
boolean, not the state path or directory members. Its ordinary domain formatter
selects the live or persistent definition and delegates formatting. Scraping a
private status file is therefore neither a public API nor an evidenced solution
to this omission.
[libvirt 12.0.0 qemu_domain.c](https://raw.githubusercontent.com/libvirt/libvirt/v12.0.0/src/qemu/qemu_domain.c),
`qemuDomainTPMPrivateFormat` and `qemuDomainFormatXML`.

For Virmill, [coldStateInspection](../../internal/backend/libvirt/cold_state_linux.go)
already reports an unresolved TPM path when absent. Its
[coldTPM parser](../../internal/backend/libvirt/cold_state_xml.go), line 351,
accepts explicit canonical `file`/`dir` sources without inventing missing ones.
Keep that unresolved outcome when another XML view provides no new authority.

## Explicit source support is a creation option

Libvirt documents emulator `<source>` since **10.10.0**, requiring **swtpm 0.7
or later**. `dir` identifies a directory; `file` identifies a single file or block
device. Without it, libvirt chooses storage. With it, the caller must prevent
multiple VMs/emulators sharing that state. `persistent_state`, available since
7.0.0, controls retention for a transient domain's power-off/undefine; it does
not name files. Profile configuration is separate and is established at initial
TPM creation. These declarations are not inventories.
[Official TPM XML documentation](https://libvirt.org/formatdomain.html#tpm-device).

For a **new, separately authorized identity**, a future reviewed recipe could
include this illustrative fragment, under an independently approved storage
root; the path below is synthetic and is not a libvirt default:

```xml
<tpm model='tpm-crb'>
  <backend type='emulator' version='2.0' persistent_state='yes'>
    <source type='dir' path='/synthetic/new-vm/tpm-state'/>
  </backend>
</tpm>
```

That recipe would need explicit allocation/initialization ownership, supported
native versions, permissions and labels, exact definition reconciliation, and a
durable state-location binding. Virmill's current creation firmware intent does
not expose that path. The architecture owner must define its compatibility and
recovery semantics before implementation.

Adding `<source>` to the existing seeded guest is a configuration mutation,
not discovery. A different or guessed location could select unrelated or newly
initialized state. A declaration-only edit cannot prove that its chosen path is
the source of the preserved marker. Retain the existing guest and its unresolved
capture status while deciding how to establish that association.

## Candidate evidence for an existing implicit source

QMP `query-tpm` reports model and backend options; emulator options identify a
chardev. `query-chardev` can identify the corresponding transport. These describe
the connection to the emulator, not its persistent storage members.
[Official QMP reference](https://www.qemu.org/docs/master/interop/qemu-qmp-ref.html#command-query-tpm).
Libvirt's monitor API permits state queries but requires domain write permission;
a fixed query is still not a new TPM inventory contract.
[Official QEMU monitor API](https://libvirt.org/html/libvirt-libvirt-qemu.html#virDomainQemuMonitorCommand).

The upstream swtpm launch path logs the emulator command before starting it and
constructs its storage argument from the internal source path. This makes a
domain/run-bound launch record or a correctly identified running swtpm process
a possible source of **candidate location evidence**.
[libvirt 12.0.0 qemu_tpm.c](https://raw.githubusercontent.com/libvirt/libvirt/v12.0.0/src/qemu/qemu_tpm.c),
`qemuTPMEmulatorStart` and `qemuTPMEmulatorBuildCommand`.

This review recommends the following conditions for any future host resolver;
they are design requirements, not properties already established by the probe:

* Bind evidence to the authorized connection, VM UUID, domain configuration and
  exact run. A matching process name, historical log line, familiar directory
  layout or UUID-shaped basename alone is insufficient. A stopped guest has no
  current emulator process from which to obtain fresh runtime arguments.
* If process evidence is used during a separately authorized run, verify process
  identity/generation, executable, transport association and mount context, then
  parse only a bounded allowlist of storage arguments. Do not expose unrelated
  arguments or secret values. Open-FD lists are corroboration, not proof that
  every required state file is currently open.
* Re-establish the exact storage root/object under a host policy after confirmed
  shutdown, with writer exclusion and restart prevention for the capture window.
  Resolve paths through held descriptors without following symlinks; retain
  identities and detect replacements. Conflicting, stale or missing provenance
  remains unresolved rather than falling back to a constructed default path.

## Location and complete inventory are separate

Swtpm's documented `--tpmstate` can use directory or single-file storage, with
backend-specific locking defaults. The directory form stores multiple state
files. The option describes storage, not an API that enumerates every recovery
dependency. Do not assume a lock flag or a copied filename alone supplies a
consistent capture.
[Official swtpm 0.10.1 manual](https://github.com/stefanberger/swtpm/blob/v0.10.1/man/man8/swtpm.pod).

A metadata-only inventory can enumerate names, object types, sizes, link counts,
ownership/labels and generation information without reading state contents.
It should operate only beneath the independently resolved root, be bounded and
deterministic, hold relevant objects, and recheck the complete tree and lifecycle
before publication. Unknown entries, symlinks, hardlink aliases, mount escapes,
special files, incomplete enumeration and concurrent changes need explicit
handling or refusal. A known socket/lock file must be classified explicitly;
it cannot be silently dropped merely because it is empty or looks temporary.
A block-device source needs its own supported inventory/capture boundary.

These are proposed safeguards. Metadata can establish an observed membership
set under those conditions; it does not verify member contents or prove a
complete hardware capture. Secret references, firmware/NVRAM, persistent XML,
disks/backings and other source dependencies still belong to the complete
capture contract. Byte verification and independent restore/guest checks remain
later authorized stages. The seeded/preserved marker does not replace them.

## Checks performed

Read the pinned Go bindings, local libvirt schema and linked official sources;
no libvirt API was called. The local schema's
`tpm-backend-emulator-source` accepts `file` with an absolute file path and `dir`
with an absolute directory path. Five synthetic documents were passed on stdin
to `xmllint --noout --nonet --relaxng /usr/share/libvirt/schemas/domain.rng -`:
explicit file and directory sources were accepted; missing type, relative path
and `unix` source for an emulator backend were rejected as expected. **Five
schema assertions passed, zero failed, zero skipped.** This checks installed
grammar only, not initialization or runtime support.

Some official web URLs returned fetch errors; the conclusions above use only
the official pages/tagged source actually retrieved and the installed files.
No Fedora source URL or third-party interpretation was used. No remote call or
TPM byte read occurred, and no production file, shared contract or evidence
ledger was changed. The only deliverable file is this review. SNAP-01 and BAK-01
remain open for complete native capture and independent recovery evidence.
