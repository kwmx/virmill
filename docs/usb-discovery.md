# USB discovery and identity limits

`USBInventoryProvider.InspectUSB(ctx, uri)` enumerates native `usb_device` node
devices on an explicit local `qemu:///system` or `qemu:///session` connection.
It uses a read-only connection and the official libvirt Go binding's
`ListAllNodeDevices(CONNECT_LIST_NODE_DEVICES_CAP_USB_DEV)`, `GetName` and
`GetXMLDesc(0)`. Interface capabilities (`usb`) are not whole USB devices and
are not selected by this adapter. Unsupported discovery or a failed read returns
an error, never an invented empty inventory.

Each result exposes native vendor/product IDs and labels, nullable current
bus/device numbers, observed serial and physical topology, a descriptive match
key, ambiguity and a reason. Bus/device numbers are ephemeral. Native names can
also reflect current enumeration; they are not independently qualified serial
or port identities.

Libvirt documents the top-level node `path` as the device's fully qualified
sysfs path. Its `usb_device` capability describes bus/device and vendor/product;
it does not supply a USB serial field. The adapter therefore enriches only the
exact native path, never searches an arbitrary filesystem tree or parses a
caller-supplied discovery path.
[Libvirt node-device XML](https://libvirt.org/formatnode.html#usb-device)

The sysfs adapter opens the fixed `/sys/devices` root and requires the sysfs
filesystem. It resolves the native relative path beneath that root with
`openat2` and no symlinks, magic links or mount crossings. It reads only these
six attribute names:

| Attribute | Use |
| --- | --- |
| `idVendor`, `idProduct` | Must match the exact native IDs |
| `busnum`, `devnum` | Explicit current addresses; must match native values when present |
| `serial` | Optional exact serial, excluding one kernel output newline |
| `devpath` | Optional observed port chain; a root hub's `0` is not a physical-port fallback |

Linux exposes serial as a read-only optional string, and supplies `devpath` from
the USB device's observed path. Its descriptor attributes format the IDs as four
hexadecimal digits. This adapter reads these existing attributes and never opens
binary descriptors, device nodes or configuration/control attributes.
[Linux USB sysfs implementation](https://github.com/torvalds/linux/blob/master/drivers/usb/core/sysfs.c)
The kernel ABI separately defines `busnum` and `devnum` as bus and device
addresses; they are not a persistence promise.
[Linux USB sysfs ABI](https://www.kernel.org/doc/Documentation/ABI/stable/sysfs-bus-usb)

Every attribute is first opened as `O_PATH` and checked for regular-file type.
Only that held regular inode is reopened through its private `/proc/self/fd`
number, so a FIFO, symlink or device replacement does not trigger special-file
I/O. Reads are bounded independently of sysfs's virtual file size. File identity
is rechecked through the native directory; root and directory identity are
rechecked through their original paths. Two complete native/sysfs projections
must agree, including private inode/change metadata. Observed disappearance,
replacement, contradictory IDs/addresses or unreadable present attributes
refuse the whole inventory.

Missing native numbers remain `null` unless the sysfs reader actually observes
them. A missing native path leaves serial and physical-port evidence empty.
Missing optional serial/devpath attributes remain unknown; malformed present
attributes do not silently become missing data.

Match keys use canonical vendor/product IDs and exact, percent-escaped serial
bytes. Leading/trailing spaces and case are preserved; serials are not trimmed
or case-folded. Duplicate serial keys are marked ambiguous even when different
ports are visible. Discovery never chooses one duplicate or automatically
substitutes a port to conceal duplicate serials.

When serial is unavailable, a topology fallback uses the exact native sysfs path
plus the observed `devpath`. A non-root path's final component must agree with
the observed bus and port chain. The result explicitly requires physical-port
selection. This descriptive key includes current root-bus numbering and may
change after host/controller re-enumeration. It does not invent a controller
identity by removing those numbers. Without serial or usable port evidence,
the match key is empty and the device is ambiguous; vendor/product alone never
becomes a match key.

Bounds are 4,096 native devices, 64 KiB XML per device, and less than 8 MiB each
for aggregate XML across both observations and the final JSON response. Native
XML uses the existing bounded tree with maximum depth 32 and 16,384 nodes;
repeated declarations, directives, foreign namespaces, duplicate relevant
fields and malformed identities are rejected. Labels and serials are bounded
at 1,024 bytes and reject control, format and malformed Unicode characters.
The accepted port chain has at most seven bounded positive components.

The native binding materializes its device list and XML strings before adapter
limits apply. These bounds do not promise native allocation limits. Native C
calls and filesystem reads are synchronous; context checks surround the calls
and final return, but do not interrupt an in-flight syscall. All owned native
handles and descriptors are closed. Two projections are not an atomic snapshot
and cannot guarantee that a device remains present after return.

`TestUSBInventory*` uses generated XML, a read-only native seam and ordinary-user
temporary files. The private fixture root bypasses only the production sysfs
filesystem requirement; no public API or policy accepts a substitute root.
Tests cover nullable unknowns, serial/port evidence, duplicate serials, native
identity aliases, malformed XML, address and inode drift, symlink/FIFO refusal,
bounded reads/responses, cancellation, sanitized failures and cleanup.

This is discovery foundation for
[DEV-01 and DEV-02](../virmill-v1-spec/docs/13-testing-and-acceptance.md).
It does not establish attachment authority, live/persistent attach or detach,
host-use safety, physical reconnect continuity, boot-critical-device exclusion
or USB redirection. Those required integration and physical-hardware checks
remain separate from these adapter tests.
