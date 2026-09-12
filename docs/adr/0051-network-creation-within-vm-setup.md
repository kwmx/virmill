# ADR 0051: Create and refresh networks within VM setup

Status: implemented; native evidence recorded separately.

The creation wizard requires an explicit active network for every original
adapter. Requiring users to abandon that wizard to create one conflicts with
the guided workflow in specification document 04. The network service and
reviewed creation workflow already exist; this decision connects those workflows
without adding a service contract or reducing any acceptance scenario.

The Networks step offers Create network and Refresh networks even when the VM
declaration is unfinished. Create network suspends the exact VM form and its
optional import form. Network creation uses its own ordinary plan, approvals
and durable job. Its job identity never replaces the preparation identity.
Before submission, the private VM draft is durably saved as editing, using the
existing versioned allowlist. No migration or executable draft state is added.

Cancel restores the suspended setup. A matching successful network job returns
automatically only when its detail page has no active dialog or interaction.
Back to VM setup is also available while the job runs or needs attention.
Returning never cancels, retries or replays a network job. After process restart,
the editing draft can be resumed and Jobs retains the separate operation.

Refresh calls only network.list and accepts bounded, unique identities for the
same local connection and source binding. It replaces observed network choices,
not CPU, memory, firmware, disks, pool, adapter models, selected UUIDs or cable
states. Late replies after cancellation are discarded. Missing selected networks
are shown as unavailable. New networks are never selected automatically.

The existing CLI network create/list workflows remain the matching service
access. Creating a network does not prove packet isolation, guest routing or
multi-NIC safety. NET-01/03/04/06 and UX-01/02 remain partial until their complete
scenario evidence passes; JOB-03 receives draft-handoff evidence only.
