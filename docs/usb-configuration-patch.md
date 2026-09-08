# Exact USB configuration patches

`xmlpatch.USBDevice` prepares a persistent domain XML change; `USBDeviceXML`
prepares the corresponding bounded device fragment for a separately authorized
native call. Neither function calls libvirt, discovers USB devices, detaches a
host driver, or proves physical attachment. This is unit-level support for
DEV-01/DEV-02, whose physical USB and reconnect evidence remains required.

The caller supplies all of these observed/requested values:

- `Alias`: `ua-virmill-usb-` followed by one canonical lowercase, nonzero UUID.
  This identifies the VM's assignment record, not the physical USB device.
- `VendorID` and `ProductID`: exactly four lowercase hexadecimal digits each,
  without `0x` prefixes. Both are required along with the current address.
- `Bus`: 1–999; `Device`: 1–127. Bus/device numbers are ephemeral after replug.
  The bus limit deliberately fits the installed libvirt 12 XML schema's
  three-digit unprefixed address representation. Larger observed buses refuse;
  inventory visibility does not imply a supported attachment topology.
- `Remove`: whether to remove the exact assignment from domain XML.
  `USBDeviceXML` retains the same complete fragment for either value.

The generated profile is fixed:

```xml
<hostdev mode="subsystem" type="usb" managed="yes">
  <source>
    <vendor id="0x04a9"/>
    <product id="0x00ef"/>
    <address bus="3" device="17"/>
  </source>
  <alias name="ua-virmill-usb-12ab3456-7890-1234-abcd-123456789abc"/>
</hostdev>
```

The implementation emits this as one compact fragment. Libvirt documents
domain-wide uniqueness for user aliases with the `ua-` prefix, and notes that
`managed` is ignored for USB; its presence here does not establish host-driver
management. The source policy has no optional-missing or reset override.
[Libvirt domain XML](https://libvirt.org/formatdomain.html#usb-pci-scsi-devices).

Insertion requires one unnamespaced domain and one direct unnamespaced `devices`
container. It checks direct native device aliases and every native USB host
source address. Duplicate aliases or USB addresses, an already occupied requested
identity, and unresolved address-free USB selectors refuse. An unrelated
address-only USB selector is sufficient to detect a collision. Vendor/product
matching alone never selects a new device.

Removal requires exactly one matching alias with the complete vendor/product
and host bus/device identity, `mode="subsystem"`, `type="usb"`, and
`managed="yes"`. Missing, changed or ambiguous identities produce no patched
XML. Unknown selected attributes/children, boot policy, startup/reset overrides
and structured identity leaves also refuse. Known representation changes such
as attribute order, quotes, expanded empty elements and schema-valid hexadecimal
ID spelling can match the same identity. Host addresses allow canonical decimal
or explicit `0x` notation within the stated limits; ambiguous leading-zero
decimal forms refuse.

One optional guest `address type="usb"` may accompany the selected device. Its
bus is 0–999 and its optional port has at most four components, each 1–127.
Guest placement is validated separately and never used as the host selector.
These bounds are an implemented subset, not a claim about every native topology.

All unrelated bytes remain exact, including other hostdev policies, namespaces,
comments, metadata, device order and whitespace. Insertion adds one span before
`</devices>`; an empty `<devices/>` changes only its closing `/>` to a paired
container. Removal deletes only the selected hostdev span, including comments
inside that device. The shared parser enforces 16 MiB, 65,536 nodes and 64 levels,
rejects directives and duplicate attributes, and validates the resulting XML.
Errors return no partial document.

Native inventory freshness, physical serial/port continuity, host-use conflicts,
exclusive consent, live versus next-boot intent, durable operation receipts and
reconciliation belong to the shared USB service and adapter. A stable alias and
an unchanged bus/device tuple alone cannot prove that a reconnected physical
device is the same one. Unsupported policy must remain visible rather than
silently attaching another vendor/product match.
