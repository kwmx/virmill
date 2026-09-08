# Owned network creation and packet qualification

Development source remains unqualified for 1.0. All 71 acceptance scenarios and
the complete release checklist remain required. This record covers NET-01,
NET-02, NET-05, NET-06, relevant job/security boundaries, and development artifacts.

Source `151536dc4a4dbbd8719b59f8558c7caa1321cb26` introduced shared CLI/TUI
network creation and reviewed activation recovery. Source
`22846d4ff828b40f06aceb689a195d0c42dd168c` increased the bounded authenticated
network-helper deadline to 90 seconds after observing actual firewalld command
cost. Its implementation digest is
`3b81f25d69d6b4156761f94626cf4801a75cc53d9e6d79d1dfab09d176381e17`.
The isolated `build/network-release` checkout preserves that implementation.
The ledger separately pins the deployed packet recipes; later fixture fixes
do not imply that the compiled product changed.

The executable profiles create a fresh allowed-host NAT or allowed-host lab,
with an explicit private IPv4 subnet and disabled IPv6. Application reservations
are bound to the original plan, input, job and complete definition. Define,
IPv6 filter installation and activation have separate durable intents. Native
network and host-allocation locks survive uncertain partial work. Reviewed
recovery inherits every original lock and never repeats definition. Independent
helper grants authorize only the exact actor/key/network UUID. Four scoped
bridge IPv6 DROP rules are installed in runtime and permanent firewalld state.
See [ADR 0026](../adr/0026-owned-network-creation-and-recovery.md),
[operator workflow](../network-creation.md) and [helper setup](../network-helper.md).

Executed development checks:

- `owned-network-core-001`: full Go race/conformance/required-IPC run passed
  2,948 test records, including 552 top-level tests; 20 test skips remain in the
  log. Disk-tool opt-in was absent. This is not hardware or packet evidence.
- `owned-network-boundary-002`: the final helper, shared application, operations,
  CLI and TUI race checks passed 878 test records, including 213 top-level tests,
  with one recorded skip. Private IPC was explicitly required.
- `owned-network-repro-001` and `owned-network-repro-002`: each selected source
  produced three identical binaries and four identical RPM/DEB artifacts across
  two builds in the same environment. This does not establish independent-host
  reproducibility or a clean-distribution installation matrix.
- `owned-network-artifacts-001`: three actual package/staging/preservation and
  private coordinator checks passed, including an 80×24 PTY action-search check.
  New network action parity additionally has shared CLI/TUI unit coverage.
- `owned-network-documents-001`: eight document/payload checks passed; 219 payload
  files, 122 Markdown documents and 185 resolved local links had no missing
  targets. The installed protocol matched its source.
- `owned-network-cross-001` and `owned-network-vet-001` passed. Cross builds are
  common-core/SDK compilation evidence, not non-Linux host support.
- `evidence-attribution-validation-001`: ten generated-file tests passed for
  immutable acceptance-ID validation before any evidence command or write.

Native work used only the owner-authorized disposable Fedora 44 VM. Observed
versions include libvirt 12.0.0-3.fc44, QEMU 10.2.2-1.fc44, firewalld and
python3-firewall 2.4.4-1.fc44, nftables 1.1.6-2.fc44, NetworkManager
1.56.1-2.fc44, iptables-nft 1.8.11-13.fc44, Python 3.14.7 and systemd 259.8.
The host already enabled IPv4 forwarding. Virmill did not change that sysctl.
Exact final versions and retained resources are in the
[native environment record](environments/disposable-fedora44-network-20260908.json).
The system-installed Virmill packages remained the earlier development version;
tests used exact copied binaries, an isolated user coordinator and a temporary
root-owned helper executable override. No source media was changed or guest booted.

Native failures are retained:

1. `owned-network-native-001` defined the new lab
   `fbb3905c-5c0b-4e03-a951-0e7ad2255847`, then exceeded the original 30-second
   helper deadline. Operation `5f24a9ee-2b6f-4d34-bfde-7fedc4f1b971` became
   recovery-required at the firewall step. The network remains inactive and
   persistent. Prior resources, rule lists, zones and helper policy were restored.
