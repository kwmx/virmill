# TUI workspace

Run **`virmill`** in a terminal to open the full-screen interface. `virmill tui`
is equivalent. The noninteractive CLI remains available with explicit commands;
redirected stdin prints help instead of trying to draw a terminal UI.

The default Overview loads real VM inventory, running/stopped counts, available
pool storage and durable jobs. It is a resource workspace, not a command prompt.
The connection remains visible. At 105 columns and wider, sections appear in a
sidebar; smaller terminals use a single content pane and compact navigation.

## Navigate and select actions

- **Tab / Shift-Tab** moves between content, action buttons and navigation.
  Use left/right on buttons, up/down in lists, then Enter to activate.
- **1–9** opens Overview, VMs, Networks, Storage, Templates, Labs, Protection,
  Devices and Jobs. **0** opens Plugins; **,** opens Settings.
- **Enter** on a resource opens its complete details, including stable IDs.
  **/** filters resource names, states and IDs. Enter keeps the filter;
  Esc clears it. Search text never acts as a command.
- **a Actions** opens the current section's action buttons. **: All actions**
  exposes every implemented service action, including import/create, protection,
  devices, guest recipes and plugin tools. Search this catalog with /.
- **r** refreshes observations. **x** in a details page toggles raw data/native
  XML. PgUp/PgDn scrolls long details or plans.
- **?** opens modal help. **Esc** returns to the previous page or cancels a form.
  **q / Ctrl-C** detaches; accepted daemon jobs keep running.

VM details also show labeled shortcuts for Start, Stop, Reboot, Pause, Resume,
Edit CPU/RAM, Capture and Guest setup. These use the selected stable VM identity;
refreshing a list preserves that identity. If it disappears or becomes ambiguous,
select a row again before acting. Neither list selection nor navigation starts a VM.

## Forms and review

CPU/RAM, cold capture, guest recipes, and repository initialization/checks have
dedicated labeled forms. CPU/RAM fields left blank remain unchanged. A stopped
VM is required for the implemented next-boot resource edit. Enter validates the
form and requests a service plan; it does not apply the changes.

Every other implemented action is also reachable through the catalog. Its form
collects a stable ID or explicit path, with a **Parameters file** field for
advanced mappings. That file contains the JSON object described by the CLI
`--input` documentation. The TUI does not ask you to type a shell command or paste
a JSON request envelope. For older examples shaped as `{id,path,input}`, put the
ID/path into their labeled fields and save only `input` as the parameters file.
Use the supplied examples as a starting point. File paths must be canonical and
absolute; symlinks/special files and duplicate JSON keys are refused. The file
is read only after explicit form submission. Credential values remain file
references, not pasted secrets.

Plans show human-readable steps, exact affected resources, complete identifiers,
policy values, risks, estimates and immutable digest. Enter opens the confirmation
page. Explicitly check every required acknowledgement with Space/Enter, select
**Apply reviewed plan**, and press Enter. The TUI submits that exact plan ID and
digest; backend authorization and stale-state checks remain authoritative.
Esc returns to the full review or cancels without applying. Hidden help or an
undersized terminal cannot submit an action. Lost replies direct you to Jobs;
there is no automatic retry with a new operation identity.

Accepted operations open Job details. Refresh to observe progress; detaching does
not cancel them. Use the job's Actions catalog for explicit cancellation or
reconciliation. Reconciliation observes uncertain effects; it does not blindly
replay them. Reopen an existing plan with **Plan show** in Jobs to review it.

## Terminal options and current boundaries

Use at least **80×24** for normal operation. The UI remains navigable down to
60×18; smaller windows retain state and suppress actions until resized.
`virmill --no-color`, `NO_COLOR=1`, or `VIRMILL_NO_COLOR=1` disables color.
`VIRMILL_ASCII=1` uses ASCII framing. State and focus also use text markers.
Untrusted names/output are sanitized before rendering; detail pages wrap complete
identifiers instead of silently truncating them to a table cell.

The catalog covers every currently implemented program action. It does not create
missing backend features: full creation/import wizards, multi-NIC editing, USB
attachment/reconnection and the complete lab/protection workflows still need
implementation or qualification. Advanced creation and import mappings currently
use parameter files rather than a full multi-step wizard. All 71 complete-v1
acceptance scenarios remain required. See the [evidence matrix](requirements-to-evidence.md).
