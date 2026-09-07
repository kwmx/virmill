# ADR 0011 — Fixed-resource configuration preservation

Status: accepted routine implementation decision. Full configuration scope and
CORE-03/04/05 remain required. No host mutation or reduced release is authorized.

The original vCPU span edit preserved unrelated bytes but did not check topology,
per-vCPU allocation, pinning, NUMA or managed-save dependencies. RAM was refused.
The shared CPU/RAM adapter now checks these dependencies before accepting a plan.
It supports fixed resources on powered-off persistent VMs, with `next-boot` intent.
`now` and `both`, advanced layouts and managed-save conflicts fail closed; no
implicit shutdown, topology rewrite or saved-state removal occurs.

Memory and currentMemory are updated together when their old byte allocations
agree. Missing currentMemory retains its documented default to memory. Existing
units and attributes are preserved, with checked integer conversion; an inexact
conversion, overflow, structured leaf or unknown dependent policy is refused.
Maximum hotplug memory, separate balloon targets, NUMA, backing policy and memory
devices need dedicated reviewed adapters. vCPU edits refuse topology, tuning,
per-vCPU allocations, automatic placement and dependent attributes. The native
backend repeats the positive maximum-vCPU probe. Parser work has byte/depth/node
bounds; unrelated XML is never rebuilt from a simplified model.

Libvirt's ordinary XML view can omit sensitive settings, including display
passwords. A redefine from that view could erase credentials. Native preflight
therefore privately compares inactive secure XML with the observed ordinary XML
and refuses a difference or unavailable secure read. Libvirt requires a writable
connection handle for secure reads; these preview calls perform no mutation. The
secure XML stays in memory and never enters the plan or error text.

Ordinary opaque XML can itself contain credentials in custom fields. The new plan
therefore persists requested CPU/RAM values, a before fingerprint and an expected
XML SHA-256, not the XML document. Execution re-observes and rebuilds the same
span edit in memory, checking its expected hash before definition. Returned native
definition errors withhold XML values. This protects the new plan boundary; it is
not a claim that every existing inventory, expert setting or diagnostic is redacted.

One durable step records intent before `DomainDefineXMLFlags(VALIDATE)`. State,
source fingerprint, dependencies and secure preservation are checked before plan
acceptance, apply and effect. Public and secure readback must exactly match the
expected XML, and the VM must remain stopped without managed-save state. A returned
acknowledgement alone is insufficient. Benign but unrecognized normalization stays
uncertain rather than discarding unknown settings during comparison. A lost
acknowledgement can reconcile after SQLite reopen from the expected hash and secure
readback, without repeating definition. Cancellation after the native call begins
cannot undo it; a completed verified effect is reported as succeeded.

Libvirt definition offers no atomic external-writer compare-and-swap. Virmill
serializes its own clients and repeats immediate observations, but another manager
can still race the final call. Plans require `exclusive-configuration-writer`
acknowledgement and disclose this limitation. Actual concurrent-writer safety and
external configuration preservation remain unqualified, not inferred from tests.

The new durable operation is `vm.configure-resources`, while the public command is
still `vm set` through `vm.plan`. This deliberately prevents older binaries from
executing the new contract through their unsafe legacy `vm.set` handler. Legacy
unapplied plans require a fresh preview; old uncertain edits retain their locks and
need explicit disposition. SQLite schema 3 is unchanged. Tests simulate a legacy
handler registry and verify refusal of the unknown newer operation.

Tests include exact byte preservation, resource/unit/dependency refusal, secret
boundaries, drift, cancellation, failed definition, journal reopen, schema and
CLI/TUI access. Official native bindings exercise libvirt's in-memory test driver,
including actual synthetic VNC password redaction. No host domain or guest was
started, and no RAM/CPU sizing or real guest behavior is certified.

Primary contracts: [memory allocation](https://libvirt.org/formatdomain.html#memory-allocation),
[CPU allocation](https://libvirt.org/formatdomain.html#cpu-allocation) and
[secure XML retrieval](https://libvirt.org/html/libvirt-libvirt-domain.html#virDomainGetXMLDesc).
The installed libvirt version remains the one recorded in the dependency lock.
