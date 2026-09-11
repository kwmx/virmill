# Beta workflow audit — 11 September 2026

Audit baseline: `0232434fda892423aeea275519412ecc6169608b`. This is a focused
source and retained-evidence review, not an installed-system test or a release
certification. The parent coordinates subsequent integration and remote tests.
Requirements come from the supplied TUI, acceptance and packaging specifications
(`04`, `13`, `15`) and the owner's request for usable defaults, discoverable
advanced controls, and complete flows without mandatory settings files.

## Five highest-impact gaps at the baseline

| Priority | Reproducible workflow gap | Code evidence | Acceptance and evidence needed |
| --- | --- | --- | --- |
| 1 | ISO import can prepare and define a VM, but the user cannot open its graphical installer or serial console from the TUI. Guest tools likewise assumes existing SSH credentials and cannot establish first access. | `internal/ui/registry.go` has readiness inspection but no console, viewer or interactive SSH command; `workspace.go` VM buttons offer power, CPU/RAM, Guest tools and Capture. | IMP-01, DEV-04, GUEST-01/03, UX-01. Add shared, capability-aware guest access and a visible next action after definition/start. Prove an actual installer/guest console opens on the selected guest, missing viewer/SSH prerequisites give actionable guidance, and endpoints stay within reviewed exposure policy. A tiny definition fixture does not prove this. |
| 2 | Protection → Restore and Back up still ask for an optional settings file even though successful requests need concrete destination/repository inputs. The user must know the JSON contract to complete common recovery work. | `action_forms.go` `actionParameterFields` and `NewActionForm` expose `parametersFile` for `snapshot.restore` and backup mutations; `workspace.go` only special-cases capture and repository init/check. | SNAP-01, BAK-01/06, UX-01/02. Dedicated forms should select capture/repository/pool, show defaults and credential file pickers, preserve review-back state and emit the existing shared request. Compare CLI/TUI plans, then complete a disposable restore and verify source preservation. Firmware/TPM restrictions must remain explicit. |
| 3 | A selected VM's hardware editor exposes only CPU/RAM. CLI-supported boot order and retained-media ejection cannot be reached through its normal `vm set` TUI route, leaving an installed guest with no guided way to remove its installer. | `workspace.go` routes selected `vm set` to `guided("resources")`; `guided_forms.go` resources fields/request include only CPU/RAM. `app/service.go` accepts `bootOrder`/`ejectMedia`; `vm.boot.get` exposes structured observed devices. | CORE-03, UX-01/02. A stopped-VM boot/media form must select only observed disk targets/NIC identities, preserve media files, require a replacement order when ejecting a boot candidate and review one shared plan. Test unavailable devices, stale state, 80×24, and real disposable media ejection/boot order. |
| 4 | Preview → Esc discards entered CPU/RAM, capture, guest-recipe and repository form values. Returning to edit the plan therefore makes the user repeat setup. | The `plan` reply in `workspace.go` clears `Form` and `ActionForm`, saving only `guest-tools`; plan Esc restores only `SavedGuestTools`. | UX-02, UX-01. Save the originating form for the particular review and restore it on Back. Test all form families, failed previews, canceled review and selecting a different operation so an unrelated stale form cannot reappear. |
| 5 | Create VM → Import new images reopens the old source-type catalog, while direct Import opens the unified browser. Users encounter two competing flows for the same operation. | `workspace_creation.go` `updateCreation` enters `CatalogMode = "import"`; `workspace.go` direct `i`/import action uses `openImport("auto")`. | IMP-01/03, UX-02. Route both entry points through the same source browser, preserve Back behavior, and walk both at 80×24 and 120×36 with ISO and disk sources. |

The boot/media form is being implemented in `boot_form.go` and its focused tests
as a follow-on to this audit; this note does not claim workspace integration or
native hardware validation. The parent owns fixes and evidence for the other
findings.

## Other confirmed gaps and documentation drift

- Import/creation drafts live in `Workspace` memory (`SavedImport`,
  `SavedCreation`, `PreparedBasics`), so closing and reopening the client loses
  unapplied choices. The normative wizard requires non-secret draft persistence.
  Durable jobs surviving client exit is separate evidence and does not meet this
  requirement.
- `docs/tui-navigation.md` still describes a three-type import chooser, no folder
  creation, and mandatory source-format entry. The current code has the unified
  metadata browser, `Ctrl+N` folder creation and detected formats. Updating this
  guide and installed copies is required by REL-03; old instructions make the
  improved flow harder to use.
- Existing guests without an agent channel cannot use automatic guest tools
  until that channel is configured outside the current editor. The optional
  channel is currently a creation setting. Linux guest package installation,
  handshake, clipboard and resize remain unverified in retained evidence.

## What existing evidence does establish

`general-sources-run.md` records installed six-format QEMU metadata reads,
owner OVA/ISO/VDI inspection, two terminal-size metadata/settings walkthroughs,
and definition of a new powered-off VM with an agent channel. It explicitly
does not establish guest boot, guest package installation, or desktop
integration. The retained recovery run establishes a scoped encrypted two-disk
BIOS backup/restore and boot marker. These are useful workflow foundations;
neither substitutes for the missing ends of the user journeys above.

No broad test suite or remote mutation was repeated for this audit. Reported
code gaps were identified by reading the dispatch paths and their shared
contracts, rather than treating registry presence as complete functionality.

## Beta versus publication decision

The owner-test beta is permitted by ADR 0030. Full 1.0 remains at 2 accepted,
57 in progress and 12 not implemented in `docs/evidence/requirements.json`.
All 71 scenarios remain required. The shipping checklist is still incomplete,
including physical USB/bridge tests, mandatory remaining implementations,
firmware/TPM restoration, real guest setup and the complete install/support
matrix. Environment gaps and unfinished adapters must remain separate.

An improved testable beta must pass complete selected TUI journeys, including
their error and Back paths, with accurate installed instructions and fixed
artifact hashes. Publication preparation can organize reproducible artifacts,
checksums, release notes and clearly scoped support evidence. Actual publication
requires owner review and explicitly chosen real repository/download/signing
coordinates. Example domains and module paths are not destinations. This audit
does not approve publishing or reduce mandatory 1.0 scope.

## Integrated follow-up

Revision `47d1a8b` resolves the baseline boot/media form, lost review input and
divergent import entry points. Native 80×24 keyboard-only creation and subsequent
boot-order editing passed, with automatic job completion and unchanged unrelated
VM configuration. See [recorded evidence](beta-ux-flow-run.md). Console/access and
settings-file-dependent protection workflows remain open; publication is not yet
qualified.
