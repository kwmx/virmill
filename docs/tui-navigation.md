# TUI workspace

Run **`virmill`** in a terminal to open the interface. `virmill tui` is equivalent.
Explicit subcommands remain available for scripts. Redirected stdin prints help
instead of drawing a terminal UI.

Overview is the home screen. It shows VM counts, storage pools and their free
space, whether the host is ready (the same check as `virmill doctor`), running
or failed jobs, the last job, and the VM list. Settings (**,**) shows the
connection, display options, version and the full host check with install
commands; **r** checks again.
The header shows the connection and any running or failed jobs; the line above
the buttons lists the keys for the current page. Terminals at least 105 columns
wide show every section in a sidebar and mark the unfinished ones: Templates
(not ready), Labs (validate only), Devices (discovery only) and Plugins
(preview). Narrower terminals list the main sections at the bottom, and **?**
lists them all. Buttons wrap onto a second row when they do not fit.

## Find an action

- **Tab / Shift-Tab** moves between content, action buttons and navigation.
  Use arrows to select a row or button, then **Enter** to open it.
- **1–9** opens Overview, VMs, Networks, Storage, Templates, Labs, Protection,
  Devices and Jobs. **0** opens Plugins; **,** opens Settings.
- **Enter** on a resource opens its details. **/** filters names, states and IDs;
  Enter keeps the filter, and Esc clears it. Search text is never a command.
- Common VM buttons include state-appropriate power actions, CPU/RAM,
  **Boot / installer**, **Guest tools** and Capture. Create VM and Import are
  available from the workspace. Selecting a VM never starts it.
- **a More** shows everyday tasks for the selected resource, with only the power
  actions its state allows. **Advanced tools...** (or **A**) lists the section's
  other tasks. Each action includes a short explanation.
- **: All tools** lists every implemented action. Left/Right changes sections;
  **/** searches names and descriptions. Esc clears the search, then goes back.
- **r** refreshes observations. **x** in details toggles technical data/native XML.
  **PgUp / PgDn** scrolls long details and reviews.
- **?** opens help. **Esc** goes back or cancels a form. **q / Ctrl-C** detaches;
  accepted jobs continue in the coordinator.

Actions use the selected VM's stable identity. If the VM disappears or becomes
ambiguous, select it again before acting.

## Browse files and create folders

The explorer starts in your **home folder** for an empty field. Reopening a path
starts at that folder or the selected file's parent. It does not assume a test
folder such as `~/images` exists.

