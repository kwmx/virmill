# ADR 0059: Guided cloud-image setup in VM setup

Status: accepted. Native evidence: `cloud-image-native-005` (Ubuntu 24.04 cloud
image).

Cloud images (Ubuntu cloud images, Fedora Cloud Base, Debian genericcloud) have
no password: they expect cloud-init to create a user with an SSH key. The service
already builds reviewed, identity-bound NoCloud seeds (ADR 0010), but only
through hand-written JSON, so a TUI user could create a VM from a cloud image
and never log in. This decision adds the guided form. It keeps the NoCloud
profile, its validation, its acknowledgements and its recovery unchanged.

For disk images, VM setup offers **Cloud image: No / Yes, create my user with
cloud-init**. It is preselected, with a label, when the VM name contains
"cloud", as common cloud image file names do. A user's own choice is never
overridden. When on, the form asks for:

- **Cloud user name**, suggested from the login name, never root;
- **SSH public key file**, suggested from `~/.ssh/id_ed25519.pub`,
  `id_ecdsa.pub` or `id_rsa.pub`. Only public keys are read, options and
  comments are dropped, and a file containing a private key is refused before
  any of it is used;
- **Downloaded from**, the https address the profile requires as declared
  provenance. It is recorded, never downloaded;
- **Administrator access**, passwordless sudo by default, because cloud users
  have no password. It adds `guest-passwordless-sudo` to the review.

The hostname is derived from the VM name. The seed is one more read-only
medium on SATA (SCSI when SATA is full) that never boots. Each adapter's IPv4
intent follows its cable: the first connected adapter uses DHCP with the default
route, other connected adapters use protected DHCP, and disconnected adapters
get no address. IPv6 stays disabled, as the profile requires.

The profile's source digest must equal the recorded SHA-256 of the prepared
boot disk's original file. The TUI reads it from the preparation result. Before
preparation it does not exist yet, so under the one approval of ADR 0057 the
approved settings are compared without that single field. The chain already
binds creation to its own preparation, and the service checks the digest against
that preparation's receipt. Every other field, including user, keys, source
address and network intent, must match what was approved. The combined review
lists the user and the added acknowledgements: `guest-root-provisioning`,
`rotate-guest-host-keys` and, when chosen, `guest-passwordless-sudo`.

The service still does not report cloud-init completion, guest login or
connectivity (GUEST-01, IMP-01). A native walk-through checked them once from
outside: Ubuntu 24.04's cloud image took a DHCP address on the default NAT
network, accepted the reviewed key over SSH, allowed passwordless sudo and
reported cloud-init done ([run record](../evidence/one-approval-import-run.md#cloud-image)).
