# 0044 — Bind guest tools to configuration while allowing agent connection

Status: implemented; corrected absent-to-installed native Fedora verification,
idempotent repeat and independent native agent ping passed on source f371fa8.

The first Fedora 44 installation fixture installed and started the agent, then
stopped at verification with `SOURCE_CHANGED`. Replacing only the observed live
agent target's `state='connected'` with `state='disconnected'` reproduced the
exact reviewed provider fingerprint. The installation's own expected connection
change was the complete cause of the failure. Its applied receipt and original
recovery-required job remain retained; they are not reinterpreted as success.

New exact built-in guest-tools recipes use frozen input version 2. They retain
the original provider fingerprint and additionally bind a tools-specific VM
fingerprint. It preserves every VM field and every byte of persistent XML. In
live XML only the unique unnamespaced unix/virtio QEMU agent target's observed
connected/disconnected attribute is excluded; other attributes and all bytes
outside that target's start tag remain significant. Malformed or ambiguous
agent targets are refused. Runtime domain ID, disk/NIC paths, channel address,
host socket, labels and unrelated devices remain bound.

Ordinary guest recipes and stored version 1 plans retain exact fingerprint
comparison. The global provider fingerprint is unchanged. Version 2 is accepted
only for an exact compiled built-in tools recipe; its new digest is frozen into
the approved input. Neither version replays a stage with durable intent.

This follows configuration preservation and durable execution requirements;
it does not relax host identity checks or permit arbitrary recipe normalization.
