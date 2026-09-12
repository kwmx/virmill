# 0043 — Private resumable wizard drafts

Status: implemented; integration and native terminal verification must pass
before the installed beta claims restart recovery.

The TUI specification requires creation/import choices to survive client exit.
Memory-only Back handling and durable backend jobs solve different problems.
The owner also requires simple defaults and accessible advanced settings; saving
only basic fields would lose that intent and is not acceptable.

Drafts are frontend preferences under `$XDG_STATE_HOME/virmill/tui-drafts`, separate
from the coordinator's journal. Only allowlisted typed non-secret user choices
may be stored. Cached appliance descriptions, provider XML, observed capabilities,
credentials and approvals are excluded. Source bindings may be hashes; opening a
saved draft must obtain fresh source and backend observations before using it.
A changed source description invalidates dependent selections and must be
explained without silently overwriting the retained draft. This binding includes
the OVA report hash, but disk/ISO descriptions contain metadata rather than a
complete content hash. Resume is not a content-integrity certificate; preparation
retains its independent source verification before mutation.

The local store uses strict `virmill/v1` envelopes, private directories/files,
held no-symlink paths, bounded documents and atomic publication with fsync.
Generation checks prevent one TUI window from silently overwriting another's
choices. Unknown future fields are refused on load and replacement; they are not
silently discarded. Unsupported filesystem safeguards produce a visible error.

Saving a draft never approves a mutation or permits replay of a submitted plan.
The workflow records a submission marker before dispatch and sends users to Jobs
when submission may have happened. Resume creates a fresh review after checking
current source, prepared-artifact and backend identities. Job execution continues
through the existing shared coordinator; the TUI store does not execute host work.

This implements the normative wizard requirement without changing service or
plugin wire contracts, reducing scope or claiming completion of UX acceptance.
