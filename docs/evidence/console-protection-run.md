# Console access and guided protection — 11 September 2026

This slice adds selected-VM console access, ordinary selected-capture backup and
restore forms, and simpler guest-tools setup. It remains owner-test beta evidence.
All 71 acceptance scenarios remain mandatory; no requirement is promoted here.

Root owned shared contracts, service/CLI/TUI integration, review, packaging,
release tracking and every remote action. The console backend agent implemented
read-only discovery and a real serial CLI/TUI fixture. The protection agent
implemented forms and simplified guest-tools fields. The console launcher agent
implemented fixed-argument session launch and the graphical fixture. File ownership
was separate; no agent operated SSH or changed the VM independently.

## What passed

- `console-protection-ui-001` and `console-protection-integration-001`: affected
  UI, shared app and libvirt adapter race suites passed. These are software tests,
  including endpoint refusals, exact identities, request shapes and failure paths.
- `console-protection-final-001`: focused integrated tests passed after adding
  explicit guest-tools Next/Preview behavior and console selection handling.
- Scoped Go vet passed. The three staged package/installer/private-IPC checks
  passed in `console-protection-packages-001`.
- `console-protection-upgrade-native-001` and restart evidence verified the exact
  package/binaries, idle coordinator activation and unchanged existing guests,
  journal tables and source-media metadata.
- `console-serial-native-001`: a new transient KVM BIOS guest with a generated
  1 MiB disk, one CPU and 128 MiB RAM emitted an exact repeating serial marker.
  Both installed CLI attachment and 80×24 TUI Console navigation received those
  bytes. Ctrl+] returned cleanly; TUI Esc restored the same VM details. The VM
  remained running after attachment ended, then only the owned fixture was
  stopped. Existing guests/jobs/media and both generated disk copies were
  preserved. This proves native serial transport and terminal restoration, not
  a guest OS login or package installation.

The serial run used source/binaries at `8fcde951d8c1ab92de355e09223a3c51554e8b93`.
The later viewer-version correction changes graphical version parsing only;
serial launch, discovery and TUI handoff implementation remain unchanged.

## Failures retained

`console-graphical-native-001` could not create its fixture because explicit
firmware='bios' requested an unavailable firmware descriptor. The fixture was
corrected to ordinary legacy BIOS selection, matching the successful serial
fixture; this was not a product console failure.

`console-graphical-native-002` then exposed a product compatibility-check bug:
Fedora prints `virt-viewer version 11.0-18.fc44`, while the check only accepted
`11.0` followed by whitespace. The gate now permits a distribution release suffix
while retaining the exact supported upstream version. The observed output has a
regression test (`console-viewer-version-001`). Both failed logs remain intact;
each run preserved existing guests, source-media metadata and the operation list.

## Native environment and limits

Observed packages: virt-viewer 11.0-18.fc44, libvirt-client and QEMU driver
12.0.0-3.fc44, qemu-system-x86-core 10.2.2-1.fc44, binutils 2.46.1-1.fc44,
Xvfb 21.1.24-1.fc44, xwininfo 1.1.6-4.fc44, spice-gtk3/spice-glib 0.42-8.fc44,
and gtk-vnc2 1.5.0-4.fc44. The administrator-installed viewer/headless-display
packages are test prerequisites, not an automatic Virmill runtime download.
Pinned Go 1.27.1 and vendored Go dependencies are unchanged.

VNC launch remains refused because this viewer's VNC clipboard callback ignores
the clipboard preference. SPICE clipboard/audio/USB/resize stay disabled; guest
integration behavior is not qualified by a BIOS display. A plain SSH session
without a desktop cannot display a graphical viewer.

Protection form tests use the exact existing backup/restore contracts; this
slice does not rerun native encrypted recovery or firmware/TPM restoration. Prior
scoped BIOS recovery evidence is retained in beta-recovery-run.md. Firmware/TPM
restore remains unavailable, and repository recovery still requires its exact
receipt inputs. Guest tools UI tests do not certify installation in a live
Debian/Ubuntu/Fedora guest. Windows remains manual. All other release gaps remain
in the requirements matrix and shipping checklist.

## Final installed build and graphical result

Installed source revision: `2c6091f04e3e76a3f95f00d4dffa2e249397677d`.
Implementation source digest: `fb771140d1af40765b87122634ad79480177b30ad8350abb9c13f1c491a03eab`.
The final package checks (`console-protection-packages-002`), guarded RPM upgrade
and idle coordinator restart all passed. The ordinary-user coordinator PID was
76838 after activation; journal and existing guest/media preservation checks passed.

| Artifact | SHA-256 |
| --- | --- |
| Core RPM | `4d384dd3b6837c422065e1b8023735643ffe2463ad468145e20aae357d4d63f5` |
| `/usr/bin/virmill` | `37ba1b76109b72d7f49b96d1d8b448f138aaf1290f98bc6db7ee5dbd08b35d40` |
| `/usr/bin/virmilld` | `07a6db7d714cacf65c1d9757d05890c6a9a9dbcf7c3708cb6476f4d0dd1416f6` |

`console-graphical-native-003` passed with the final installed binaries. The
fixture deliberately used an emulated, diskless BIOS guest; it is native QEMU/
libvirt/viewer evidence, not KVM acceleration or guest OS qualification. Read-only
QMP observed actual SPICE main/display channels over Unix transport, the viewer
window showed the exact guest, and parent visual review confirmed its BIOS
framebuffer. A diskless boot message is expected for this display-only fixture.
Closing the viewer returned exit 0, removed its connections and left the guest
running. Only the owned fixture was then stopped. Existing guests, operation list
and source-media metadata were preserved. No automatic guest actions were tested.

Framebuffer SHA-256:
`0b94542ce3ece37e6a599417d7d0aaa90dce87d9eb43c043ed7c38881a376fda`.
The generated PNG/report are retained in the ignored
`build/console-protection-final-delivery/` and remote
`~/virmill-tests/console-protection-2c6091f/graphical-access/`. Earlier serial
screens and failed graphical attempts remain in their separate run directories.
Reproducible fixture recipes are tracked; generated disk images, private Xauthority
cookies, executable artifacts and local host configuration remain ignored.
`git ls-files -ci --exclude-standard` returned no tracked ignored artifacts.

No package was published. The owner-test beta has these tested paths; complete
1.0 and publication readiness remain governed by the unchanged release checklist.
