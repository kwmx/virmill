# Cold source inventory XML fixtures

These are generated XML fixtures. Their file, device, pool, volume, endpoint and
runtime identifiers do not describe an accessed host. The parser does not open
them. `DO_NOT_ECHO_*` markers test that opaque dependency content is omitted from
the projection; they are synthetic values, not credentials.

| Fixture | Declared layout |
|---|---|
| `local-chain.xml` | File/volume/file backing chain, explicit terminator, unknown volume format, empty CDROM and read-only floppy |
| `unresolved.xml` | Block/network disks, secret UUID, filesystem, PCI hostdev, shared memory and QEMU command line |
| `structured-source.xml` | Disk encryption and external data store, plus a named volume in direct mode |
| `configuration-only.xml` | Disk with ordinary controllers, input, video, balloon, watchdog, panic, hub and sound/audio configuration |

All four fixtures passed the installed native schema from
`libvirt-libs-12.0.0-3.fc44.x86_64`, using `xmllint` supplied by
`libxml2-2.12.10-6.fc44.x86_64`:

```sh
xmllint --nonet --noout --relaxng /usr/share/libvirt/schemas/domain.rng tests/fixtures/protection/cold-source-xml/local-chain.xml tests/fixtures/protection/cold-source-xml/unresolved.xml tests/fixtures/protection/cold-source-xml/structured-source.xml tests/fixtures/protection/cold-source-xml/configuration-only.xml
```

`TestColdSourceXMLNativeSchemaFixtures` repeats this check when the installed
validator and schema exist, and reports blocked skips otherwise. Current primary
references are libvirt's [disk/backing source format](https://libvirt.org/formatdomain.html#hard-drives-floppy-disks-cdroms),
[filesystem format](https://libvirt.org/formatdomain.html#filesystems) and
[shared-memory format](https://libvirt.org/formatdomain.html#shared-memory-device).
The checked native schema hashes are:

| Schema | SHA-256 |
|---|---|
| `domaincommon.rng` | `45348d9a50b564d2a64d439b3f2659f34ce120c4140cd08cd61bae72d717feed` |
| `storagecommon.rng` | `6e96c1b93e909bfbdc37f02c17e647ee8bb4a06abf5558a0872fec219935cef5` |
| `basictypes.rng` | `88ad93bfa89584e1fa9b60aa5a5200c2aa8da27be93f84902d970c04064e6304` |

Tests derive malformed, unsupported and over-limit variants in memory. Those
variants are parser tests, not claims of native schema validity. No fixture is a
capture, independently recovered VM or complete source graph.
