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

For an empty field, the explorer starts in your **home folder**. Reopening a
populated field starts at its existing folder or the selected file's parent. Use arrows and Enter to open a folder or select a file.
**Backspace** goes to the parent; **Ctrl+H** goes home. **/** filters names,
**Ctrl+L** enters a path (including ~/), and **.** shows hidden entries.
For folder fields, **Ctrl+S** chooses the current folder. **Ctrl+O** reopens the
explorer from a path field in a form.

Selection fills one field and returns to the form. Esc backs out without applying
anything. The explorer reads names and metadata only. It does not extract media
or create folders. Permission errors stay visible; links and special files are
not offered. New destination names can be typed after choosing their parent.

## Import options

Choose **Import**, then OVA, ISO or existing disks. Each opens a three-page form;
no settings file is required. **Tab / Shift-Tab** moves between options,
**Enter** activates the selected button or file explorer, left/right changes
selectors, and **Space** changes a checkbox. A short explanation follows the
focused option. **Back** or Esc returns one page without losing your choices.

1. **Source:** choose an OVA/ISO file or the folder containing existing disks.
   For OVA, select **Inspect appliance**, then choose the appliance member when
   the archive contains several systems. For ISO, the media name identifies the
   installer; **Advanced verification** accepts an optional publisher checksum.
2. **Destination:** choose an existing parent with **Save in**, then enter a new
   folder name. The reviewed import creates that folder; browsing does not.
   Existing output folders and original media are preserved.
3. **Disks:** use the disk selector to edit each disk. ISO imports offer
   **Add blank disk**, a disk name and capacity in MiB. OVA retains every disk
   attached to the selected appliance. OVA and existing disks require an explicit
   source format and maximum virtual-size limit; that limit does not resize the
   disk. Existing disks offer separate **Add disk file** and
   **Add backing / extent file** buttons. Include every file needed by the chain;
   backing files do not become extra guest disks.

For ISO and existing disks, stop any program or VM using the source images, then
explicitly check **Source images are not in use**. Check every disk's size and
format before selecting **Preview import**. Changing the source clears dependent
inspection and source-confirmation choices. The backend repeats source, format,
dependency and space checks; selecting a filename does not certify its contents.

**Export settings** is optional. It saves the current validated options to a new
private JSON file; choose its folder and filename, then Enter to export. Existing
files are never replaced. The exported object can be reused with the existing
CLI `--input` option, supplying the source path separately. For example:

```sh
virmill import prepare-install /path/to/installer.iso \
  --input "$(cat -- "$HOME/virmill-import-settings.json")"
```

Use `import prepare` for OVA or `import prepare-disks` for a disk-source folder.
Exporting does not create a plan or start an import. The settings file contains
the chosen destination and disk options; check those before reusing it elsewhere.

These workflows **prepare images**. Applying their plans does not define a VM,
boot it or install a guest OS. VM creation remains a separate action.

## Forms and review

CPU/RAM, cold capture, guest recipes, and repository initialization/checks have
dedicated labeled forms. Each input takes one row, with a short explanation only
for the focused field. CPU/RAM fields left blank remain unchanged. A stopped
VM is required for the implemented next-boot resource edit. Enter validates the
form and requests a service plan; it does not apply the changes.

Other implemented actions are also reachable through the catalog. Where a
dedicated form is not yet available, the catalog form
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
missing backend features: the complete VM creation wizard, multi-NIC editing, USB
attachment/reconnection and the complete lab/protection workflows still need
implementation or qualification. Import preparation has native controls; advanced
VM creation mappings still use parameter files. All 71 complete-v1
acceptance scenarios remain required. See the [evidence matrix](requirements-to-evidence.md).
