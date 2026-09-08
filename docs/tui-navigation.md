# TUI workspace

Run **`virmill`** in a terminal to open the full-screen interface. `virmill tui`
is equivalent. The noninteractive CLI remains available with explicit commands;
redirected stdin prints help instead of trying to draw a terminal UI.

The default Overview loads real VM inventory, running/stopped counts, available
pool storage and durable jobs. It is a resource workspace, not a command prompt.
The connection remains visible. At 105 columns and wider, sections appear in a
sidebar; smaller terminals use a single content pane and compact navigation.

## Choose a task

- **Tab / Shift-Tab** moves between content, action buttons and navigation.
  Use left/right on buttons, up/down in lists, then Enter to activate.
- **1–9** opens Overview, VMs, Networks, Storage, Templates, Labs, Protection,
  Devices and Jobs. **0** opens Plugins; **,** opens Settings.
- **Enter** on a resource opens its complete details, including stable IDs.
  **/** filters resource names, states and IDs. Enter keeps the filter;
  Esc clears it. Search text never acts as a command.
- Common tasks have direct buttons: VM Start/Shut down/Resume follows the
  observed state; Create VM and Import sit beside Details. Other sections expose
  tasks such as Create network, Restore, Read job events and Install plugin.
- **a More** shows a short list of everyday tasks for the selected resource.
  **Advanced tools...** holds saved-state, recovery, diagnostics and other
  specialist tasks. Select that entry or press **A**; Esc returns to common tasks.
  Each task has a brief explanation. All tools retains every implemented action.
- **: All tools** exposes all implemented actions. Left/Right switches sections;
  **/** searches task names and descriptions (for example, CPU). Esc clears a
  search, then returns to the resource page. **i Import** opens source preparation
  choices directly from Overview or VMs: OVA appliance, ISO installer or existing
  disk images. Choosing a source type opens the explorer automatically.
- **r** refreshes observations. **x** in a details page toggles raw data/native
  XML. PgUp/PgDn scrolls long details or plans.
- **?** opens modal help. **Esc** returns to the previous page or cancels a form.
  **q / Ctrl-C** detaches; accepted daemon jobs keep running.

VM details show state-sensitive power controls, CPU/RAM, Capture and More.
Restart, Pause, saved-state controls and Guest setup are grouped under More. These use the selected stable VM identity;
refreshing a list preserves that identity. If it disappears or becomes ambiguous,
select a row again before acting. Neither list selection nor navigation starts a VM.

## Choose files without typing full paths

Import opens a local explorer in **~/images** when that folder exists, otherwise
in your home folder. Use arrows and Enter to open a folder or select a file.
**Backspace** goes to the parent; **Ctrl+H** goes home. **/** filters names,
**Ctrl+L** enters a path (including ~/), and **.** shows hidden entries.
For folder fields, **Ctrl+S** chooses the current folder. **Ctrl+O** reopens the
explorer from a path field in a form.

Selection fills one field and returns to the form. Esc backs out without applying
anything. The explorer reads names and metadata only. It does not extract media
or create folders. Permission errors stay visible; links and special files are
not offered. New destination names can be typed after choosing their parent.

## Forms and review

CPU/RAM, cold capture, guest recipes, and repository initialization/checks have
dedicated labeled forms. Each input takes one row, with a short explanation only
for the focused field. CPU/RAM fields left blank remain unchanged. A stopped
VM is required for the implemented next-boot resource edit. Enter validates the
form and requests a service plan; it does not apply the changes.

Every other implemented action is also reachable through the catalog. Its form
collects a stable ID or explicit path, with a **Settings file** field for
advanced mappings. That file contains the JSON object described by the CLI
`--input` documentation. The TUI does not ask you to type a shell command or paste
a JSON request envelope. For older examples shaped as `{id,path,input}`, put the
ID/path into their labeled fields and save only `input` as the settings file.
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
not cancel them. Use the job's More menu for explicit cancellation or
reconciliation. Reconciliation observes uncertain effects; it does not blindly
replay them. Reopen an existing plan with **Review saved plan** in Jobs to review it.

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
