# First disposable-host run — 2026-09-07

The owner supplied a Fedora 44 test VM, passwordless sudo and six image samples.
The host was expanded before testing to about 253 GiB, with about 191 GiB free.
Connection details remain in ignored `.virmill-local/test-host.json`.
[Exact native versions](environments/disposable-fedora44-20260907.json) and
[source hashes](fixtures/disposable-samples-20260907.json) are recorded separately
from the local build environment. No guest image is redistributed.

The initial installed build was `65930c63498f58b19f962e86a4b20f83841de872`.
Two observed defects were corrected and the host upgraded to
`c9aa310693c337a8908787f8b616262b3fa53faf`:

- VirtualBox manifest syntax `SHA1 (file) = digest` was rejected because the parser
  required no space. Commit `f174a4e` accepts horizontal spacing, with tests for
  SHA1/SHA256/SHA512, missing end markers, mismatched digests and malformed paths.
  The supplied Windows OVA now passes all provided manifest checksums. SHA1 remains
  explicitly weak; publisher authenticity, firmware recovery and boot are unverified.
- The native QEMU driver refused the read-only connection used for domain-capability
  queries. Commit `c9aa310` corrects the handle access while retaining observation-only
  preview and durable apply. See ADR 0012. The corrected native creation preview passed.

## Executed evidence

| Evidence | Observed result | Limit |
| --- | --- | --- |
| `native-first-core-001` | Full race-enabled core tests passed from committed source | Default-sandbox/unit/simulated scope; not hardware qualification |
| `native-first-build-001` | Two offline builds produced identical three binaries and four packages | Unsigned development artifacts |
| `native-first-artifacts-001` | Three staged package/private-daemon tests passed with actual confined generated-file workers | No development-host VM or service mutation |
| `native-first-upgrade-001` | Real test-host package replacement and `rpm -V` passed; completed import journal survived restart | Full distribution/install/uninstall matrix remains open |
| `native-first-qcow2-001` | Supplied QCOW2 converted, checked and content-compared in confinement | Preparation, not guest creation or boot |
| `native-first-tui-001` | Real PTY showed native VM inventory and the running import job | Read-only navigation, not full CLI/TUI acceptance |
| `native-first-ova-001/002` | Original manifest failure retained; corrected inspection passed | No Windows disk extraction or firmware/TPM restoration |
| `native-first-capabilities-001/002` | Original native preview failure retained; corrected preview passed | Preview created no resources |
| `native-first-creation-001` | Actual allocation/upload/full readback passed; definition confirmation failed | Operation remains `recovery-required`; VM never started |
| `native-first-preservation-001` | Original QCOW2 hash and preexisting guest fingerprint unchanged | Source preservation for this run only |
| `native-first-recovery-001` | Already-uncertain job and three locks survived coordinator SIGKILL; reconcile replayed nothing | Crash was after failure, not during allocation/upload/definition |

The Kali archive SHA-256 is
`c7c35588d05277c482c908bf7a136d348f76ffa68700b04ff53c0b217e6bd071`.
Its extracted 15,999,631,360-byte QCOW2 has SHA-256
`4e24751faa18753ad5f854053c523481d1bc5efa9a45c3828071b7475d825104`.
The verified independent artifact and native volume both hash to
`8eb9be4a9592ad8242fa4e99fbac2dd2eef474bb93e99f3699d17617c7d93c70`;
virtual capacity is 86,000,000,000 bytes. Extraction used unprivileged 7-Zip inside
isolated bubblewrap namespaces, a read-only archive, private output and resource limits.
The VMware, VirtualBox, Hyper-V and ISO samples were inventoried and hashed; their
import/installation and guest behavior have not yet been tested.

## Remaining native creation failure

The plan requested Q35, two CPUs, 2 GiB RAM, BIOS, one virtio disk, a local VNC
socket and no NICs. Libvirt added controllers, PS/2 inputs, audio-none, a virtio
balloon and an `itco` watchdog with action `reset`. The last two in particular
have semantics that must be represented in reviewed intent, not silently ignored.
Strict verification refused completion. The captured inactive XML is retained at
`tests/fixtures/creation/qemu12-q35-unreviewed.xml` for a unit regression.

The subsequent [device-policy run](device-policy-run.md) implemented and tested
explicit intent on a separate guest. Handling this old uncertain plan without
silently changing its meaning remains outstanding. The current plan must not be redefined, accepted or started merely
to make this test pass. This is an implementation blocker, not unavailable hardware.

Retained operation: `b5983e68-bfcc-42f0-bf4e-3877029b756b`.
Retained VM: `ae630461-91d3-4f07-ad88-e6842c3dc3ea`, powered off.
Its volume remains in the dedicated non-autostart pool
`virmill-qualification-65930c6`. Source media and the preexisting guest are preserved.
No guest boot, networking, USB, Secure Boot, TPM, backup or full release support
claim passes from this run. All 71 acceptance records remain open.

## Inspect the retained test state

Run on the authorized test VM as its ordinary user:

```sh
virmill_test_root="$HOME/virmill-tests/run-65930c6-20260907"
export XDG_STATE_HOME="$virmill_test_root/state"
export XDG_CACHE_HOME="$virmill_test_root/cache"
export XDG_DATA_HOME="$virmill_test_root/data"
export XDG_CONFIG_HOME="$virmill_test_root/config"
export XDG_RUNTIME_DIR="$virmill_test_root/runtime"
virmill operation show b5983e68-bfcc-42f0-bf4e-3877029b756b --output json
virmill vm creation result b5983e68-bfcc-42f0-bf4e-3877029b756b --output json
virmill tui --connection qemu:///system
```

This first run used transient user unit `<test-vm-login>-c9aa310.service`; the
[subsequent run](device-policy-run.md) replaced it with `<test-vm-login>-c7f8b76.service`,
with a two-hour runtime bound. Run `virmilld` with these variables if that unit has
expired; never start a second coordinator over the same journal. Systemctl user
commands need the normal login runtime/bus environment, not this private runtime.
The installed helper remains disabled. No persistent Virmill service was enabled.