Use arrows and **Enter** to open a folder or select a file. **Backspace** goes to
the parent, **Ctrl+H** goes home, **/** filters names, **Ctrl+L** enters a location
(including `~/`), and **.** shows hidden entries. **Ctrl+S** selects the current
folder when choosing a folder or a source directory. **Ctrl+O** reopens the
explorer from a form's path field.

**Ctrl+N** opens **New folder**. Enter a name and press Enter to create a private
folder in the current directory. Esc cancels. The new folder is highlighted;
Enter opens it, and Ctrl+S selects it. Existing files and folders are never
replaced. Permission errors stay visible; links and special files are not offered.

Selecting a source reads metadata. It does not extract archives or start an import.

## Create a VM

Choose **New VM** or press **n** (or **i**) from Overview or VMs, then choose a
file. One explorer accepts OVA, ISO, QCOW2, raw/IMG, VMDK, VDI, VHD and VHDX
files, or a directory containing disk images and their dependencies. Virmill
detects the actual supported format; extensions alone do not establish what a
file contains. Driver availability still depends on the supported QEMU build.

1. **Choose the file.** Virmill reads it by itself. Choose a system when an OVA
   contains several.
2. **One settings page.** Name, CPU cores, memory and, for an installer, the size
   of its new disk. Storage, network and display are shown with their defaults.
   **Start it and open its display** is on. Appliance values are labeled as
   detected; disks and ISO images use suggestions. **Advanced settings** opens
   firmware, storage pool, disk buses, networks, cloud-init and other hardware;
   Esc returns with every change kept. Esc on the settings page asks once more
   before discarding them.
3. **Create VM, then Confirm.** One confirmation lists what happens and every
   consequence you agree to: setting up libvirt's standard storage pool when
   there is no usable pool (or starting a stopped one), copying and checking the
   images, creating the VM, starting it and removing the prepared copy. Nothing
   changes before Enter. `d` shows the technical plan.
4. **Progress on the same screen.** One line per step. When the VM is running its
   display opens in its own window; **Open display** opens it again. Esc hides
   the progress, and the steps continue; follow them in Jobs.

If a step needs anything the confirmation did not list, Virmill stops and shows
that step's own confirmation. Prepared copies go in a new private folder under
`~/.local/share/virmill/imports` (or `$XDG_DATA_HOME/virmill/imports`). The
original file is never changed.

For ISO and existing disks, close programs or VMs using the selected images
before confirming; the confirmation says so. The backend repeats source identity,
dependency, format and free-space checks. A quick metadata summary is not a
checksum verification or proof that the guest will boot.

**Tab / Shift-Tab** selects an option, **Left/Right** changes a selector,
**Space** toggles a checkbox, and **Enter** opens a path field or activates a
button. Contextual help explains the focused option. Back retains choices;
changing the source clears settings that depended on it. Cancel inspection stops
the read and retains the source without submitting a job.

Compressed archives such as 7z or ZIP must first be extracted into a new folder.
Loose OVF descriptors and native recovery exports are not interchangeable with
ordinary disk imports; use their documented workflow where available. Do not
assume that every recognized container or guest configuration is supported.

## Configure the VM

Preparation creates independent images. It does not define or start a VM.
When preparation completes while its job is still open, VM setup opens
automatically. Otherwise choose **More**, then **Create VM**, and select the
prepared images.
The CPU/RAM choices from the source summary carry into this setup.

The ordinary **VM options** page contains name, CPU cores, memory, storage pool
and **Firmware**. Firmware the source declares is preselected; otherwise BIOS
is preselected and labelled *Suggested*; choose UEFI if the image needs it. The
storage pool is preselected when libvirt's `default` pool or a single usable pool
exists. With no pool, **Create storage pool** reviews libvirt's standard folder
(`/var/lib/libvirt/images` on the system connection) and creates it as its own
job while the VM form stays open; the new pool is selected when it is ready.
**Advanced hardware** keeps machine type,
CPU model, display, clock, USB controller, memory balloon, watchdog and the guest
agent channel available without putting all those decisions on the first page.
Changing the machine reloads its supported options; unsupported retained choices
must be reviewed rather than silently replaced.

**Continue to disks** reviews storage controllers and boot order. Position 1
boots first. Moving a device adjusts the other positions automatically; the
summary shows the resulting order. For installer media, **Attach only** keeps it
available to the guest but excludes it from booting. Select a numbered position
to boot from it again. These choices appear in the final review.
**Continue to networks** configures each adapter separately. Original
adapters remain represented. Choose a network and a compatible adapter model;
**Disconnected** keeps the cable down on first boot. Connecting multiple networks
can bypass isolation through the guest. No network exposure is enabled silently:
your own disk images and installers start with one e1000e adapter on libvirt's
active `default` NAT network, connected and labelled *Suggested*, and shown in
the review. Appliance adapters start disconnected.

If the network you need is missing, choose **Create network** on this step.
Your VM settings stay saved while you choose the network's purpose and review
its separate creation plan. Cancel returns to the same VM settings. After a
successful network job, setup returns with refreshed choices; **Back to VM
setup** also lets you return while the job is running or needs attention.
Choose the new network for the intended adapter yourself. Its cable stays as
you set it. **Refresh networks** reads the list again without changing any VM
setting. An unavailable selected network is identified so you can resolve it.

Continue catches unfinished fields on the current step and moves focus to the
problem. **Preview VM creation** requests a separate reviewed plan. Creation
leaves the VM powered off; Start is a separate action. Completed creation does not
claim guest boot, provisioning or connectivity verification.

## Guest tools and installer media

For a new VM, enable **Guest agent channel** under Advanced hardware if you intend
to install QEMU guest tools. After the guest boots, **Guest tools** opens a guided
Linux installation form with a guest-system choice and optional desktop tools.
It requires a reachable guest, a non-root SSH account with the documented sudo
access, a key file and verified SSH host keys. Windows has manual instructions;
Linux package installation is not a universal guest adaptation service. See
[Guest tools](guest-tools.md) for supported profiles and verification boundaries.

On a stopped VM, **Boot / installer** opens observed boot devices. **Space**
includes or excludes a device; **Left/Right** moves its priority. **Installer
media** can eject a loaded read-only CD-ROM while keeping its file. Preview shows
the exact next-boot changes before approval. This control does not attach an
arbitrary new ISO, start a VM or prove that OS installation finished. Unsupported
boot configurations remain unchanged with an explanation.

## Review, progress and recovery

If submission fails, the review starts with the complete issue and next steps.
Use **PgUp/PgDn** to read it; your exact plan and settings remain available.
Check Jobs before trying again because an accepted operation may still run.
Missing helper setup explains that an administrator must configure it first.

**CPU / RAM** reads the selected VM again, compares **Live** and **Next boot**
values, and fills the requested settings for you. Only changed fields enter the
review. Running guests offer a separate graceful-shutdown preview; unsupported
layouts explain which setting needs advanced configuration. See
[CPU and memory settings](cpu-memory-settings.md).

Dedicated forms cover creation/import, CPU/RAM, boot order, guest tools, backup, restore and cold
capture, guest recipes and repository initialization/checks. Each requests a
shared service plan before any mutation. Some specialist catalog actions still
use a **Settings file** for mappings described by the CLI `--input` contract;
the TUI does not require a shell command or pasted request envelope.

Every change opens one plain confirmation (ADR 0065): what will happen, then
"By confirming, you agree that:" with every acknowledgement the plan requires
in plain words, then **Confirm**. Enter confirms and submits the exact plan with
each acknowledgement named. `d` switches to the technical details: steps,
affected resources, identifiers, storage estimates and the immutable digest.
The backend rechecks authorization and state. Esc leaves the technical details,
and from the confirmation returns to the originating form with its values. Closing help or using an undersized
terminal cannot submit changes.

Accepted operations open Job details. Jobs refresh automatically every three
seconds and show the current status, failure explanation and available next
actions. **Activity** opens the selected job's recorded progress and errors
without asking for its ID. Use PgUp/PgDn or arrow keys to read full messages;
Tab selects **Refresh**, available **Older events**/**Newer events**, or **Back to
job**. Active jobs refresh automatically until the current page is full or a read
fails. A read failure explains the issue and retains loaded events; choose Refresh
to retry. **Checked** shows the last successful read time in UTC. Esc returns to the same job. Detaching does not cancel a job.
Use **Cancel job** when offered; cancellation may wait for a safe boundary.
**Check recovery** observes uncertain effects rather than blindly retrying them.
Review an existing saved plan through **Review saved plan** in Jobs.

