# Protected network implementation and qualification

Production source is frozen at `36e7838726c046fcd7deb1ad99af20b44e621a71`,
implementation digest `011f66a994382bc02682c6b116fbe9c2671fa8afccc8d6b6b7146f29b9bd24d3`,
in `build/protected-network-release`. The parent owns integration, architecture,
release attribution and every remote mutation. No release was published.

Three agent contributions were integrated: Nash implemented strict protected
XML/backend handling and CLI/TUI examples/tests/docs; Beauvoir implemented the
bounded firewall executor and ordering/recovery tests; Russell implemented the
packet fixture and reviewed application/transport boundaries. The parent added
versioned recipes and reservations, separate grants, client/server integration,
native orchestration and the fixes described below. See [ADR 0028](../adr/0028-protected-network-policy.md).

The new services-only NAT/lab and guest-only profiles use internal policy
version 2 with separate administrator grants. Old version 1 plans, XML hashes,
helper rules and journals remain compatible. Shared CLI/TUI planning preserves
the exact requested policy and exposes no packet verification claim. A
guest-only CIDR is only a logical reservation; an addressless guest-only result
correctly reports no reserved subnet. Cancellation returns no successful result
payload. These last two result boundaries were found during agent integration.

| Frozen check | Actual result |
| --- | --- |
| `protected-network-race-001` | 529 test records passed, zero failures/skips. Includes shared application, helper, XML/backend and CLI/TUI integration with required private Unix-socket tests and in-memory libvirt cases. |
| `protected-network-xml-001` | 315 generated XML test records, including exact version 1 goldens and fuzz seeds, passed with zero failures/skips. The ledger class label also mentions in-memory libvirt, but this command executed only `internal/backend/networkxml`; it made no native calls. In-memory backend cases belong to the separate race check above. |
| `protected-network-repro-001` | Two offline builds produced identical three binaries and four unsigned RPM/DEB packages. |
| `protected-network-documents-001` | 54 package-input and documentation-transform tests passed. |
| `protected-network-staged-docs-001` | Eight checks passed; 235 payload files, 131 Markdown documents, 205 resolved local links, zero missing targets. |
| `protected-network-artifacts-001` | Three package/staged-preservation/private-coordinator tests passed, including actual 80×24 TUI navigation. No real host installation was performed. |
| `protected-network-vet-001` | Scoped static analysis passed. |

The agent's initial required private-IPC execution was blocked by sandbox
`getsockopt` permissions. The parent reran it with authorized temporary IPC;
all receiver paths passed, then passed again in the frozen integration check.
No real privileged helper endpoint was used in those local tests.

Native run `protected-network-native-001` used the frozen binaries and packet
recipe `d7ef2000a2c90223dc4f513ee31634b7eed290c8e9fb1637e897b95ce533997e`.
It created only fresh lab UUID `ca5526c4-10e1-45f3-bcd4-00103647f568`,
bridge `vmca5526c410e1`, subnet `10.193.70.0/24`. Unapproved apply was refused;
the separately granted creation job `7a0efe9a-5b19-4c52-9298-5f30414e9af4`
succeeded through define/filter/activate. Packet observations established:

- Two own DHCP leases without router options and successful IPv4 peer traffic.
- Arbitrary host TCP was blocked while the same listener's before/after host
  controls succeeded. External TCP was blocked while host controls succeeded.
- IPv6 link-local was not connected and no solicited RA appeared in five seconds.
  Neither negative window establishes complete IPv6 isolation.
- DNS returned REFUSED for the fixture's external `example.com` query. The
  retained libvirt-generated isolated-network dnsmasq configuration contains
  `no-resolv`. The fixture must qualify a locally learned DHCP name instead;
  the failure is retained and is not reclassified as a pass.

The packet result and overall run failed. Endpoint cleanup was verified;
the new network was deactivated, its exact test rules removed, and helper policy
restored. All ten pre-existing guests and seven pre-existing network definitions
retained their checked state/XML. Runtime and permanent direct rule inventories
returned to their empty baselines. Active-zone membership changed from public
`ens18 ens19` to `ens18`, so the strict firewall-preservation assertion failed.
Earlier native work independently observed recurring NetworkManager DHCP
timeouts on `ens19`; this does not convert the failed assertion to a pass or
authorize changing that interface to obtain a green result.

