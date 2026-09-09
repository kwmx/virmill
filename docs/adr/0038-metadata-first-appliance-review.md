# ADR 0038: Show appliance settings before image preparation

Status: implemented; software and native evidence recorded separately.

The import workflow must retrieve and explain appliance settings before asking
for storage or conversion options. Waiting for every disk checksum to show a
CPU count was an implementation mistake, not a scope decision. The specification's
shared-service and integrity requirements remain authoritative.

`import describe` is a read-only service operation. It reads bounded tar headers
and OVF metadata and seeks past ordinary disk payloads. It rejects unsafe paths,
links, sparse entries, malformed descriptors, missing disks and truncation. Its
report explicitly says `metadata-only` and `not-verified`, with empty hashes.
Full inspection, hashing, plan review and apply-time revalidation remain mandatory
in preparation. A metadata report cannot authorize or supply a verified receipt.

Selecting an OVA opens Review appliance automatically. VM name, CPU and memory
start from unambiguous source declarations; absent values have labeled suggestions.
The summary shows source OS, disks, controller and device hints. Advanced settings
is one action away and works before a destination exists. Changes survive the
in-session preparation handoff and return navigation. Destination changes do not
invalidate hardware choices; source identity and disk mappings do. Incomplete
wizard drafts are not yet automatically persisted across application restarts.

Optional metadata preserves exact declared fields. Known VirtualBox OS metadata
can be more specific than its generic OVF OS description: show both when they
differ, prefer the explicit vendor label, and do not infer OS from filenames.
Namespaces and ambiguous declarations are checked. Source NVRAM references are
shown as included only when that referenced member exists. Inclusion does not mean
its state can be restored. USB/audio descriptions are observations, not promises
that those devices will be recreated. Host networks and firmware still require
explicit reviewed choices; no test-host defaults are introduced.

Earlier prepared receipts remain valid. New optional system/device metadata is
covered by the prepared-import schema; the full source configuration remains
available for review. This improves IMP-01, IMP-03, UX-01 and UX-02 without promoting
any full acceptance claim or reducing the 71 required scenarios.
