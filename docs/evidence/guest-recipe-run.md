# Guest recipe transport and service evidence — 2026-09-08

The guest recipe slice provides shared CLI/TUI planning, frozen script and
credential identities, durable stage intents/receipts and explicit non-root
readiness. Its deterministic tests exercise real SQLite jobs with a fake SSH
boundary: check/apply/verify, source edits after approval, stale VM/key/tool
refusal, failures and lost replies, no replay, and withholding raw output.
Those tests do not establish native guest provisioning.

`guest-ssh-native-001` failed strict host-key verification because the selected
known-hosts file trusted the authorized VM hostname, not its literal address.
The fixture copied only those pre-existing trusted public host keys into a
private IP-specific trust file. No new key was trusted and no insecure fallback
was used. The subsequent native run passed.

`guest-ssh-native-003` additionally verified the reviewed SSH executable digest
at launch, real remote exit 3, ordinary UID 1000, exact quoted argument data and
strict refusal with an empty trust store. It used fixed read-only commands
against the authorized test VM itself. Actual local OpenSSH was 10.2p1 with
OpenSSL 3.5.8; its executable SHA-256 was
`ae339eda0cbac945d383cd6519de3e2e53d18acfb6cc634a5276a93f9839b2aa`.
Sealed parent-descriptor credentials worked with this real OpenSSH executable.
No key bytes or raw diagnostics were recorded.

Agent review found and root corrected a gap between plan-time executable
validation and the transport's later reopen: the held executable now must match
the approved digest before execution. Result reads also enforce the selected
connection. The focused regression tests cover those changes.

The native target here was the authorized host, not a nested guest bound by
libvirt. GUEST-02/GUEST-03 still need actual nested-guest recipe qualification,
and GUEST-01 still needs its first-boot/user/key support matrix. Missing guest,
OS, reboot and application support is not converted into acceptance by this
transport evidence. See [operator instructions](../guest-recipes.md).

The final frozen `1eccfc4` adapter passed `beta-owner-ssh-native-001`, including
pre-launch rejection of an incorrect executable digest. Its integrated race
suite and installed package evidence are linked from the [owner handoff](../owner-beta-handoff.md).
