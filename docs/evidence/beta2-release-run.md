# 1.0.0-beta.2 owner pre-release verification

Scope: REL-01 and REL-04 release evidence, plus scoped UX, network, job and
creation workflow checks. All 71 acceptance scenarios remain mandatory. No
status is promoted, and this is not 1.0 qualification.

## Build

`b327d7bdef5e3c159fcfea1df1cc346e603837d9` is the `7005898` implementation with the
product version changed to `1.0.0-beta.2`, a CHANGELOG entry, and version
expectations in tests and upgrade fixtures derived from the package builder.
Several earlier builds had all reported `1.0.0-beta.1`; this build has its own
version.

`make verify` passed (Go tests, vet and traceability). The package integration
tests passed with the private IPC opt-in (3 tests, including the 80×24 PTY
search check). `beta2-reproducibility-001` built twice from a clean linked
worktree of `b327d7b` with the pinned Go 1.27.1 archive. Both passes produced
identical binaries and packages, and they match the delivered hashes below.

The GitHub build workflow had never passed: `.tools/` is untracked, so a fresh
checkout had no toolchain. `4f3f2b0` runs `scripts/bootstrap.py`, which verifies
the locked SHA-256 before extraction, and checks that the generated CLI reference
and completions are committed. Run `34762115859` on `cedbf2c` passed.

## Native results

All runs used the authorized Fedora 44 test VM (libvirt 12.0.0, QEMU 10.2.2)
through the installed packages. Remote reports are retained under
`~/virmill-tests/beta2-b327d7b*`; local copies of the delivery are in
`build/beta2-delivery/`.

`beta2-install-native-001` **passed**. With all 34 jobs terminal (33 succeeded,
one historical recovery-required fixture job), the idle coordinator was stopped
and its state copied. Both beta.1 RPMs were upgraded to beta.2; the three installed
binary hashes, version and revision, CLI inventory commands and an actual 80×24
TUI start and exit were verified. Guest and network definitions, source-media
metadata and the inactive helper policy were unchanged.

`beta2-network-form-native-001` **passed**: NAT, lab and guest-only TUI previews
each matched the equivalent CLI plan, Back retained settings and cancel applied
nothing. `beta2-job-activity-native-001` **passed** against the retained removal
job.

`beta2-spice-creation-native-001` **passed** end to end: a generated El Torito ISO
was prepared, a VM was created through a reviewed plan and started, and the BIOS
serial marker was read. The 80×24 TUI found its console, and a SPICE viewer
connected on an Xvfb desktop. The fixture `virmill-spice-e79f41dbb545` (creation
`f8202289-350d-48ab-b16d-1170ea59887e`) was stopped through a reviewed hard stop
and is retained. Existing guests, jobs and source media were preserved.

## Failed fixture runs

These failures are retained. Each came from a probe written before later TUI
changes; the observed screens were the current, intended ones.

- `beta2-general-sources-native-001` and `-002`: all 14 source description checks
  passed, then Import showed **Continue your saved setup?** The probe's own qcow2
  walkthrough auto-saves a resumable draft (ADR 0043), which postdates the probe.
  Run `-002` used a private `XDG_STATE_HOME`, which could not help because the
  draft is created during the same run.
- `beta2-tui-workspace-native-001` and `-002`: the probe expected the CPU/RAM
  labels from before `3b74fc7`, then plan IDs on the first review page. The current
  review shows its summary first and the exact identities below it.

Owner state: the first runs of both probes used the default client state. They
left a probe-created import draft in the owner's `~/.local/state/virmill/tui-drafts`
(last written during `beta2-tui-workspace-native-002`). The copy taken before the
upgrade shows no import draft existed. The file referenced only this run's fixture
paths; it was moved, not deleted, to
`~/virmill-tests/beta2-b327d7b/probe-created-owner-state-draft-import.json`
(SHA-256 `8baf26a8c9621cab81f705c3c294ec04f9d7f800089987b68bc46703908bd69e`).
Later probe runs use private state directories.

## Probe corrections and passing reruns

Each correction changed only what a probe expects to see on the current screens.
The installed `b327d7b` build did not change.

- `6e5eb11` matched the current review summary, the Import file browser that
  opens directly, and the saved-setup prompt (the probe chooses Start new).
  `beta2-tui-workspace-native-003` then got through the review, task menus and
  file browser. It stopped where an unused import form returns to the VM's More
  tasks menu instead of the retired source-choice menu.
  `beta2-general-sources-native-003` passed every source walkthrough and stopped
  at Guest tools, which now shows connection readiness first.
- `628ec18` expected the More tasks menu. `beta2-tui-workspace-native-004` passed,
  but its report still claimed a selected source file through a hardcoded field.
  The walkthrough no longer selects one.
- `782b16c` corrected that field and opens the guest tools form through
  **Prepare options / Windows help**. `beta2-tui-workspace-native-005` **passed**
  at 80×24 and 120×36. `beta2-general-sources-native-004` **passed**: qcow2, raw,
  VMDK, VDI, VHD, VHDX, ISO and folder descriptions, source review with the CPU
  and memory choices kept, and the guest tools form. Neither run submitted an
  import or guest tools plan or applied anything, and the owner's draft directory
  stayed unchanged.

UX observation: pressing `i` in the VM list and then Esc twice left the selected
VM's More tasks menu open instead of returning to the list. It was fixed after
this release (CHANGELOG, Unreleased); the published beta.2 packages still behave
this way, and the updated workspace probe expects the fixed behavior.

## Artifacts

| File | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.2.x86_64.rpm` | `752dd92f7967e17e45a780516713d4de4cb859b0e86e1e9b655e4c36ab84b7ae` |
| `virmill-host-helper-1.0.0-0.beta.2.x86_64.rpm` | `d549501656ed422a4d3e453f059fe6dbc728fdfbf4100db25c5108acd1c2f030` |
| `virmill_1.0.0~beta.2_amd64.deb` | `e898a5603d4b7bdbc7459b5ebd7e90648ff789ea1085c830182cf139ddaeffbf` |
| `virmill-host-helper_1.0.0~beta.2_amd64.deb` | `439afc1d9e03ff9efddbe8efda205d2a1dd0d9427901ded01d1c25d05f03b1e5` |
| `virmill-1.0.0-beta.2-source.tar.gz` | `a5d774303ab07539ca5c64a58dc969398dc098d78eb21a4848a6c5c82a46a55c` |
| installed `/usr/bin/virmill` | `8e3cfa951f3a9ec6f7ba26ec40cd856916096c5aa459a823133a0144e9a5512b` |
| installed `/usr/bin/virmilld` | `a61db5bf894070c0e63eb0787b6d25878f1974f783edc5d066697d42f0e1ad3a` |
| installed `/usr/libexec/virmill-host-helper` | `331ca26ba7e0329d644c0da8e96756b3aa422c479dcef7fc1d5fdfe1a4f36c70` |

The packages are unsigned. The DEBs were built on Fedora and not installed on
Debian or Ubuntu. No guest OS family, physical hardware or other distribution is
qualified by these results.
