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
- **a More** shows everyday tasks for the selected resource. **Advanced tools...**
  (or **A**) opens specialist actions. Each action includes a short explanation.
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

## Import an image

Choose **Import** or press **i** from Overview or VMs. One explorer accepts OVA,
ISO, QCOW2, raw/IMG, VMDK, VDI, VHD and VHDX files, or a directory containing disk
images and their dependencies. Virmill detects the actual supported format;
extensions alone do not establish what a file contains. Driver availability
still depends on the supported QEMU build.

1. **Review the source.** Selecting a file inspects it automatically and opens
   **Review appliance** or **Review source**. Choose a system when an OVA contains
   several. Edit the VM name, CPU cores and memory directly. Appliance values are
   labeled as detected; disks and ISO images use labeled suggestions where they
   do not provide hardware settings. A disk does not reliably identify its OS,
   firmware or installed drivers. **Advanced settings** opens optional hardware
   choices before any copying. Done or Esc returns to the summary.
2. **Choose where to save.** Select an existing parent with **Save in**, then enter
   a **New folder name**. The reviewed preparation job creates that destination.
   Existing output folders and the original media remain untouched.
3. **Review the disks.** Switch disks with Left/Right. Detected source formats and
   sizes fill the controls. The maximum virtual size is a safety limit, not a
   resize operation; lowering it cannot shrink an existing disk. ISO imports
   offer blank disks for installation. For disk sets, include all required
   backing and extent files; dependencies do not become extra guest disks.
4. **Preview image preparation.** Review the source, destination, disk conversion
   and required storage. Space errors name the shortfall in readable units;
   **Back: Destination** returns to the location choice. Confirm the listed
   consequences and choose **Apply reviewed plan** to start preparation.

For ISO and existing disks, stop programs or VMs using the selected images and
check **Source images are not in use**. The backend repeats source identity,
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
automatically. Otherwise choose **Create VM** and select the prepared images.
The CPU/RAM choices from the source summary carry into this setup.

The ordinary **VM options** page contains name, CPU cores, memory, storage pool
and **Firmware**. Choose BIOS or UEFI to match the original guest or installer.
Unknown firmware is not guessed. **Advanced hardware** keeps machine type,
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
can bypass isolation through the guest. No network exposure is enabled silently.

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

Plans show steps, affected resources, identifiers, risks, storage estimates and
an immutable digest. Enter opens confirmation. Check every required
acknowledgement, select **Apply reviewed plan**, then press Enter. The backend
rechecks authorization and state. Esc returns to review; backing out of review
restores the originating form and its values. Closing help or using an undersized
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

Select a VM, open its details, then choose **Console**. Start it first if the
console choices are unavailable. A graphical display opens on the host desktop;
a plain SSH terminal needs a configured serial console. **Ctrl+]** leaves serial
access. A serial port does not guarantee a guest login. Closing the console does
not ask Virmill to stop the VM.

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

After a VM job completes, **Open VM** takes you to fresh VM details. Creation
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

Remove a stopped VM with **More → Advanced tools → Remove VM**.
The exact-name confirmation and Preview explain retained files and recovery; see
[VM removal](vm-removal.md).

VM removal keeps disks by default. Its unchecked disk choices allow permanent
deletion of selected writable volumes, followed by an exact-file Preview and a
separate data-loss acknowledgement. See [VM removal](vm-removal.md).
