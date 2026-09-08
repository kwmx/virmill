# ADR 0034: Native import options and optional export

Status: accepted implementation decision, 8 September 2026.

The owner requires editable TUI options and toggles instead of requiring a settings
file for ordinary imports. The earlier generic action form made the existing CLI
input schema a prerequisite for using the TUI. That shortcut conflicts with the
documented workflow guidance and the owner's explicit UX direction. The owner
instruction takes precedence; mandatory v1 scope and service contracts stay fixed.

OVA, installation ISO and existing disk preparation now use Source, Destination
and Disks pages. File selection starts at home for empty fields. Controls expose
disk identities, formats, sizes, included backing files and explicit offline
confirmation where required. OVA inspection uses the existing service and retains
every attached disk of the selected system. The inspection report does not retain
OVF capacity units, so this UI requires an explicit maximum size instead of
guessing a potentially unsafe conversion bound.

Preview emits the existing service input and enters the existing plan review and
authorization flow. Returning from review preserves edits. Source changes clear
source-specific inspection, checksum and offline confirmation. Canceled inspection
replies cannot overwrite the draft. Preparing images does not define a VM or
certify guest boot.

Export is a separate, optional button. It saves the same input object accepted by
the CLI's --input option; the source path remains an explicit CLI argument. The
user chooses an existing folder and a new file name. Private, bounded JSON is
published exclusively from an anonymous file in a held directory. Links and
existing entries are never overwritten. Unsupported filesystems receive an
actionable error. No daemon, plugin protocol, dependency or persisted schema
changes are needed.

Component and workspace tests cover all three forms, complete multi-disk inputs,
cancellation, export, path refusal and terminal constraints. The native terminal
fixture exercises edited ISO options, export and real service preview at two
terminal sizes without applying operations. Its synthetic ISO recognition fixture
does not validate installation or boot. Evidence is recorded separately from
remaining full-v1 acceptance.
