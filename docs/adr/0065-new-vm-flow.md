# ADR 0065: One New VM flow, one plain confirmation, and the display on start

Status: accepted; implemented. The acceptance walkthrough passed natively on
`9a2f6dd` ([record](../evidence/new-vm-run.md)).

## Context

The owner could not create or import a VM in beta.5 and start working, which is
Virmill's most important task. Every step probe had passed. The step probes had
proved that each operation worked; no one had walked the path a person takes
from "I have an image" to "I'm using the VM" on a host with nothing set up.

`new-vm-baseline-native-001` measured today's best path, from a private session
with no pools to a running VM from a disk image:

- 72 keypresses and 77 screens. 32 of the keypresses are Tab presses to reach
  buttons, and confirming the source is not in use alone takes 11 Tabs and a space.
- Two separate reviews, the pool and then the import, with 10 checkboxes. Each
  checkbox shows a raw identifier such as `[write-import-artifacts]`, beneath
  `Plan: <uuid>`.
- The review appears under the Overview heading and starts with
  `Affected resources: disk-source:local:64512:125107209` and warnings about
  "OFD guards" and "cooperating QEMU writers".
- VM settings open at "Step 1 of 3" with the pool set to `< Choose… >`.
- It ends on a job page. The VM's display is never offered.

That is the path where everything works. The owner took a slightly different
path, and that path exposed more problems:

- The in-setup Create storage pool button appears only on the first page of the
  VM settings form. Advanced settings, a resumed setup, and leaving for the
  Storage page and coming back all skip that page. Coming back also silently
  drops the one-approval chain, which splits the flow into separate reviews.
- Create VM leads into Import, and Import can send the user back to Create VM.
- A saved-setup prompt appears on entry, and the step counter jumps from 1 to 3.
- The display exists only as "Console" inside a VM's details. Nothing offers it
  after start, and the TUI freezes while the viewer is open.

## Decisions

The owner chose these on 2026-09-16.

1. **One New VM flow.** Create and Import become one flow. You choose an ISO,
   disk image or OVA and see one settings page with good defaults. You confirm
   once and follow progress on the same screen until the VM runs.
2. **One plain confirmation** for every change Virmill makes. The confirmation
   says what will happen and lists every consequence you agree to, in plain
   words, with nothing left out. It has one Confirm. The full technical plan is
   one key away. The per-item checkbox list is removed.
3. **The display opens by itself** when a VM made with New VM starts. Running VMs
   get a Display button, which replaces "Console".
4. **When no usable storage pool exists, the same confirmation includes it.**
   libvirt's standard `default` pool is created, or a stopped one started, as the
   first step. Choosing a different pool or folder stays in Advanced settings.

## Design

**Confirmation.** A plan screen shows three things in order:

- the action and what will happen;
- "By confirming, you agree that:" followed by each acknowledgement's plain label,
  with the later steps of an approved chain listed after it;
- a single Confirm button.

`d` switches to the technical details: the complete plan, identifiers, digests,
resources and planner notes, as before. Confirm applies with every
acknowledgement named explicitly, exactly as the checkbox list did. A consequence
Virmill has no plain label for is shown by its identifier, never hidden. The CLI
is unchanged: `plan apply` still takes each `--ack`.

**New VM.** New VM (`n`) replaces the separate Create VM and Import entries. It
runs in five stages:

1. Choose a file. Virmill inspects it.
2. One settings page: name, CPUs, memory, disk size for an installer, storage,
   network and display. It also has "Start and open the display", on by default.
   Advanced settings expand in place and keep everything else: firmware, pool and
   folder, network, disk bus, cloud-init, and keeping the prepared copy.
3. Confirm, as above. It covers the pool when needed, preparation, creation,
   start, the display, and removing the prepared copy.
4. Progress on the same screen, one line per step. The flow never switches to
   the Jobs page.
5. "NAME is running", with the display open and buttons to open it again or go
   to the VM.

New VM always starts fresh. It shows no saved-setup prompt, and Esc never
discards settings without saying so.

**Pool in the same approval.** The pool plan and the preparation plan are both
shown in the one confirmation. The pool is applied first, and creation waits
for it. A pool created in this approval is approved by name and folder. If the
pool that exists when creation is planned is not that one, the chain stops at
its own review, as it does today for any unapproved difference.

**Display.** Only the graphical viewer is launched detached, so the TUI keeps
working while it is open. The serial console still takes over the terminal, as
it must. The private viewer settings folder is removed when the viewer exits.

## Acceptance

A flow is done when a TUI walkthrough on the authorized test VM passes. It runs
in a private session that starts with no pools, under a virtual display. It
covers a disk image, an installer ISO and an OVA, and each case must:

- need exactly one approval;
- take at most six keypresses, not counting choosing the file;
- show no raw identifiers, digests or resource IDs on a default screen;
- never switch to the Jobs page;
- end with the VM running and its viewer process started;
- leave the host's own pools, networks and VMs unchanged.

## Consequences

- The earlier per-item confirmation and the golden-path probes that drove it are
  superseded. They remain as the record of the builds they tested.
- Every other plan in the TUI gets the plain confirmation too.
- Drafts no longer offer to resume on entry.
