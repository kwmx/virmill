# Observed CPU/RAM settings and TUI edits

The shared `vm.resources.show` method reports exact current/maximum values in
separate live and persistent layers. It does not alter the existing VM provider
fingerprint or durable mutation recipes. CPU and memory edit eligibility use
the existing preservation adapter independently. Layout restrictions, managed
save and shutdown requirements remain explicit.

The TUI loads fresh observations, prefills requested values, skips unchanged
fields and rejects blank/invalid changes. Non-whole-MiB RAM defaults to **Keep
current** without rounding; explicit supported reductions remain available when
the old setting exceeds current edit input limits. Advanced details expose
maximums and full restrictions. Review shows exact before/after values; stale
replies and failed previews retain user choices.

`observed-resources-core-001` passed the full Go race suite. Vet also passed.
`observed-resources-ui-001` passed the TUI/CLI race suites after the final review
summary and label changes. Reader tests cover units, overflow, malformed and
independently invalid fields. Service tests cover identity, canceled reads,
shutdown/save policy, supported reductions, no side effects and bound before/
after reviews. These are source-level tests, not native hardware evidence.

The native fixture recipe uses a new marked diskless, networkless, stopped VM.
It submitted 1 CPU/128 MiB → 2 CPUs/256 MiB through an actual 80×24 TUI, compared
CLI and reopened-TUI observations, check unrelated XML/guest/job/media
preservation, and undefined only its exact stopped fixture. An existing running
guest received read-only live/persistent resource inspection; no existing
guest was started or edited. All checks passed.

All 71 full-release acceptance scenarios remain required. CORE-03/UX-01/UX-02
receive evidence at its actual level, without promoting acceptance status.

## Installed native result — 12 September 2026

Installed CLI/coordinator revision: `3b74fc7a1ce51b7aca51a5e7d9a5859abc914a21`.
Implementation-tree digest: `52eb35fac478d5fa387eb4451b28e934fb9a51a55ae0de50dcf93609b5a124dc`.
The vendored offline Go 1.27.1 build produced RPM and DEB artifacts. Three package
integration tests passed (`observed-resources-packages-001`). Core RPM upgrade and
idle user-coordinator restart passed (`observed-resources-upgrade-001` and
`observed-resources-restart-001`), preserving all 16 prior jobs and their journal
records, guest inventory and source-media metadata. New coordinator PID: 86819.

| Installed artifact | SHA-256 |
| --- | --- |
| Core RPM | `64f28d809d0167a1ee503ca5cb89df0a81e6471f476c64cda9ddcda2f3db1897` |
| `/usr/bin/virmill` | `08a559494598821c65100bbcf4d221bf1edeac97e089efe12c55285fc203e3d7` |
| `/usr/bin/virmilld` | `3bfc37efa8dc841bb49ee10def109d0952d470400c323fadc98dc98669a1291f` |

`observed-resources-native-001` passed on the authorized Fedora 44 VM with libvirt
12.0.0/QEMU 10.2.2. Fresh diskless/networkless fixture
`c769b820-32a8-4055-bc49-b4e297eabc12` used advertised `pc-i440fx-10.2`.
Actual TUI submission created operation `e3555c30-b477-4a1d-bbba-8b93e2c98aac`,
which succeeded. Native XML and CLI reads confirmed 2 CPUs/256 MiB; a fresh TUI
session displayed those defaults. Captured 80×24 editor, before/after review,
confirmation and reopened-editor frames were inspected. Unrelated persistent
XML, existing guests, existing jobs and supplied source-media metadata were
preserved. The exact owned stopped fixture was undefined after verification.

Read-only inspection of running guest `f9c6c7b4-0f62-44ae-8445-850dcac6c98b`
matched native live/persistent resource values and correctly required shutdown
before basic edits. No guest was booted by this fixture. These results qualify
this stopped configuration path, not boot readiness, hotplug, guest performance,
all resource layouts or the complete CORE-03 scenario.

Private full reports and terminal captures remain at
`~/virmill-tests/observed-resources-3b74fc7/resource-settings` on the test VM and
in ignored `build/observed-resources-delivery` locally. Three agents supplied the
XML reader, TUI editor and native fixture; root integrated the shared contract,
review summaries, tests, deployment and evidence. No release was published.
