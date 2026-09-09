# Unified source review and guest tools — 9 September 2026

Installed beta revision: `1b22c55a6184fbad33ad2c9b23e975ecd168c7ad`.
This remains development beta evidence, not acceptance of all 71 scenarios.

| Installed artifact | SHA-256 |
| --- | --- |
| Core RPM | `1799bc17df529816702ca3e697e82e393b886dee5db57dcbdf4317b054ebc55c` |
| `/usr/bin/virmill` | `4b7e75a584a7d3758a743c0aa218ca427b62a1c9127ed56c3d22b072426cee32` |
| `/usr/bin/virmilld` | `46a55a4811081cbaabbae14d042fabeae5c1d15aedd8c92d1cef3c832f602e60` |

The final package is in `dist/`; manifests are in
`build/general-sources-delivery/`. Frozen source/package build output is retained
in `build/general-sources-packages-final.json`. Exact vendored dependencies and
Go 1.27.1 remain unchanged. Native test RPMs were qemu-img 10.2.2-1.fc44,
libvirt-libs/libvirt-daemon-kvm 12.0.0-3.fc44 and xorriso 1.5.8-2.fc44 (x86_64). Packaging and three staged artifact/installer/private
IPC integration checks passed. No package was published.

## Work allocation

- Erdos implemented confined native metadata reads and optional guest-agent
  channel XML, with regression tests and a bounded native fixture.
- Arendt implemented the general source summary and added guest-tools form,
  request, credential-picker and return-navigation regression tests.
- Zeno implemented fixed guest-tools recipes, documentation, the installed TUI
  probe and privilege/channel authorization tests.
- Parent owned shared DTO/schema/registry integration, source selection,
  timeout behavior, plan acknowledgement, native runs, packaging and this ledger.
  Only the parent operated SSH or changed the authorized VM.

## Evidence actually observed

- `general-sources-core-001`: affected import, image, libvirt, creation,
  guest setup, TUI, CLI and Unix transport packages passed race tests. An earlier
  unrecorded sandbox run refused temporary socket creation; the authorized
  temporary-socket rerun passed. Native hardware opt-ins remain separate.
- `general-sources-integration-001`: integrated UI/guest setup/creation/libvirt/
  import race tests passed. Final browser regression and whole TUI race tests
  passed after the native navigation failure described below. Scoped vet passed.
- `general-sources-qemu-native-001`: actual confined QEMU metadata reads passed
  for raw, qcow2, vmdk, vdi, vpc and vhdx; missing backing metadata did not open a
  backing file; split VMDK extents required explicit selection. Tiny generated
  fixtures only; this is not guest boot or conversion qualification.
- `general-owner-sources-native-001`: existing Windows OVA, Kali ISO and extracted
  Kali VDI each described in approximately 0.11 seconds through the installed
  shared service. Device/inode/size/mtime/ctime remained unchanged. This check did
  not rehash the large source files or prove bootability.
- `guest-channel-native-001`: a new network-free, powered-off 512 MiB VM with a
  1 MiB disk was prepared and defined successfully. Its native inactive XML
  retained one fixed guest-agent channel with automatic socket policy. Repeated
  Apply returned the same operation. Prior guests/jobs and supplied media
  metadata remained unchanged. No guest was started and no crash was induced.
- `general-sources-ui-native-002`: final installed CLI descriptions for all six
  disk formats, a disk folder and ISO passed. Actual 80×24 and 120×36 PTYs
  passed QCOW2/ISO selection, editable CPU/RAM, Advanced before destination,
  return-navigation preservation, and guest profile/desktop/SSH-field controls.
  Guest inventories, jobs and generated fixture metadata remained unchanged.
- Both guarded RPM upgrades and idle coordinator restarts passed. Final ordinary
  coordinator PID was observed as 69679; five terminal jobs and zero locks were
  retained. Existing guests and media stayed unchanged during the updates.

Native backend checks above ran revision `5a916f2` with CLI SHA
`0048da928f4aacbba94f1bb84b53229e0aa459689bd2fc4457fca2b93503bda4`.
The final revision changes only browser directory selection and summary wording
plus its regression test. Recorder source digests include working-tree fixture
and UI changes; installed executable hashes identify the code actually executed.

## Failure retained and corrected

`general-sources-ui-native-001` passed all six CLI metadata descriptions, folder
and ISO descriptions, then caught typed folder paths selecting the whole folder
instead of opening it. Revision `1b22c55` fixes this: Enter opens folders in the
mixed source browser; Ctrl+S explicitly chooses the folder. A regression covers
typed folder navigation, explicit folder selection and typed file selection.
The failed log remains in the ledger.

## Support limits

Guest tools have software tests and native UI/channel evidence only. Actual
Debian/Ubuntu/Fedora package installation, agent handshake, SPICE clipboard and
resize remain unverified. Linux auto detection is limited to those exact supported
distributions with systemd; Kali is not silently treated as Debian. Windows
installation is manual using trusted local media. Existing guests need their
channel configured explicitly; the current editor does not retrofit it.

Archives require extraction to a new directory. Loose OVF/VMX/VBOX reconstruction
is not implemented by this slice. Persistent interrupted wizard drafts, broad
hardware/firmware recovery and other outstanding acceptance work remain open.
No acceptance scenario was promoted: 2 accepted, 57 in progress, 12 not implemented.
