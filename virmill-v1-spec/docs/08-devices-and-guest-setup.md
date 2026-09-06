# 08 — Devices, sharing and guest provisioning

## Host-local USB is mandatory

Discover devices using reliable host APIs and sysfs/udev data, not parsed display tables. Show vendor/product IDs, serial where available, physical topology path, device class, current host use, VM ownership and whether safe persistent identification is possible.

A USB selector prioritizes vendor/product plus a unique serial, then an explicitly selected physical port. Bus/device numbers may change after replug and are only suitable for a current-session attachment. Some devices share missing or nonunique serials: display the ambiguity and require a port-specific choice instead of attaching a random matching device.

Before assignment, check whether the device is mounted, supplies the active network connection, is the only usable input device, or is already used by another VM. Do not detach host storage with mounted filesystems or force-unmount it automatically. Explain that exclusive passthrough transfers use from host to guest.

Attach/detach has separate live and next-boot intent. A persistent binding resolves to the current host device at VM start; it cannot assume a stale bus address. Missing-device policy is `optional` or `required`. Optional devices permit boot without the device and publish a warning; required devices block start with a concrete message. Replug auto-reattachment occurs only for a unique preapproved binding and an eligible running VM.

Device re-enumeration, such as a phone changing mode, must not trigger automatic matching by vendor alone. A detach failure or plugin crash must leave an explicit ownership record and a reconciled actual state.

Libvirt's domain model distinguishes host devices and redirected USB. v1's mandatory path is host-local attachment; forwarding a USB device from another computer is a different future extension. [S25]

## PCI/IOMMU diagnostics

Include read-only PCI inventory, drivers, IOMMU group membership and an explanation of common assignment constraints. VFIO's security model depends on IOMMU grouping; do not offer a safety-bypassing ACS override as a routine fix. [S26]

Automatic GPU passthrough, initramfs/bootloader edits, host-driver rebinding and specialized mediated-device management are outside the mandatory v1 workflow. These can be separately designed plugins with explicit privilege and hardware tests. The normal hardware editor may preserve existing PCI configuration without claiming the application safely provisions it end-to-end.

## Folder sharing and desktop integration

Provide managed shared-folder definitions using supported virtiofs or other explicitly tested mechanisms, with read-only as the safe default. Only share user-approved directories; never expose the whole home directory or root filesystem by convenience. Show symlink/mount traversal behavior and guest support requirements.

Graphical viewer integration uses supported SPICE/VNC connections with local-only/private transport. No unauthenticated display listener on all interfaces. Clipboard, audio, USB redirection and display resizing are opt-in features with their guest-agent/viewer dependencies visible. External viewer failure does not stop the VM or corrupt the TUI.

Serial console is available where configured; do not promise a guest shell just because a serial device exists. SSH needs a known address, correct credentials and verified host key. Do not use `StrictHostKeyChecking=no` as a normal default.

## Provisioning tiers

| Tier | Mechanism | Scope |
|---|---|---|
| First boot | cloud-init/NoCloud on supported Linux images | User, SSH keys, hostname, disks, network intent, packages and approved setup |
| Existing reachable guest | explicit SSH or approved guest-agent operations | Recipes, software setup, status checks and remediation |
| Windows | documented guest tools, supported unattended profiles or explicitly installed cloud provisioning agent | Limited to profiles actually implemented/tested; do not assume Linux cloud-init syntax works |
| Unknown appliance | console and vendor-specific documented instructions | Honest manual steps; no arbitrary guest modification disguised as automatic setup |

Automatic setup is a complete feature for supported profiles, not a claim to configure every opaque appliance. A guest-agent channel is a high-trust host/guest interface and is enabled visibly. It does not inherently provide every provisioning capability or prove the identity of a network service.

## Recipe contract

A recipe has a versioned ID, supported guest/profile predicates, requested transport, required privilege inside the guest, inputs, secret references, executable content digest, check/apply/verify stages, timeout, reboot policy and cleanup behavior. Recipes declare whether rerunning is idempotent; unknown idempotency requires confirmation.

Prefer structured steps and explicit argument arrays. When shell code is necessary, it runs inside the selected guest, not on the host, under an explicit approved recipe. Never interpret values from guest output as host commands. Host plan previews show the guest identity and privilege scope.

Recipes run only after the declared readiness gate. Record output with redaction, exit status and verification results. Package installation success is not proof a service started; verify the requested end condition. Reboot is never hidden inside a generic setup step.

## Original shell-profile bundle

The inspected archive contains a POSIX-sh installer/uninstaller, shared helpers and a script-catalog manifest for Bash/Zsh profile modules. It does not implement VM management. Retain it as a version-pinned optional **guest** recipe, executed as the chosen non-root guest user. Keep its backup/uninstall behavior and do not silently run it on the host.

Pin recipe content to the archive digest recorded in the input audit. Installation is opt-in; updated recipe content requires a new version/digest. Do not fetch-and-execute the latest script inside every VM as an invisible default.

## Secret handling

Declarative files and operation logs use secret references, not plaintext passwords/private keys. Cloud-init seed content can contain sensitive values: generate it with restricted permissions, avoid long-lived password authentication by default, disclose persistence inside the guest, and remove host-side seed artifacts when safe and configured.

Do not place private SSH keys in VM exports or templates. Generate distinct cloud-init instance IDs for clones, rotate guest host identity only through supported preparation, and warn when a template includes credentials. A secret-store reference may work interactively but be unavailable to unattended schedules; the setup wizard must test that distinction.

## Acceptance

Test USB unplug/replug, two identical devices without serials, mounted storage refusal, phone mode changes, required versus optional devices, VM restart, backend restart, and failure halfway through a dual live/persistent attachment. Test Linux first-boot setup, existing-guest recipe execution, secret redaction, failed verification, authorized reboot, and shell-profile uninstall without destroying user customizations.
