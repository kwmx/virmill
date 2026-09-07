# Generated BIOS multi-disk probe

This fixture contains original assembly and deterministic data, with no supplied
guest media. The boot sector reads the first sector of BIOS hard disk `0x81` and
compares its marker. It prints `VIRMILL SECOND DISK PASS` or `FAIL` through BIOS
video and COM1, then halts without disk writes. It is a disk-path probe, not a
Linux/Windows guest or a provisioning/readiness recipe. Boot evidence must come
from an explicitly authorized real QEMU/KVM guest; compiling these files alone
does not qualify hardware.

Run `build.py --destination NEW_PRIVATE_DIRECTORY` as an ordinary user on the
designated Fedora fixture host. It requires installed GNU binutils, QEMU image
tools, bubblewrap and prlimit and records exact package/tool hashes. At least
16 GiB free is required. Existing destinations are refused. Raw source files and
archive members are preserved; no VM, pool, network or device is created by the
builder. Image conversion and comparison run in a bounded namespace sandbox with
only the raw inputs and private output exposed.

The boot disk is 16 MiB. The data disk is 3 GiB with a marker and a 1 GiB generated
nonzero payload, allowing an actual upload to be observed for crash testing.
Both become `twoGbMaxExtentSparse` VMDKs; the larger disk spans two extent files.
The OVA includes both descriptors, every extent, OVF disk IDs/controller positions
and a SHA-256 manifest. `--payload-mib 16` or `256` can produce smaller diagnostic
fixtures, but those are not interchangeable with the recorded 1024 MiB upload
fixture. QEMU content IDs may vary; the recipe records each actual archive/member
hash rather than promising byte-identical VMDK containers.

GNU binutils 2.46.1 adds property notes to the assembled object. The builder links
at `0x7c00`, extracts only the relocated `.text` section and requires exactly 512
bytes ending in `55 aa`. This avoids appending host ELF metadata to a boot sector.
The source and raw marker bytes are independently hashable.

Use the resulting `probe.ova` in Virmill's actual `import inspect/prepare` workflow
with system ID `virmill-multidisk-probe`, source format `vmdk` and explicit virtual
limits of 16 MiB (`boot`) and 3 GiB (`data`). Later VM creation requires fresh plans,
an owned disposable pool, BIOS, explicit hardware/device policy, no NICs, and
both disk mappings with boot orders 1 and 2. Never replay old plan digests.

References for the fixture's interfaces:
[QEMU VMDK subformats](https://www.qemu.org/docs/master/system/images.html#disk-image-file-formats)
and [SeaBIOS INT 13h read handling](https://github.com/coreboot/seabios/blob/master/src/disk.c).
These describe interfaces; observed test results belong in the evidence ledger.
