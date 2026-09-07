# Cold firmware and TPM layout extraction

Inspect a VM through the ordinary-user coordinator:

```sh
virmill vm recovery inspect VM_UUID --connection qemu:///system --output json
```

In the TUI, open **Protection → vm recovery inspect** and enter the stable VM
UUID. Both interfaces call the same read-only service. System and session
connections stay separate; remote connection URIs are refused by this core path.
The response includes current state, managed-save/autostart observations, the
configuration fingerprint, the layout below and warnings about unresolved
dependencies. Running guests may be inspected, but the result never permits a
live file copy. Unsupported, ambiguous or canceled inspection returns an error
without a successful layout payload. This command creates no capture or job.

`InspectColdStateXML(raw)` returns a `domain.ColdStateLayout` from an observed
persistent domain definition. It identifies configuration facts and confidential
state dependencies. It does not establish VM state, exclude concurrent writers,
open state files, resolve secret values, grant access or capture anything. The
original persistent XML remains authoritative for recovery configuration.

The projection contains a canonical domain UUID; loader path, ROM/pflash type,
read-only, secure, raw/qcow2 format and stateless flags; NVRAM path, format and
template metadata; and emulator TPM model, version, explicit state source,
persistence policy, profile and encryption secret UUID. TPM profile `source`
and resolved `name` are kept separately. This matches the relevant native
[firmware](https://libvirt.org/formatdomain.html#bios-bootloader) and
[TPM](https://libvirt.org/formatdomain.html#tpm-device) formats.

Only explicit facts are populated. Missing loader/NVRAM defaults, TPM source
path/type, model, version, persistence policy and profile remain empty rather
than being guessed from the distribution, VM name or UUID. Absent NVRAM and TPM
elements are represented by null pointers; an explicitly present NVRAM element
whose path is not resolved retains a non-null layout with an empty path. The
inspection result is never a completeness claim, even when all projected paths
are present.

NVRAM supports a text path or a plain local-file `<source file='…'/>`, with
explicit `type='file'` or the native schema's implicit file type. Mixed text and
source declarations, multiple sources, descriptors, source encryption, slices,
external data stores, block/network/volume sources and other structured sources
are refused. Loader `stateless='yes'` with an NVRAM element is contradictory and
refused. The newer `varstore` layout and the pSeries NVRAM device require separate
adapters and are refused.

TPM backends are restricted to `emulator`, with declared versions `1.2` or `2.0`
and explicit state source types `file` or `dir`. A file declaration does not
prove that the path is a regular file; later access checks must establish that.
Passthrough and externally managed TPM backends are unsupported. Version 1.2
cannot use a CRB model, profile or active PCR-bank selection. Debug/PCR-bank
metadata is validated but not projected; the complete original definition must
be retained when restoring. Other unknown TPM state configuration is refused.

Secret references are UUIDs only. TPM encryption and native disk encryption/auth
references are canonicalized, deduplicated and sorted without querying libvirt
secrets. Native `usage` references are explicitly unsupported until an approved
UUID-resolution adapter exists. TPM encryption values and extra attributes are
rejected; errors do not echo the input path, UUID or secret text. Namespace-owned
extension subtrees and domain metadata remain opaque and are never interpreted
as secret lookup authority.

The parser rejects duplicate attributes, duplicate or foreign relevant
elements, malformed scalar fields, DTDs, extra processing instructions and
ambiguous source layouts. Paths must be canonical absolute POSIX paths, cannot
be `/`, and cannot contain controls or bidirectional formatting characters.
This lexical policy neither follows symlinks nor grants permission to read a
path. Bounds are 1 MiB input XML, depth 32, 16,384 elements, 65,536 tokens, 4,096
bytes per path/scalar, 256 bytes per profile identifier, and 256 distinct secret
UUIDs. Any failure returns an empty layout and an error.

The fixture source and native-schema provenance are in
`tests/fixtures/protection/cold-state-xml/README.md`. Focused checks use the pinned
repository toolchain and vendored dependencies:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -tags libvirt_dlopen ./internal/backend/libvirt -run 'TestColdStateXML|FuzzColdStateXML' -count=1
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -tags libvirt_dlopen ./internal/backend/libvirt -run '^$' -fuzz '^FuzzColdStateXML$' -fuzztime=5s -parallel=2
```

These checks establish extraction, wire compatibility, rejection behavior and
native XML shape. They do not prove safe cold copying, file ownership, stopped
state, swtpm encryption, complete capture or independent recovery. Those require
the separate stopped-state, access-control, capture and restore workflows and
their real-system evidence.
