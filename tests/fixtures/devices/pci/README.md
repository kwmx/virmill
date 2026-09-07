# PCI discovery fixtures

These are generated, fictional node-device observations. They were authored for
Virmill's read-only PCI adapter; they are not captures from a host and do not
qualify physical hardware.

`group-function-0.xml` and `group-function-1.xml` describe two PCI functions in
group 12, with a driver and NUMA node zero. The first also has an SR-IOV address
which must never be confused with an IOMMU member. `unknown-topology.xml` has no
driver, group or NUMA facts. Its missing values must remain unknown.

The field structure follows the official [libvirt node-device format](https://libvirt.org/formatnode.html#pci).
Tests construct invalid variants in memory to cover duplicate and malformed
identities, foreign namespaces, group contradictions, controls, directives,
numeric overflow and response bounds.

The native integration test composes these definitions and a USB definition into
a temporary `<node>` configuration for libvirt's [in-memory test driver](https://libvirt.org/drvtest.html).
It opens that connection read-only and checks native PCI filtering, XML decoding,
group membership, explicit unknowns and unchanged repeat observations. The
default native fixture has no PCI devices. The installed native test driver
omits configured driver names in XML readback, so driver observation remains
fixture evidence here. No system/session connection is opened by these tests.