After a lost apply reply, inspect Jobs before submitting again: acceptance may
already have occurred. There is no automatic retry with a new operation identity.

## Export settings and terminal options

**Export settings** saves validated import or creation choices to a new private
JSON file. Choose a folder and filename; existing files are never replaced.
Export does not start a job. CLI users can reuse that object with `--input`,
supplying the source or prepared-operation identity separately. Inspect saved
paths before reusing settings on another host.

Import and VM setup choices are saved automatically on this computer, including
advanced hardware choices. When you reopen setup, choose **Resume saved setup**
or **Start new setup**. Virmill re-inspects the source and available hardware
before restoring the choices. If preparation was submitted, **Continue prepared
setup** checks its job first; an uncertain submission leads to Jobs instead of
repeating it. Saving choices never approves an operation.

Drafts live under `$XDG_STATE_HOME/virmill/tui-drafts` (normally
`~/.local/state/virmill/tui-drafts`). They exclude credentials, approvals and
cached appliance metadata. Keep one setup window active: another window changing
the same draft pauses saving and shows an explanation. Storage errors remain
visible; do not assume choices were saved after an error. Export remains useful
for a separate reusable settings file. Disk and ISO resume checks compare
reported metadata; preparation independently verifies media before mutation.

Use **80×24** or larger for normal operation. Narrower layouts retain choices;
very small windows ask you to resize. `virmill --no-color`, `NO_COLOR=1`, or
`VIRMILL_NO_COLOR=1` disables color. `VIRMILL_ASCII=1` uses ASCII framing. Text
markers identify focus and state, and untrusted output is sanitized.

