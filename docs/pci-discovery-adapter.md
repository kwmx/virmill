# Read-only PCI discovery adapter

`Provider.InspectPCI(context.Context, uri)` implements the platform-neutral
`domain.PCIInventoryProvider`. It opens an explicit `qemu:///system` or
`qemu:///session` connection using the official Go binding's read-only API and
enumerates native PCI node devices. Other connection URIs, including the test
driver, are refused by the production entry point. The application service owns
CLI/TUI dispatch and capability presentation.

The adapter returns the native device name, canonical PCI address
(`dddd:bb:ss.f`), hexadecimal vendor/product IDs (`0xhhhh`), available labels,
driver, optional IOMMU group and group member addresses, and optional NUMA node.
Fields follow the [libvirt node-device format](https://libvirt.org/formatnode.html#pci).
Labels are display data and are never commands. A missing driver is an empty
string; group and NUMA absence are JSON `null`. An absent NUMA node attribute or
the conventional unknown value `-1` also remains null. Group member arrays are
empty when no group is reported. The adapter emits explanations for unavailable
driver, group and NUMA observations.

Discovery does not determine whether a device can safely be assigned to a VM.
The output always carries that limitation. Group presence does not certify DMA
isolation, exclusive ownership, host-use checks, reset behavior or successful
passthrough. The implementation contains no detachment/rebinding, device reset,
VM assignment, host file write, subprocess, bootloader/initramfs edit or ACS
override operation. Existing guest PCI configuration is unaffected.

Every device requires one native PCI capability, an exact native/XML name match,
complete address and vendor/product identities, and unambiguous scalar fields.
An observed IOMMU group must contain its own device, unique valid member
addresses, and a consistent member set across all enumerated members. Missing
members, contradictory groups and duplicate names/addresses cause an error and
discard the entire result. This catches detectable partial observations; no
missing members or unknown facts are synthesized. Missing optional group/NUMA
information alone is not an inventory failure.

Parsing rejects foreign namespaces, duplicate attributes, duplicate selected
fields, DTD directives, unexpected processing instructions, malformed XML,
numeric overflow and terminal/control characters in displayed text. Unused
native fields, such as SR-IOV and PCI Express metadata, may be present but do not
become assignment claims. XML is neither reconstructed nor sent to a mutating
libvirt API.

Bounds are 4,096 devices, 256 KiB XML per device, depth 32, 16,384 XML elements,
and 8 MiB minus 128 KiB for both aggregate XML input and serialized response.
Names/drivers are limited to 256 bytes and vendor/product labels to 1,024 bytes.
The response check includes JSON escaping. Cancellation is checked before
connecting, between observations and before returning. Libvirt C calls are
synchronous and cannot be interrupted by a Go context once entered. The native
binding allocates its returned device list/XML before application bounds can
be checked; these checks bound subsequent processing and returned data, not
libvirt's internal allocation. All acquired native handles are freed on exit.

Libvirt does not provide one atomic transaction spanning node enumeration and
all XML reads. Device hotplug, driver changes or permission filtering can make
an observation stale or hide devices which no visible group references.
Unsupported, denied or failed native reads are returned as errors, never an
invented empty success. A successful empty enumeration means only that the
selected backend returned no visible PCI devices. Re-run discovery after access
or topology changes. Physical-host evidence is still required for DEV-03.

Focused offline checks use the pinned toolchain:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -tags libvirt_dlopen ./internal/backend/libvirt -run 'TestPCI|TestReadOnlyPCI|FuzzPCI' -count=1 -v
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -tags libvirt_dlopen ./internal/backend/libvirt -run '^$' -fuzz '^FuzzPCIInventoryXML$' -fuzztime=5s -parallel=2
```

Parser tests cover malformed and conflicting facts, boundedness, native read
failures and cancellation. The native test uses generated XML with libvirt's
in-memory test driver and makes only read calls. It confirms PCI-only filtering
and native group/NUMA readback; the installed test driver drops configured
driver names, so it does not establish native driver observation. See
`tests/fixtures/devices/pci/README.md` for provenance. This documentation records
adapter behavior and test scope, not physical-host or release certification.
