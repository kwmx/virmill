# Cold auxiliary-state XML fixtures

All four XML files are generated fixtures. Their `/fixture/` paths are strings
for extraction tests and are not opened. They are not captures from a host and
do not qualify snapshot, backup, guest recovery or TPM encryption behavior.

| Fixture | Declared layout |
|---|---|
| `bios-no-aux.xml` | No explicit loader, NVRAM or TPM |
| `rom-tpm12-default.xml` | ROM loader and emulator TPM 1.2 with unknown default state location |
| `uefi-tpm20-dir.xml` | Raw pflash and text NVRAM, directory TPM state, encrypted secret reference and distinct profile source/name |
| `uefi-tpm20-file.xml` | Qcow2 pflash and file-source NVRAM, file TPM state, profile and disk encryption secret reference |

The shape follows the official [firmware](https://libvirt.org/formatdomain.html#bios-bootloader)
and [TPM](https://libvirt.org/formatdomain.html#tpm-device) documentation and the
installed native schemas from `libvirt-libs-12.0.0-3.fc44.x86_64`. All four files
passed `xmllint --nonet --noout --relaxng /usr/share/libvirt/schemas/domain.rng`
with their fixture path as the final argument. A preliminary Python validation
attempt could not run because system Python lacked `lxml`; the completed
validation used the installed `xmllint` instead.

Native schema SHA-256 values inspected for this implementation:

| Schema | SHA-256 |
|---|---|
| `domaincommon.rng` | `45348d9a50b564d2a64d439b3f2659f34ce120c4140cd08cd61bae72d717feed` |
| `storagecommon.rng` | `6e96c1b93e909bfbdc37f02c17e647ee8bb4a06abf5558a0872fec219935cef5` |
| `basictypes.rng` | `88ad93bfa89584e1fa9b60aa5a5200c2aa8da27be93f84902d970c04064e6304` |

Tests derive malformed variants in memory. These intentionally invalid variants
are parser rejection evidence and are not native-schema fixtures. Unknown
extension metadata is retained in the source and excluded from secret lookup.