This page describes implemented controls, not a complete-v1 support certificate.
Hardware and guest checks retain their recorded evidence level. All 71 acceptance
scenarios remain required; see the [requirements-to-evidence matrix](requirements-to-evidence.md).

## Open a guest and protect it

Select a running VM and choose **Display**, in the VM list or its details. A VM
with one graphical display opens it straight away, in its own window on the
host desktop, and Virmill stays usable while it is open. A VM created and
started in one confirmation opens its display by itself. With several displays
or a serial console, Display lets you choose; a plain SSH terminal needs a
configured serial console, which takes over the terminal until **Ctrl+]**. A
serial port does not guarantee a guest login. Closing the display does not stop
the VM.

New creation forms suggest **Local display (SPICE)** when the host supports it.
The display choice and desktop requirement are shown before creation. Advanced
hardware keeps the other observed display choices, and saved choices remain intact.

The beta graphical adapter supports private SPICE with virt-viewer 11.0 installed
by the administrator. Clipboard, sound, USB redirection and resizing stay off.
Other viewer versions and VNC currently report an explicit unsupported reason.
Missing tools or display access produce guidance, never an automatic host change.

**Guest tools** shows Linux profile detection and SSH credentials first. Supply an
existing private key and verified known-hosts file; do not paste key contents.
Open **Advanced** for desktop tools or a different SSH port, then choose
**Preview installation**. The guest must already be reachable, have its agent
channel enabled and permit the reviewed guest administrator operation. Windows
and unsupported distributions retain manual instructions.

In **Protection**, select a recovery point and choose **Back up** or **Restore**.
Back up selects an initialized encrypted repository and its password file with
**Ctrl+O**. Restore asks for a new VM name and an observed active directory pool.
The restored VM is stopped and disconnected; originals are retained. Firmware/TPM
restoration is still unavailable and affected recovery points are refused.
Review the plan before applying. Back from review preserves your choices.

### Completed jobs and guest tools

After a VM is created, **Start VM** opens it and shows the start review. After
any VM job completes, **Open VM** takes you to fresh VM details. Creation
checks its saved receipt first; the completed copy/definition is separate from
booting and configuring the guest. If the result cannot be verified, **Refresh
result** retries the observation and leaves the job unchanged. Activity and advanced
job tools remain available.

**Guest tools** checks the selected guest before asking for SSH settings. A
stopped guest with its management channel configured offers **Preview start VM**.
After starting, return through **Open VM → Guest tools**. Paused or saved guests
explain which state to resolve first. **Prepare options / Windows help** remains
available for advance preparation. Running with a configured channel opens the
installation form; neither condition alone proves software is installed.

### Create a network without writing a file

**Networks → Create network** opens name, purpose and subnet controls.
**Advanced options** exposes access, DHCP/DNS and route advertisement. Read the
short access summary and choose **Preview network**. Back retains your choices;
applying always uses the separate reviewed-plan confirmation. **Export settings**
saves the declaration for reuse. **Advanced declaration file** remains available
for an existing document. See [network creation](network-creation.md).

When network preview reports an issue, **Read full issue** opens its complete
message and recovery instructions. Use PgUp/PgDn or arrows to scroll; Esc
returns to the same settings. An unresolved allocation must be reconciled from
its original declaration; typing another subnet does not bypass that check.

If the background service cannot be reached, Overview and Settings show startup
instructions and **Retry connection**, with optional sign-in setup under
**More help**. See [coordinator startup](coordinator-startup.md).

**VMs → More → Change VM automatic startup** shows a fresh current value and
a requested toggle with a separate Preview button. See [automatic startup](vm-autostart.md).

Remove a stopped VM with **More → Remove VM**.
The exact-name confirmation and Preview explain retained files and recovery; see
[VM removal](vm-removal.md).

VM removal keeps disks by default. Its unchecked disk choices allow permanent
deletion of selected writable volumes, followed by an exact-file Preview and a
separate data-loss acknowledgement. See [VM removal](vm-removal.md).
