# 1.0.0-beta.3 owner pre-release verification

Scope: REL-01 and REL-04 release evidence, plus scoped UX, network and job
checks. All 71 acceptance scenarios remain mandatory. No status is promoted,
and this is not 1.0 qualification.

## Build

`0627800b70d6e04e826df72d7dc0e257c1c0d7df` is `02ea66b` with the product version
changed to `1.0.0-beta.3`, the CHANGELOG section retitled and the packaging
guide's timestamp sentence corrected. Since beta.2 it adds the dpkg-installable
DEB builder (`f33453b`), the Import Esc fix (`87598b4`) and the private-value
protections (`707c858`).

`make verify` passed: Go tests, vet, traceability and the private-value scan. The
package integration tests passed with the private IPC opt-in.
`beta3-reproducibility-001` built twice from a clean worktree of the build commit.
Both passes produced identical binaries and packages, and they match the
delivered files below.

`beta3-deb-install-container-001` **passed** in Debian 13 and Ubuntu 24.04
containers: dpkg-deb archive checks, apt installation, `dpkg --verify`, systemd
unit verification, `virmill version` as an ordinary user, and a purge that left
nothing behind. lintian's only error is `unstripped-binary-or-object`; the Go
binaries keep their symbols.

## Native results

All runs used the authorized Fedora 44 test VM through the installed packages,
with a private client state directory for each TUI probe. Remote reports are
retained under `~/virmill-tests/beta3-0627800*`.

`beta3-install-native-001` **passed**. With all 38 jobs terminal (one historical
recovery-required fixture job), the idle coordinator was stopped and its state
copied. Both beta.2 RPMs were upgraded to beta.3; the three installed binary
hashes, version and revision, CLI inventory commands and an actual 80×24 TUI start
and exit were verified. Guest and network definitions, source-media metadata and
the inactive helper policy were unchanged.

`beta3-tui-workspace-native-001` **passed** at 80×24 and 120×36, including the
Import Esc fix: leaving an unused Import started from the VM list returned to the
list. `beta3-job-activity-native-001`, `beta3-network-form-native-001` and
`beta3-general-sources-native-001` **passed**. No plan was applied and no guest
was started. The owner's draft directory held only its lock file afterwards.

SPICE creation was not rerun. Since `beta2-spice-creation-native-001`, no creation
or console product code changed; the only creation-related change re-pinned one
test fixture hash after the private-value redaction.

## Artifacts

| File | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.3.x86_64.rpm` | `fa75f7658731b3087aab1762833dec644204d8039d8a4003929982e55b9ecfae` |
| `virmill-host-helper-1.0.0-0.beta.3.x86_64.rpm` | `fe3a154d8b50cd31db47876de1c51c854b988bc3457fb73e7f7e802295c41986` |
| `virmill_1.0.0~beta.3_amd64.deb` | `8c0d005b6eddef4dda9e6ace1305edb3aa648835d19e09025faff8de355e32ea` |
| `virmill-host-helper_1.0.0~beta.3_amd64.deb` | `0707956630e810902968aa2a71a3d51b2051f00c6c6104e3d5b2f36b3f14eae0` |
| `virmill-1.0.0-beta.3-source.tar.gz` | `e8fff234771fd97ff2f3973ea3702cc36b9698d7653da0441690f3e5b93453af` |
| installed `/usr/bin/virmill` | `eaa0379aeb6982c28ec7237e78b64fcfcd51c441877e1800ffe08916263ccfa5` |
| installed `/usr/bin/virmilld` | `4926c3198380c77647acd3dedfbaf0a53026fc135d4de2684c578488a4e83430` |
| installed `/usr/libexec/virmill-host-helper` | `2321037e07a666798fbb896c9aad9a3f120e11f16bedd4fc2dae8eb71e15904e` |

GitHub serves the DEBs as `virmill_1.0.0.beta.3_amd64.deb` and
`virmill-host-helper_1.0.0.beta.3_amd64.deb`; the bytes are the same. The packages
are unsigned. No guest OS family, physical hardware or real Debian/Ubuntu host is
qualified by these results.