Test-runner revision `4900123` adds independent profile selection and continues
after packet discrepancies only when endpoint cleanup is verified. It changes
no product code. NAT and guest-only can therefore proceed while the lab DNS
probe is corrected; each run retains its exact source/fixture attribution.

Native run `protected-network-native-002` retained the same binaries and packet
recipe, with the independent runner. Services-only NAT UUID
`150afad4-0efb-485d-a9d9-bfbcfa4b1b82` (`10.193.74.0/24`) passed creation and
its entire scoped packet suite: two own DHCP leases with gateway router options,
peer IPv4, managed DNS, host TCP denial with successful controls, and external
TCP connectivity. Its bounded IPv6 observations did not contradict disabled
IPv6, and still do not prove the complete policy.

Guest-only UUID `ac06e66c-5f16-4997-b02a-55b327c522e6` (`10.193.75.0/24` logical)
also passed creation. Its packet fixture stopped with `KeyError: 'address'`:
`ip -4 -j address` omits the link-layer MAC required to bind the existing host
target. No guest-only packet result was established. Endpoint cleanup was
verified, all ten guests and eight prior network definitions were preserved,
and exact test rules and helper policy were restored. The active-zone
`ens19` membership difference again made firewall preservation fail; the overall
run remains failed.

Fixture revision `c3ea878` adds `--dns-own-lease`: DHCP sends unique generated
hostnames, and role A must resolve exactly role B's own acknowledged address.
Revision `0004d68` reads combined address/link inventory, validates its exact
target identity, and then selects IPv4. It refuses incomplete identities rather
than weakening the MAC binding. `protected-network-packet-parsers-003` records
62 passing pure tests for the corrected fixture. These changes require no new
product binaries. The following native run separately records the new fixture
hashes against the unchanged frozen production source.

Native run `protected-network-native-003` passed both selected creation and
packet suites with fixture `d87cc1d563188aa2bdddac849cddc4625840d7d62c10ded9131e4f512e2e4916`:

| Profile / retained UUID | Observed scoped result |
| --- | --- |
| services-only lab `7fe79d9a-9779-4df2-b8cb-08170380947c` | Two own DHCP leases without router options; role A resolved the exact role B DHCP hostname to `10.193.76.252`; peer IPv4 connected; arbitrary host TCP and external forwarding blocked with successful before/after host controls. |
| guest-only `76e1eaa8-42b2-46ac-93d3-be9d5a31caac` | No host L3 at every checkpoint; explicit `10.193.78.10` and `.11` peer traffic connected; host TCP and external forwarding blocked with successful controls; no DHCP response in the five-second discovery window. |

Both packet processes exited zero with verified endpoint cleanup. The bounded
IPv6 observations found no contradiction but do not prove complete isolation.
The ten guests and ten prior network definitions retained their checked state
and XML; both new networks are inactive, persistent, and have autostart disabled.
All direct test rules were removed and the exact original helper policy restored.
The active-zone `ens19` difference again failed the final preservation assertion,
so the ledger correctly records overall exit 1 despite the two scoped packet
passes. No assertion was weakened to obtain a green run.

Final read-only checks found no namespaces, inactive helper service/socket, no
test service override, the default network active, and all eleven generated
networks inactive. All ten pre-existing guests remain shut off. Restored policy
SHA-256 is `1ec86a37a44cac12e7fa7c16c542ec83e552e348ea2f2cec841597dc999d6ed4`.
These network-only runs never read or altered source media or guest disks.

The main `build/bin`, `dist` and package stage now contain the verified frozen
36e7838 artifacts. Earlier artifacts are retained under
`build/artifacts-before-protected-network`; ignored `build/current-artifacts.json`
and the [per-run artifact inventory](protected-network-artifacts.json) identify
the exact production and fixture bytes. Disposable-host installed packages still
remain the earlier 490b88c revision; these native runs used isolated test copies.

The support claim remains unqualified. Namespace traffic is real kernel packet
evidence, but does not prove real guest OS routing, multiple NICs, physical bridge
behavior, firewall reload/reboot persistence or the complete IPv6 matrix. All
71 scenarios and the complete release checklist remain mandatory.
