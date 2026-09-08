# Automatic network allocation integration

Source: `66c2d1f` (`build/automatic-allocation-release`, clean frozen checkout).
Implementation digest:
`2ac46f59a679eb1ad61b858a3e98ebcf414b29b095bdc80a74186e0917a4d566`.
This is development source. All 71 acceptance scenarios remain required.

The shared creation workflow now resolves `cidr: auto` from ordered configurable
RFC 1918 pools, retains the exact choice in the immutable plan and refuses later
conflicts without selecting a replacement. Durable reservations and reviewed
recovery preserve that choice. Foreign managed networks require an exact native
profile; hidden guest-only subnets can be reconciled with UUID/CIDR declarations
whose complete native intent matches. See [operator guide](../network-allocation.md)
and [ADR 0029](../adr/0029-configurable-network-allocation.md).

The three agents delivered independent work with root retaining the shared
contracts, integration, remote mutations and release tracking:

| Agent | Owned deliverable | Acceptance contribution |
|---|---|---|
| Nash / host_prefixes | Pure ordered interval allocator and adversarial/property tests | NET-06 |
| Beauvoir / pci_inventory | Bounded settings loader and tests; read-only integration review | NET-06, JOB-02 |
| Russell / plugin_faults | Actual shared-service CLI/TUI tests, examples and operator guide | NET-06, UX-01, UX-03, REL-03 |
| Root | Shared observation/creation/recovery, foreign-profile binding, schema, daemon integration, builds and native fixture | NET-06, JOB-02, SEC-01, REL-01, REL-03 |

Recorded checks:

- `automatic-allocation-race-001`: **810 passing test records, no failure or skip**
  across allocator, application service, settings, CLI, TUI, schemas and private
  coordinator transport. Actual SQLite/journal and temporary-file/IPC behavior;
  substituted virtualization/firewall observations are software evidence.
- `automatic-allocation-static-001`: whole-module vendored Go vet passed.
- `automatic-allocation-repro-001`: two offline builds produced identical three
  binaries and four RPM/DEB packages in the same recorded native environment.
  This is reproducibility within that environment, not an independent-host claim.
- `automatic-allocation-documents-002`: **62 tests passed**; 243 staged payload
  files, 135 Markdown files and 216 local links checked with zero missing targets.
- `automatic-allocation-artifacts-002`: all three package/staged-preservation and
  temporary-coordinator tests passed. The actual 80×24 PTY exercised existing
  navigation and error feedback; automatic-allocation form/review parity is
  separately exercised by the shared-service UI race tests.

Failures and limits remain visible. The first staged-document run (`documents-001`)
started before package staging completed and failed on its absent install
manifest; it remains a failed ledger entry. `artifacts-001` omitted the explicit
IPC opt-in and skipped that case; only `artifacts-002` supplies the IPC/PTY result.
Development test assumptions about error envelopes, TUI paging and a purported
nested range were corrected before the frozen run. Existing zero Plan/Job error
envelopes remain unchanged and carry no valid IDs or authority. The independent
review found an initial foreign guest-only visibility gap: a stray route could
look like an observable allocation. Complete fixed-profile matching closes it,
and regressions cover added routes/IPs, missing/mismatched UUID declarations and
exact empty/subnet-bearing guest-only profiles. No executed pre-fix exploit or
hardware result is claimed.

Native fixture results are recorded separately below. They do not establish
physical USB, guest routing, complete host/guest compatibility, backup/revert,
bridge rollback or complete NET-06/1.0 acceptance.

## Disposable native result

`automatic-allocation-native-001` exited **0** on the authorized Fedora 44 VM.
It ran isolated binaries from the frozen checkout; installed system packages
were not upgraded. The supplied fixture settings searched `10.193.76.0/22`
for /24 networks. Existing inactive lab `10.193.76.0/24`, planned
`10.193.77.0/24` and the exact declared foreign guest-only `10.193.78.0/24`
were excluded. A second foreign guest-only subnet outside the pool was also
explicitly reconciled. The plan chose **`10.193.79.0/24`**.

The first apply, lacking a UUID-specific administrator grant, was refused.
After the exact grant, operation `d12a1d88-e0b0-4bf5-a99d-8d16475eab22`
succeeded with network `2159dd52-0b7f-47cb-8403-d5921231b305` and bridge
`vm2159dd520b7f`. Its plan digest was
`e53a380b49f546958da5b10f9e624758a90edc051ce370483ccfae7622e83a6c`.
This run used the explicit `allow` host-access lab profile. Services-only and
NAT automatic selections are covered by shared-service tests; their earlier
explicit-CIDR packet evidence remains separate.

The real kernel namespace packet fixture exited 0. It acquired two DHCP leases
(`10.193.79.163` and `.164`) without default-router/alternative-route options,
connected peer-to-peer and verified the expected allowed host TCP listener.
An explicit external target was not connected, but this cohort has no positive
external control and therefore establishes no firewall enforcement proof.
Managed DNS was not tested in this cohort. IPv6 link-local connection failed and
no RA was seen in a five-second window; these bounded observations do not prove
IPv6 isolation. No VM guest or physical device participated.

Cleanup verified every existing guest state/XML (10 guests), every existing
network state/XML (12 networks), original helper policy, runtime/permanent direct
rules and active firewall zones. All remained equal. Unlike earlier protected
runs, this run's zone snapshots matched; it does not retrospectively convert
those earlier failures into passes. The original policy SHA-256 remained
`1ec86a37a44cac12e7fa7c16c542ec83e552e348ea2f2cec841597dc999d6ed4`.

The new network remains persistent, inactive and not autostarted. Its journal,
configuration and DHCP leases are retained. Temporary namespace/veth resources
and the runtime helper override were removed; the helper service/socket are
inactive. No owner guest disk or source medium was read, rewritten or removed.
The recorded packet result retains `fullNetworkAcceptance: false` and
`guestOrHardwareQualification: false`.

[Artifact attribution](automatic-allocation-artifacts.json) records binary,
package, fixture/settings and retrieved report hashes. Main `build/bin` and
`dist` now contain these verified development artifacts; the previous artifacts
remain under `build/artifacts-before-automatic-allocation`. No publication or
signing identity was selected and nothing was pushed.

The complete release gate (`automatic-allocation-release-gate-001`) still fails:
2 acceptance scenarios are accepted, 52 are in progress and 17 are not
implemented. All 69 remaining scenarios and unchecked ship requirements remain
mandatory. This slice promotes no complete acceptance scenario based on its
narrower evidence. The ledger contains 435 entries; all 433 available log hashes
were verified (two historical intake notes have no logs). Git contains no tracked
ignored files, and generated binaries, packages, media, test-host settings and
private application state remain excluded.
