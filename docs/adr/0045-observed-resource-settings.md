# 0045 — Read current resources before offering edits

Status: implemented; package integration and native stopped-VM TUI edit/readback passed.
See [scoped evidence](../evidence/observed-resources-run.md); full acceptance remains open.

The resource editor previously opened blank CPU/RAM fields, requiring users to
remember values and discover unsupported layouts during preview. The TUI
specification requires current, next-boot and requested values to be distinct.

`vm.resources.show` observes one exact local VM through the shared service. A
bounded XML reader returns separate live and persistent configuration values,
including current versus maximum vCPUs and balloon target versus configured
maximum memory. Byte counts remain exact. A malformed field does not discard an
independently readable field; it carries an explanation instead of a guessed
default. These are configuration values, not utilization or guest RSS.

Edit eligibility comes from the existing preservation-aware resource adapter,
independently for CPU and memory. The read service does not expand mutation
support. Dependent topology/NUMA/balloon/hotplug settings stay visible while a
basic edit is refused with its reason. Running guests need separate reviewed
shutdown; managed-save state needs restoration and shutdown first. Available
host RAM and a successful next boot are not inferred from readable XML.

The read view is a separate domain contract rather than another field on `VM`.
Adding observations to `VM` would change durable provider fingerprints and old
job semantics. Existing mutation recipes and schema versions remain unchanged.
CPU/RAM plan reviews now include observed before/after values, checked against
the original fingerprint. Stale plans continue to fail safely.

The TUI populates requested values from this fresh view, sends only changed
supported fields, and keeps edits across review/back/error. CLI users access the
same observation with `virmill vm resources show UUID` and make changes with
the existing reviewed `vm set` flow. Neither frontend parses libvirt XML.
