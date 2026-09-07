# ADR 0012: Native domain-capability access during creation preview

Status: accepted, 2026-09-07. Scope remains the complete local 1.0 contract.

The first owner-authorized Fedora 44 QEMU-driver creation preview failed before
allocation: libvirt 12.0.0 rejects `virConnectGetDomainCapabilities` on a read-only
connection. The native in-memory fixtures had not exercised this driver behavior.
Source preparation had already succeeded and remains usable.

Open a writable connection for creation preflight's observations and capability
queries. This is an API access requirement, not authorization to mutate resources
in preview. Keep allocation, upload and definition behind durable apply, and keep
the coordinator unprivileged. Do not substitute the in-memory driver, grant broad
new privileges automatically, or skip capability validation.

Existing inventory-only methods continue using read-only connections. No protocol,
database schema, acknowledgement or locked workflow changes. The correction needs
the actual disposable-host preview/creation rerun as evidence; passing local
coordinator tests alone cannot establish that it fixes the native workflow.
