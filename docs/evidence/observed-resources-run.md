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
It will submit 1 CPU/128 MiB → 2 CPUs/256 MiB through an actual 80×24 TUI, compare
CLI and reopened-TUI observations, check unrelated XML/guest/job/media
preservation, and undefine only its exact stopped fixture. An existing running
guest may receive read-only live/persistent resource inspection; no existing
guest is started or edited. Native results remain pending until executed.

All 71 full-release acceptance scenarios remain required. CORE-03/UX-01/UX-02
receive evidence at its actual level, without promoting acceptance status.
