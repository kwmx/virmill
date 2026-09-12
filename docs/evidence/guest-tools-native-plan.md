# Native Linux guest-tools qualification plan

Status: **Fedora 44 CLI installation, idempotent repeat and native agent ping
passed in guest-tools-fedora-live-002; full cross-profile/desktop/TUI submission
qualification remains open**. See [observed results](wizard-drafts-tools-run.md).
Scope: GUEST-03 package installation and fixed host-to-agent handshake, UX-01
CLI/TUI flow; GUEST-01 only where the actual supported first-boot profile runs.
The root agent alone may run SSH or mutate the owner-authorized disposable host.
This plan does not authorize a different host, an OS download, or publication.

## Fastest useful route and current blockers

Use a new independent copy of a supported Fedora disk already present on the
authorized host, if read-only preflight proves it suitable. The ignored host
record lists a stopped `virmill-nested-fedora` with `ownedByThisRun: false`;
historical pool observations include `nested-fedora.qcow2` and
`nested-fedora-seed.iso`. These are leads, not verified current identities,
Fedora version, cloud-init support, credentials, or permission to modify that VM.
The root agent subsequently confirmed UUID
`2ec994ce-2950-498c-8b19-d2f7dbb53a78`, stopped with no managed-save state, and
disk `/var/lib/libvirt/images/virmill-test/nested-fedora.qcow2` plus read-only
`nested-fedora-seed.iso`. The full observation is retained in ignored
`build/guest-tools-preflight-inventory.json`. Recheck that exact identity before
copying. Never start the original guest, change
its XML, reset its password, attach media, or edit its original disk/seed.

The supplied Kali 2026.2 QEMU/VirtualBox/VMware/Hyper-V images and installer ISO
are suitable for their image-format/boot tests, but do not qualify current
Fedora/Debian/Ubuntu guest-tools profiles. `internal/guestsetup/tools.go` checks
exact `/etc/os-release` `ID` values `debian`, `ubuntu`, or `fedora`, including for
`linux-auto`; `kali` is rejected before package installation. Never alter
`os-release` or choose Debian to defeat that guard.

The existing console fixtures are BIOS marker programs without a package
manager, SSH, or systemd. They cannot be reused as package-install evidence.
`tests/fixtures/lifecycle/reboot_native.py` does provide a useful native
independent-copy/preservation pattern; it is a copied Kali boot test, not a
supported guest-tools bootstrap.

The product's first-boot profile is currently only
`nocloud-netplan-ipv4-v1` (`internal/app/provision/nocloud.go`). It requires
Netplan/networkd and explicitly disabled IPv6. An uninspected Fedora cloud image
must not be declared compatible. A fixture-specific Fedora NoCloud bootstrap
can establish the guest-tools test bed, but does **not** qualify the product's
first-boot implementation or claim GUEST-01 acceptance.

## Read-only preflight the root agent can perform now

1. Verify staged and installed binary hashes, ordinary UID 1000, exact authorized
   hostname, and idle operation state. Save all existing VM UUID/state/inactive
   XML hashes, network XML/state, pool/volume metadata, and source-media
   device/inode/size/mtime/ctime. Record observed native package versions.
2. Resolve the named Fedora guest through native inventory. Require it stopped,
   persistent, without managed-save state; resolve each disk through libvirt
   volume metadata. Record format, virtual/allocated sizes, external backing,
   encryption and auxiliary firmware/TPM dependencies without guessing paths.
3. Determine whether an unused supported cloud image already exists. Otherwise,
   require a single standalone Fedora disk with no firmware/TPM dependency before
   making the simple independent fixture copy. If the source has a backing chain,
   encrypted storage, or unaccounted state, stop this route and record the exact
   blocker rather than constructing a partial clone.
4. Inspect OS/boot/bootstrap facts through a confined libguestfs inspection tool
   against a private independent copy, if already installed or root explicitly
   elects to install it. Do not mount an untrusted guest filesystem on the host.
   Needed facts: actual `ID`/`VERSION_ID`, architecture, BIOS/UEFI requirements,
   systemd, cloud-init version/datasource configuration/cache policy, renderer,
   SSH server, sudo, and whether `qemu-guest-agent` is already installed. Do not
   print old user-data, private keys, passwords, or unrelated guest files.
5. Record native helper tools actually installed, especially `qemu-img`,
   `xorriso`, `ssh-keygen`, and any chosen libguestfs tool. No version in this
   plan is an asserted observation of the nested guest.

