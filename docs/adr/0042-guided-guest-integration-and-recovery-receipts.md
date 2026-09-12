# 0042 — Guided guest integration and portable recovery receipts

Status: accepted implementation decision; native qualification remains separately recorded.

The owner requires understandable actions across every supported image source and
simple guest-tools setup. The source wizard already describes OVA, ISO and
supported existing disks before preparation, with metadata-derived appliance
settings and explicit defaults where disk/media metadata cannot supply hardware.
Image format support does not imply support for automatic setup of every guest OS.

Existing VMs previously required an externally configured guest-agent channel.
The shared service now offers a stopped, persistent, channel-only configuration
plan. The TUI reads channel status before presenting installation options and
provides focusable setup/shutdown/help actions. CLI `vm guest-agent enable` uses
the same plan. Enabling the high-trust channel requires explicit acknowledgement;
it never implies installed software, a responding agent, or a verified SSH identity.
Guest software remains a separately reviewed operation for implemented profiles.

Version 3 uses the distinct `vm.configure-guest-agent` durable operation. It binds
an observed free virtio-serial controller/port, before fingerprint and preservation
digest. The XML edit inserts only the reviewed devices. Reconciliation permits
only tightly validated native normalization of those additions and preserves
unrelated XML. Version 1 resource and version 2 hardware recipes retain their
original meaning. An uncertain definition is observed, never blindly replayed.
No database migration is required; older binaries lack the new operation handler.

Backup recovery previously required users to transcribe hashes. A typed
`virmill/v1` `BackupReceipt` instead carries exact recovery selectors from a
verified backup proof. The service lists recent receipts for the actor/connection
and permits an exact older operation lookup. CLI and TUI save a strict, private,
exclusive JSON document; receipts contain no credentials. A saved receipt can be
read independently of the original database. It is data, not authority: repository
authentication, exact snapshot/manifest checks and complete restored-member
verification remain mandatory. Recovery first restores files; creating a VM is a
separate reviewed Protection action.

These choices resolve the apparent conflict between convenient automatic setup
and explicit high-trust consent in favor of the safety invariants and shared
contracts, following specification precedence. They do not reduce any of the 71
acceptance scenarios, certify guest hardware, or authorize release publication.
