# ADR 0052: Edit creation boot order as a sequence

Status: implemented; native evidence recorded separately.

The creation form exposed independent integer boot priorities while the existing
shared creation contract requires one complete, unique sequence. An ordinary
installer choice, changing media from priority 1 to attach-only, left its disk at
priority 2 and caused validation to fail. Users had to repair interdependent
numbers manually. This conflicts with the guided creation workflow in specification
document 04. Specification document 05 still requires explicit boot-order review.

Changing Boot priority now means moving the selected device within the sequence.
Other enabled boot devices shift to fill its previous position. Choosing Attach
only for read-only media removes it from that sequence and compacts the remaining
positions; selecting a position again inserts the medium there. Every disk remains
in the boot sequence. This only edits the requested creation declaration.

The form shows a short ordered summary and explains that position 1 boots first.
The existing complete review retains every device's exact numeric bootOrder,
source identity and controller. Attach only remains numeric zero in the shared
CLI/service schema. No API, recipe, persisted-draft format or native adapter changes.

Reordering happens only after a deliberate priority edit. Loading or resuming a
draft, rendering a screen, changing a controller or visiting another page does
not normalize boot choices. Invalid saved orders remain visibly unresolved until
the user edits them. CPU, RAM, firmware, source mappings, controllers, NICs and
device policies are retained. Preview/apply remains separate and revalidates the
complete declaration through the shared service.

The same disk-page review found that the intended installation-media SATA
suggestion tested an obsolete source-kind name. New PreparedInstallation forms
now receive that suggestion only when the host advertises SATA. Raw disks and
OVA controllers are not guessed; restored choices continue to override defaults.

This adds workflow evidence to IMP-01/02/04 and UX-01/02. Form tests
and a real TUI editing test do not prove a guest boots in the chosen order. Native
boot and the complete acceptance scenarios remain separately required.