Root also observed `/usr/bin/virt-inspector`, `/usr/bin/virt-customize`,
`/usr/bin/guestfish`, and `/usr/bin/qemu-img` installed. Their versions and the
source content hash still need recording. Run these image tools as the ordinary
actor against the private copy, never as root and never against a live guest.

No executable native recipe has been added yet: guest inspection, bootstrap
compatibility, source hash, and selected new network are unresolved. The concrete
copy-only bootstrap below is ready for root review; a runnable mutation script
must consume those verified facts rather than guess them.

## New fixture and network

Give every fixture artifact a fresh UUID and ownership marker. Independently
copy/convert the stopped disk into a new exclusive destination and verify its
initial content against the source. Prefer an independent sparse/reflink copy
over a backing overlay: the test must not depend on the unrelated source guest
remaining stopped. Preserve originals and source hashes; retain the fixture
copy for failure examination. Boot only the new persistent guest.

Use the existing shared `network.create` plan/apply service with a unique name,
one new conflict-checked subnet, NAT, explicit `hostAccess: allow`, IPv6 disabled,
and the consciously reviewed egress policy. `examples/networks/allowed-host-nat.yaml`
shows the contract; its sample CIDR is not a reservation. The guest needs a
reachable non-loopback literal address for SSH and distribution repositories
for package installation. `internal/guestsetup/service.go:validTarget` rejects
loopback, IPv4-mapped IPv6, noncanonical addresses, root login, and zero ports.
A localhost-forwarded SSH port is therefore not an equivalent test route.

Attach only the new fixture NIC to that new network. Use one locally administered
MAC and a static or observed DHCP address bound to that exact NIC. Do not change
an existing default network, share a host folder, enable USB forwarding, or
borrow an unrelated VM's lease. If the shared service lacks authority for this
network, record that specific prerequisite instead of silently falling back to
host shell firewall commands.

## Preferred copy-only Fedora bootstrap after root preflight

This route avoids depending on the old cloud-init instance or seed. It requires
actual `ID=fedora`, systemd, OpenSSH server, sudo and NetworkManager inside the
copy. If any is absent, stop and report it; do not install bootstrap packages
using libguestfs networking or silently switch distributions.

1. Root copies the verified stopped source into an exclusively created test
   location, makes only that copy actor-readable/writable, and records initial
   source/copy hashes. The actor invokes `virt-inspector --format qcow2 -a COPY`
   and checks the reported OS and mounts. Reject encryption, ambiguous multiple
   OS installations, or firmware/TPM requirements not covered by the copied
   fixture. Keep the source seed detached from the new guest.
2. Generate a private client Ed25519 key with `/usr/bin/ssh-keygen`. The bootstrap
   script has a fixed body and receives only generated fixture values: fresh VM
   UUID, constrained MAC, and addresses from the newly created network's actual
   definition. Write the script and public-key/network files into the private
   test directory; record script hashes, never client private key contents.
3. Invoke `/usr/bin/virt-customize --no-network --format qcow2 -a COPY --run SCRIPT`
   as UID 1000. Supply uploads as separate fixed arguments or through prepared
   guest files. The script checks its Fedora prerequisites before changes,
   creates only user `virmillprobe` with `/bin/bash`, sets an unusable password
   without locking public-key access, installs the generated authorized key,
   and installs an exact mode-0440 sudoers entry validated with `visudo`.
4. On this copy only, disable cloud-init to prevent copied instance state from
   overriding the fixture identity. Replace guest NetworkManager connection
   files with one mode-0600 MAC-bound static keyfile using the reviewed address,
   prefix, gateway and DNS; IPv6 is disabled. Do not infer the guest interface
   name. Use a minimal reviewed SSH configuration allowing only `virmillprobe`,
   public-key login and no root/password/keyboard-interactive login; validate it
   with `sshd -t` inside the appliance. Enable NetworkManager and sshd for boot.
5. Remove only the copy's old SSH host-key files and run guest `ssh-keygen -A`.
   Read only the new Ed25519 **public** host key back using ordinary-user
   `guestfish --ro --format=qcow2 -a COPY -i cat /etc/ssh/ssh_host_ed25519_key.pub`.
   Build the exact IP-specific private known-hosts file from that offline key.
   This avoids copying a guest host private key into test seed files entirely.