2. `owned-network-native-002` successfully completed all three creation steps
   for lab `1739e5f7-27cd-4896-992f-961cf5c8b17e`. The packet fixture refused
   libvirt's equivalent omission of `ipv6="no"` before creating endpoints.
   Cleanup and prior resource/firewall/policy preservation passed. The corrected
   parser accepts only documented omitted defaults, with enabled IPv6 refused.
3. `owned-network-native-003` successfully created lab
   `bc247e57-0919-489b-a02e-3f3ad3c92447`. The packet fixture stopped before
   bridge attachment because a newly created veth had no retained alias.
   Its journal also observed MAC initialization between two snapshots with
   unchanged kernel indices. The pair was left down and unattached for review.
   `owned-network-veth-cleanup-003` refused before mutation when its original-MAC
   assumption differed; `owned-network-veth-cleanup-004` verified both matching
   later journal observations, removed only that exact pair and verified all
   other link identities remained. The original failed cleanup is preserved.
4. `owned-network-native-004` passed endpoint initialization and cleanup, but
   the DHCP worker timed out after nine seconds. Host dnsmasq logged a DISCOVER
   and OFFER. The fixture now observes bridge-port forwarding before requesting
   DHCP, allows 18 seconds per DHCP phase, and retains bounded received frames
   on failure. The timeout's cause is not retroactively asserted.

`owned-network-native-005` completed both native creation jobs and both scoped
packet suites using recipe SHA-256
`0e41d68b7e54b865d62560dc511cd7084526575efeba43de8f4bb18479892de9`.

| Actual observation | Lab `10.197.230.0/24` | NAT `10.197.231.0/24` |
|---|---|---|
| New network UUID | `dceba41d-8d1a-4cb1-9e4a-4e66a6cf148a` | `e1916980-5169-4557-8180-669b10c93c8a` |
| Two independent DHCP leases | Passed | Passed |
| DHCP router option | Absent on both ACKs | Gateway advertised on both ACKs |
| Peer IPv4 and allowed bridge-host TCP | Connected | Connected |
| Explicit target route to `1.1.1.1:443` | Connection refused | Connected |
| DNS through advertised gateway | Not requested | `example.com` resolved |
| IPv6 link-local peer connection | Not connected | Not connected |
| Solicited RA observation, five seconds | None observed | None observed |
| Endpoint cleanup and unchanged selected bridge/XML | Verified | Verified |

Negative connections and bounded RA absence do not prove complete filtering or
IPv6 isolation. These endpoints had one NIC each; no real multi-NIC guest was
qualified. Test target routes were explicit `/32` routes, not installed guest
default routes. Source media and all ten pre-existing stopped guest definitions
were preserved. Test networks remain inactive/persistent with autostart off.

**The overall native-005 record is failed**, despite passing packet suites.
Runtime and permanent direct-rule lists returned to their empty baselines and
the exact helper policy was restored, but `ens19` disappeared from firewalld's
active-zone listing. The separate `owned-network-nm-diagnostic-001` log records
the existing NetworkManager connection repeatedly failing DHCP and disconnecting
during the run. That supports an unrelated dynamic-state explanation; it does
not convert the strict before/after preservation assertion into a pass. No
`ens19` configuration was changed to obtain a green result. Complete host-network
preservation qualification remains open. Final read-only observation found no
fixture namespaces, both helper units inactive and all ten original guests stopped.

The native-003 recorder was invoked inside the frozen checkout. Its unmodified
[origin record](owned-network-native-003-origin.json) is retained. Main-ledger
import keeps the failed result, exact log and compiled source identity, and
corrects the separately deployed packet recipe digest from the recorded output
and retained deployment copy. This is a provenance correction, not another run.
Earlier core/repro entries accidentally listed nonexistent OPS IDs; their two
append-only attribution records map them to the actual JOB IDs without a rerun.

Restricted host-access profiles, guest-only networks, enabled IPv6, automatic
allocation, physical bridge setup/rollback, Wi-Fi handling, forwards, ownership-safe
teardown and actual multi-NIC guest routing remain required. Namespace packets
cannot certify a guest OS, physical NIC, external IPv6 path or complete isolation.
The product result continues to report `packetVerification: not-run` and
`guestRoutingVerified: false`; an active libvirt network is not packet proof.
