# 0048 — Collect network intent in the TUI, validate it in the shared service

Status: implemented; scoped validation recorded in the evidence ledger.

Create network previously required a file even for supported ordinary profiles.
This conflicts with the TUI's required creation controls and the owner's explicit
request to offer settings instead of asking users to import them.

The TUI collects a complete virmill/v1 Network declaration, with simple purpose,
name and subnet choices plus explicit advanced access and addressing controls.
It supplies network.create with input.document. The same service accepts either
this object or the existing file path, never both. Both paths pass through the
same bounded declaration/schema validator and produce the existing immutable
network recipe. CLI network create [PATH] supports the same inline envelope via
--input. Shape and policy errors retain the shared structured error response.

No new network profile, privilege or durable schema is introduced. Existing
recipe versions, UUID-derived native identities, helper grants, collision checks,
apply/reconcile and packet-verification boundaries remain unchanged. NAT means
host-reachable egress including LAN; it does not imply internet-only filtering.
Services-only DHCP/DNS can use host upstream DNS. Guest-only needs static guest
addressing; omitted CIDR supplies no logical reservation. IPv6 remains explicitly
disabled for these supported creation adapters, with no new isolation claim.

Export writes the full declaration as a new JSON file via the existing safe
export path. The advanced declaration-file flow remains available. Preview Back,
errors and canceled observations preserve in-memory choices; closing the form
or exporting it does not apply a plan. Networking support still requires its
original packet/guest/hardware acceptance, independently of this UI work.