6. If `qemu-guest-agent` is installed in the copy, remove that exact package with
   a normal guest RPM transaction under this reviewed fixture preparation.
   Refuse dependency errors; never use `--nodeps` or remove arbitrary packages.
   Recheck package absence. The later Virmill job, not the bootstrap, installs it.
   Apply supported SELinux relabelling to the copy so injected key/config paths
   work under the guest's existing enforcement policy; do not disable SELinux.
7. Before boot, recheck source XML/state/hash, copy ownership, network UUID/MAC,
   image format and new VM identity. Define a new persistent guest with only its
   copied disk, one fixture NIC, serial console and the explicit agent channel
   policy under test. Do not carry original NICs, host devices, shared folders,
   old seed media, or auxiliary device paths into the new guest.

This is a test-bed preparation procedure, not implementation of a general image
customizer. libguestfs explicitly supports ordinary-user copy customization and
`--no-network`; it requires the guest disk be offline.
[virt-customize reference](https://libguestfs.org/virt-customize.1.html).
The network configuration must match the installed guest NetworkManager version;
its keyfile format supports a MAC-bound Ethernet connection with explicit IP
settings. [NetworkManager keyfile reference](https://www.networkmanager.dev/docs/api/latest/nm-settings-keyfile.html).

The root agent still needs: source hash/standalone format proof; independent-copy
inspection showing exact Fedora release and bootstrap prerequisites; installed
libguestfs/SSH-tool versions; free-space budget; and a newly created, conflict-free
NAT network UUID/subnet/host-reachability proof. No guest package install or
bootstrap pass is inferred from the host tools merely being present.

## Bootstrap credentials without trusting a network scan

Generate a fresh test SSH client key and a distinct fresh guest SSH host key in
a private fixture directory. Bootstrap only the new copied guest through its
verified cloud-init mechanism, or a specifically reviewed confined offline
editor. Install the test client's public key for a new non-root test user;
enable passwordless sudo explicitly for that fixture user. Disable password
and root SSH login. No client private key is put in guest media.

For a verified NoCloud-compatible image, an exclusively created `cidata` seed
can carry the new instance ID, test user, authorized public key and guest SSH
host key using the documented `ssh_keys` fields. Pin the matching public host
key in a private IP-specific known-hosts file **before the first connection**.
Host-key material in seed/private guest files is fixture secret data: never
write it into tracked evidence, command logs, or release artifacts. Cloud-init's
[SSH module](https://docs.cloud-init.io/en/latest/reference/modules.html#ssh)
and [NoCloud datasource](https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html)
document these mechanisms; compatibility still depends on the version observed
inside this image. Cached datasource state may prevent a new seed from running;
fix only the independent fixture copy after reviewing the exact cache state.

Alternatively, read the new guest's generated public host key through a trusted
local console/offline channel and bind it to the observed NIC/IP. `ssh-keyscan`
may compare a presented key with that already trusted key; it never establishes
trust by itself. Every SSH connection uses strict host-key checking, the exact
private key and known-hosts paths, and no forwarding. An empty/mismatched trust
file must fail before a package operation.

## Actual test sequence

1. Start the new persistent guest through Virmill's reviewed lifecycle flow.
   Allow at most five minutes for bootstrap. Confirm the observed OS identity,
   non-root UID, cloud-init status where used, sudo, repository reachability,
   and absent agent package. Record only nonsensitive identity/status output.
   If the package is already installed, the first run proves the already-set-up
   path only. To prove installation, remove it only inside this independent
   fixture copy under an explicit fixture preparation step, then record absence.
2. Begin without an agent channel if testing root's retrofit implementation.
   From the TUI use the new channel setup flow, inspect the plan, apply the exact
   required stopped/next-boot transition, and start the fixture again. A newly
   created VM can instead use `hardware.guestAgent: true`; record which path ran.
   Verify exactly one native `org.qemu.guest_agent.0` channel. XML presence alone
   is not agent readiness.
3. At 80×24, open the new guest's Guest tools form, select the exact observed
   distribution, leave desktop integration off, and choose the verified SSH
   key/trust files. Review and apply through keyboard input. Save the plan ID,
   frozen built-in recipe digest, operation ID, terminal frames and durable
   `guest.recipe.result`. No settings JSON is required from the TUI user.
   A CLI run uses the same `guest.tools.install` service; jobs are stored as
   `guest.recipe.run`, not a separately invented operation.
4. Require the package to transition from absent to installed, record its exact
   distribution package version, and require `qemu-guest-agent.service` active.
   Then independently call `virmill vm readiness show UUID`: require a successful
   fixed `guest-ping` through the official libvirt adapter and the same live VM
   identity. The recipe's systemd verification alone does not prove this.
5. Run the same reviewed built-in again once. Verify its already-configured check
   skips package mutation and preserves service state. Exercise a mismatched
   known-hosts file before a separate attempt and prove no guest mutation occurs.
   Record interruption/no-replay evidence separately if induced; do not assume
   terminating SSH stops a guest package manager. Inspect its actual state and
   durable recovery receipt before any retry.
6. Stop only this fixture VM. Preserve evidence and its new disks/seed privately;
   either retain its marked network for review or tear it down through a supported
   exact-identity flow after proving no other guest uses it. Recheck all baseline
   guest XML/state, source metadata/hashes, existing networks and journal rows.

The recipe currently permits up to 300 seconds per stage. Use one bounded boot
attempt and one installation attempt; investigate the recorded failure instead
of repeatedly restarting or extending timeouts. Repository/package downloads
occur only inside the fixture guest during the explicitly reviewed tools job.
They are not permission to fetch another OS image or change host repositories.

## Claims and remaining matrix

| Observation | Evidence it supplies | Does not establish |
| --- | --- | --- |
| Exact Fedora version, missing→installed package, active service, native agent ping | That recorded Fedora guest-tools configuration, partial GUEST-03 | Debian/Ubuntu/Kali support, desktop integration, full GUEST-03 |
| Real TUI setup/review/job/result at 80×24 | UX-01 workflow evidence | All mandatory TUI workflows |
| Fixture-specific cloud-init seed | Test bed bootstrap | Product NoCloud profile or GUEST-01 acceptance |
| Product NoCloud Netplan/networkd flow on an actually compatible image | Partial GUEST-01 | Other network renderers, distributions, multi-NIC routing/isolation |

Kali support would require a deliberate explicit profile or documented addition
to auto-detection, a new built-in recipe version/digest, exact ID guard, catalog
and TUI updates, failure/idempotency tests, and real Kali 2026.2 package/service/
agent evidence. Its Debian ancestry is not sufficient. That extension can use
the supplied Kali media after this decision; it must not silently broaden the
meaning of the existing `debian` profile.

## Observed fixture preparation — 12 September 2026

The stopped Fedora source contains 622,919,680 bytes with SHA-256
`3a6b44a4db1299ec7bef95e2921b83fdaa17dc5b74449b588e66fc81d63fae59`.
An ordinary-user independent copy was made and verified at
`~/virmill-tests/guest-tools-fedora-copy-001/fedora-copy.qcow2`; the original
source and existing guests were preserved. Product confined metadata inspection
recognized one standalone QCOW2 disk. The initial fixture call used the wrong
virt-inspector option spelling and failed before inspection; the installed tool
requires `--format=qcow2`. Both the failure (`guest-tools-fedora-copy-001`) and
successful corrected inspection (`guest-tools-fedora-inspect-001`) are retained.

Offline inspection identified Fedora Linux 44 Cloud Edition, x86-64, Btrfs root,
with NetworkManager 1.56.0-1.fc44, cloud-init 25.3-3.fc44, systemd 259.5-1.fc44,
OpenSSH server 10.2p1-7.fc44, sudo 1.9.17-7.p2.fc44 and QEMU guest agent
10.2.2-1.fc44 already installed. Host virt-inspector/virt-customize report 1.56.0;
guestfish/libguestfs report 1.60.1, QEMU image tools 10.2.2. These are observed
versions, not inferred from dependency examples. The installed customizer's
`--selinux-relabel` is a compatibility no-op, so the fixture must explicitly
handle copied guest labeling during boot.

The existing `default` NAT network is active with UUID
`e4aa7897-db51-45de-a0dc-a13554eba163`, bridge `virbr0` and IPv4
`192.168.122.1/24`. The bounded follow-up may attach only the new fixture to that
network, leaving its definition/state unchanged. A fresh MAC-specific DHCP lease
and an independently injected host key bind the fixture SSH target. This avoids
creating or changing host networking merely to test guest package installation;
it does not claim network isolation qualification. Actual installation still
requires removing the existing agent only from a new fixture copy, installing
through Virmill and observing the native agent response.
