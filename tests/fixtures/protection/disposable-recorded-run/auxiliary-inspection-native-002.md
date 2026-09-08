# Native auxiliary metadata inspection recipe 002

This parent-owned, single-use successor retains the scope and safety requirements
of [recipe 001](auxiliary-inspection-native-001.md). That earlier native execution
failed in preflight and remains failed in the ledger. It created no domain or
auxiliary state and made no policy replacement. The native capability query was
forbidden on a read-only libvirt connection. Final saved volume XML differed only
in three existing QCOW2 access timestamps; this is not complete byte preservation.

Recipe 002 makes these reviewed changes before its first native execution:

- Its output, state root, administrator root ID, domain name and policy temporary
  files use `002`. It never reuses or removes recipe 001 artifacts.
- The fixed `domcapabilities` read query uses a normal libvirt connection, as the
  API requires. This does not authorize any additional mutation or guest start.
- Volume comparison validates exactly one direct file-volume access timestamp,
  records every observed value and excludes only its numeric bytes from equality.
  All other volume XML, including size/allocation, owner, permissions, path,
  format, modification and change timestamps, remains exact. Pool comparisons
  retain only the already documented allocation/available counter exception.
  Malformed, missing, duplicate, misplaced or decorated atime fields refuse.

The new resources are `/var/lib/virmill-host-helper/auxiliary-fixture-002` and
`virmill-auxiliary-fixture-002` with a fresh UUID selected after baseline capture.
They contain newly generated dummy bytes, never valid or existing TPM/NVRAM
state. The domain has no disks/NICs and is never started. Original policy content
and access metadata are conditionally restored as described in recipe 001; the
exclusive administrator policy window remains required. Lost acknowledgement or
concurrent state changes retain all resources without blind retry or cleanup.

The frozen revision, deployment, binaries, source inventory, execution switches
and byte/member bounds remain exactly those documented by recipe 001. Replace
the script/output name with `auxiliary-inspection-native-002` and supply the actual
uploaded SHA256. The recipe independently checks that hash before execution.
The parent exclusively coordinates upload, activation and the one native run.

Fifteen pure local self-tests passed before native execution. The first authored
new capability-query test had an incomplete synthetic constructor and failed;
using an uninitialized seam object corrected that test. No native call ran in
that authoring check. The recorded self-test evidence is
`auxiliary-inspection-native-fixture-002`. Native results belong in the separate
ledger and integration report, never inferred from these synthetic tests.

Acceptance contributions remain SNAP-01, SEC-01, SEC-03, UX-01 and UX-03
prerequisites. Actual TUI, native stale-request injection, capture, independent
restore and boot qualification remain separate; every complete-recovery claim
stays false.
