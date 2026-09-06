# Simulated provider conformance fixture

This executable creates only JSON records inside its sandbox workspace. It does
not contact libvirt or any remote service. Create is idempotent; a requested partial
failure occurs after a durable fake resource record. Restart and reconcile read
that record. Backup, USB and cancellation return unsupported capability.

Build using the repository toolchain from this directory. Tests label its evidence
`simulated-contract`; it never validates real remote management or guest behavior.
