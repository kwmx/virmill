# ADR 0032: Organize TUI controls around user tasks

Status: accepted implementation decision, 8 September 2026.

The owner found the action catalog confusing. The previous interface displayed
capitalized CLI command names in an alphabetical list. Following the TUI
specification's visible-action requirement, use explicit task labels and short
explanations, groups by purpose, and selected-resource context. Common actions
belong in the resource toolbar. All tools remains available with section filters.
State-sensitive power buttons reflect observations; the service still validates
state and capabilities when planning and applying. Power tasks with a selected
VM go directly to plan review instead of asking for its identity again.

This changes presentation and navigation only. Shared requests, plans, approval,
backend authorization and durable execution stay authoritative. Every registered
action remains available. Advanced parameter files and missing full-v1 workflows
remain open; this decision neither reduces scope nor certifies usability acceptance.
