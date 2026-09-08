# Libvirt 12.0.0 implicit TPM state resolution

Date: 2026-09-08. Read-only source investigation for SNAP-01, BAK-01 and SEC-01
prerequisites. This is an architecture input, not an accepted resolver or capture
implementation. [ADR 0022](../adr/0022-authenticated-auxiliary-inventory.md)
continues to refuse missing native TPM sources.

**The default pathname is deterministic for a known libvirt build and daemon
configuration, but public XML and a version number do not establish all those
inputs or prove that the resulting directory contains the guest's last-used
state.** A narrowly qualified installed helper could verify build and filesystem
metadata without reading TPM bytes. It would still need independently bound
source provenance, including resolution of the omitted-source ambiguity below,
before treating a derived directory as authoritative for cold recovery.

The existing [native API review](native-tpm-state-resolution.md) and
[swtpm lock-selection review](libvirt-swtpm-lock-selection.md) remain applicable.
This investigation does not repeat lock-policy analysis or extend producer
exclusion evidence. The normative requirement is to account for every required
firmware/TPM member and secret dependency, without presenting incomplete state as
complete recovery. [Storage specification](../../virmill-v1-spec/docs/07-storage-snapshots-backups.md).

**Path construction and inputs**

`qemuExtTPMInitPaths` passes the driver storage root, domain UUID and resolved TPM
version into `qemuTPMEmulatorInitPaths`. Only a missing `source_path` is filled.
`qemuTPMEmulatorStorageBuildPath` produces the following path; unknown/default
version fails. It neither changes `source_type` nor probes directories to choose
one. The VM name affects log naming, not this state pathname. Model, profile,
encryption-secret UUID and persistent-state policy are not additional components.
[Tagged qemu_tpm.c:57](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_tpm.c#L57),
[initialization:951](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_tpm.c#L951).

```text
<swtpmStorageDir>/<canonical domain UUID>/tpm1.2   version 1.2
<swtpmStorageDir>/<canonical domain UUID>/tpm2     version 2.0
```

| Driver mode | `swtpmStorageDir` selected by the constructor |
|---|---|
| System, privileged, no embedded root | `LOCALSTATEDIR/lib/libvirt/swtpm` |
| Session, unprivileged, no embedded root | `virGetUserConfigDirectory()/qemu/swtpm` |
| Embedded root supplied | `<embedded root>/lib/swtpm` |

These are separate constructor branches, with the embedded branch first.
`swtpmStateDir` is a different runtime/socket directory; `RUNSTATEDIR`, the
socket path and log directory are not the persistent storage root. The
constructor initializes TPM UID/GID from `tss` with root fallbacks for privileged
mode, or inherited credentials for session mode. `virQEMUDriverConfigLoadSWTPMEntry`
only reads `swtpm_user` and `swtpm_group`. No `swtpmStorageDir` configuration-file
override exists in this reviewed upstream loader. `shared_filesystems` classifies
storage; it does not relocate it. [Tagged qemu_conf.c:119](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_conf.c#L119),
[SWTPM settings:1214](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_conf.c#L1214).

The build generates `LOCALSTATEDIR` in `configmake.h`. Meson's `system` option
selects `/var`; otherwise it uses the configured prefix/localstatedir, with a
specific `/usr/var` to `/var` adjustment. Thus `/var/lib/libvirt/swtpm` is supported
by a particular build configuration, not by the release number alone. A
distribution rebuild or patch must be included in the qualification.
[Tagged meson.build:55](https://github.com/libvirt/libvirt/blob/v12.0.0/meson.build#L55),
[generated constant:151](https://github.com/libvirt/libvirt/blob/v12.0.0/meson.build#L151).

On Unix, `virGetUserConfigDirectory()` appends `libvirt` to GLib's
`g_get_user_config_dir()`. GLib documents XDG configuration-directory selection
and caching. These are the daemon process's resolved inputs; the helper's HOME,
XDG variables or present-day login configuration cannot substitute for them.
The GLib documentation is explanatory, not attestation of any installed GLib
build. [Tagged virutil.c:598](https://github.com/libvirt/libvirt/blob/v12.0.0/src/util/virutil.c#L598),
[GLib configuration directory](https://docs.gtk.org/glib/func.get_user_config_dir.html).

`qemuStateInitialize` constructs the configuration, loads its `qemu.conf`, then
validates and defaults it. `qemuStateReload` reloads domain definitions using the
existing configuration object; it does not reload that driver configuration.
`qemuConnectOpen` checks embedded root and mode-specific URI paths. These facts
support restricting a future adapter to a freshly identified system daemon;
they do not attest which process, loaded driver module or mount namespace an
installed client actually reaches. [Tagged qemu_driver.c:570](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_driver.c#L570),
[reload:940](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_driver.c#L940),
[connection:1062](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_driver.c#L1062).

Missing emulator version is resolved during QEMU postparse: x86 `tpm-tis` gets
1.2; other model/architecture combinations get 2.0. Default model is `tpm-spapr`
on PPC64 and `tpm-tis` otherwise. A helper should consume a fresh explicit native
version and refuse unresolved values, rather than repeating these defaults from
a guest label. [Tagged qemu_postparse.c:713](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_postparse.c#L713).

File versus directory storage is selected by source type, not TPM protocol
version. `FILE` uses `backend-uri=file://...`; `DIR` and `DEFAULT` use `dir=...`.
Explicit file support has a separate swtpm capability check. Newer swtpm or TPM
2.0 does not silently switch an implicit source to file storage. The setup
command's file URI support and TPM-version arguments are likewise separate.
[Tagged qemu_tpm.c, command construction](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_tpm.c#L673).

**What XML establishes, and the omitted-source ambiguity**

The schema permits an explicit emulator source only with `type='file'` or
`type='dir'` and an appropriate absolute path. The formatter emits `<source>`
only for non-default source types, so native XML can omit a computed path even
after initialization. [Tagged TPM schema](https://raw.githubusercontent.com/libvirt/libvirt/v12.0.0/src/conf/schemas/domaincommon.rng),
[tagged domain_conf.c, TPM parser and formatter](https://raw.githubusercontent.com/libvirt/libvirt/v12.0.0/src/conf/domain_conf.c).

There is a further source-level counterexample to assuming omission proves
default derivation. `virDomainTPMDefParseXML` reads source `type` without
`VIR_XML_PROP_REQUIRED` and stores a supplied `path`. A missing optional enum
becomes zero (`DEFAULT`), while `InitPaths` preserves a non-null path. Therefore
this fragment can produce a default-typed, non-default-path internal definition:

```xml
<tpm model='tpm-tis'>
  <backend type='emulator' version='2.0'>
    <source path='/administrator-selected/state'/>
  </backend>
</tpm>
```

The fragment is schema-invalid, but ordinary definition adds schema validation
only when `VIR_DOMAIN_DEFINE_VALIDATE` is requested. The generic and QEMU TPM
validators do not add a source-type/path consistency check. The formatter
subsequently omits that source. These are direct source observations, not an
executed native reproducer. XML copies, persistence and shutdown transitions
must be included in any later lifecycle reproduction; no claim is made here
about an observed guest using the fragment.
[Optional enum handling:463](https://github.com/libvirt/libvirt/blob/v12.0.0/src/util/virxml.c#L463),
[generic TPM validation:3231](https://github.com/libvirt/libvirt/blob/v12.0.0/src/conf/domain_validate.c#L3231),
[QEMU TPM validation:5349](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_validate.c#L5349),
[definition flags:6459](https://github.com/libvirt/libvirt/blob/v12.0.0/src/qemu/qemu_driver.c#L6459).

Consequently, validating the returned XML does not prove all original inputs
were schema-validated or recover suppressed values. A known Virmill creation
record containing the exact submitted, validated declaration could narrow this
problem; an arbitrary adopted VM lacks that evidence. That record must be bound
to the actual lifecycle and current native identity, not merely contain a
plausible UUID/path. The source formatter is also used for internal copies, so a
historical declaration and a current declaration need not locate the same bytes.
[Tagged domain_conf.c, virDomainDefCopy and virDomainObjSetDefTransient](https://raw.githubusercontent.com/libvirt/libvirt/v12.0.0/src/conf/domain_conf.c).

No reviewed public native API returns the effective private `swtpmStorageDir`
and source path together. Public XML flags do not expose internal status;
capabilities describe support, not effective directories. `DomainXMLToNative`
constructs a prospective QEMU command from supplied XML, not a stopped guest's
swtpm storage observation. Private logs/status and process command lines would
need a separately reviewed authority, freshness and lifecycle binding; a string
found there is not a complete inventory. [Existing API trace](native-tpm-state-resolution.md),
[tagged QEMU driver](https://raw.githubusercontent.com/libvirt/libvirt/v12.0.0/src/qemu/qemu_driver.c).

**Minimum qualification for a future installed resolver**

The following are review recommendations, not implemented policy or sufficient
proof of capture. They separate metadata-verifiable inputs from unresolved
source provenance.

1. Bind the actual serving local system daemon and loaded QEMU driver to an
   approved exact build, including downstream source changes, generated
   `LOCALSTATEDIR`, process start identity and filesystem namespace. A package
   label, client library version, binary on disk after an upgrade or service PID
   alone is insufficient. A trusted administrator build manifest plus verified
   loaded artifacts is one possible design; it has not been implemented here.
2. Independently reobserve the exact persistent VM UUID, configuration
   fingerprint, stopped state, managed-save/autostart exclusions and one supported
   emulator source. Require an explicit resolved version. Preserve profile,
   encryption references and all other dependencies. Do not reinterpret an
   explicit file, directory, external or passthrough backend as an implicit one.
3. Establish the derivation's precondition: the actual source path was absent
   when initialized, or obtain an independently authoritative effective path.
   Native source omission alone fails this gate. Resolve historical launch,
   migration, declaration changes and daemon upgrades before claiming the
   directory holds the last-used TPM identity. Present metadata cannot prove
   missing history or discover state abandoned at another path.
4. Require an exact administrator-approved root whose held filesystem identity
   matches the attested daemon view. Reuse bounded beneath-root metadata
   enumeration, ownership/ACL/SELinux checks, member completeness, directory and
   path-generation rechecks and native rechecks. Policy UID/GID must not be
   guessed from the helper's current `tss` lookup: the daemon may have loaded
   different configuration or name-service results. Missing/unverifiable facts
   must leave the source unresolved.
5. Treat producer exclusion as a separate gate. Correct path arithmetic, an
   empty `.lock`, stable timestamps and absence of an obvious process do not
   prove initialized state, authentic guest identity, a complete member set or
   exclusion of every writer. Apply the existing lock review and future capture
   design separately. Path resolution neither authorizes content access nor
   verifies a capture, restore or guest boot.

The build/configuration checks and object metadata could be inspected without
TPM payload reads. They cannot, by themselves, recover an unrecorded effective
path from a stopped daemon definition that suppresses it, or establish the
provenance of matching files. Current explicit-source refusal therefore remains
appropriate. Session/embedded support would require additional daemon-specific
configuration attestation and is outside the present system-only helper.

**Source provenance and performed checks**

All libvirt reads targeted upstream `v12.0.0`. Existing direct-byte SHA256
provenance for `qemu_tpm.c` and `domain_conf.c` is retained in the
[lock-selection review](libvirt-swtpm-lock-selection.md); it was not silently
replaced with a browser-rendered hash. Browser retrieval collapses some blank
lines, so its excerpts are cited by function where exact raw lines were
unavailable. A sandboxed direct Python fetch failed DNS without retrieving
source; no escalation was requested. Web retrieval supplied tagged source and
schema; the read-only GitHub connector supplied the following full text and
reported Git blob IDs. These are blob IDs, not newly calculated SHA256 values.

| Tagged source | Git blob ID reported by the connector |
|---|---|
| `src/qemu/qemu_tpm.c` | `660410bcba09ddec98f1ae6933f27db72be32676` |
| `src/qemu/qemu_conf.c` | `de6e51177a514e5a0de69cba4d372e6265f15cee` |
| `src/qemu/qemu_driver.c` | `3f154969b863cd744a692de9920733b601aa0408` |
| `src/qemu/qemu_postparse.c` | `8940cb09b34fda98873e1f2d084f72eefe1d6654` |
| `src/qemu/qemu_validate.c` | `184c23d307d2877f9335044718f6d2844d62cbcc` |
| `src/qemu/qemu_extdevice.c` | `28cea52980577d3ce980d46b2e29456716993458` |
| `src/conf/domain_validate.c` | `7346a61731df450f80ba23e4d79f15c98276145a` |
| `src/util/virutil.c` | `fb64237692bbc729b1f140c3f863494eccdc3739` |
| `src/util/virxml.c` | `274f0725981b5b9dadc935b4727398006911b277` |
| `meson.build` | `2f807bc3a2f8ac2afbad5fe0ff30527252576af1` |

The connector returned an empty body for the oversized `domain_conf.c`; no
full-content verification is claimed for that response. Its reviewed functions
came from tagged web excerpts and the previous provenance record. No downloaded
code was executed, installed or retained as a checkout. Local documentation
whitespace and link-target checks passed. No test, libvirt call, guest operation,
SSH, host-service action or TPM byte read occurred. This document is the only
edited file; there are no new acceptance passes or release qualifications.
