# Creating with a NoCloud seed

A successful [existing-disk preparation](existing-disk-preparation.md) can be used
as a declared cloud-image source. Add `provisioning` to the shared `vm create`
input to generate and attach a NoCloud seed. This development path verifies local
seed contents and carries them through durable creation and recovery. It has no
qualified guest image and does not report cloud-init completion or connectivity.

The current declared profile is `nocloud-netplan-ipv4-v1`: a Linux cloud image with
cloud-init's NoCloud datasource, Netplan and systemd-networkd already installed.
`cloudInitCompatible: true` is your explicit assertion, not a detected or certified
capability. Other renderers, enabled IPv6, Windows provisioning, passwords, private
keys, package/setup recipes and guest readiness monitoring remain unsupported in
this profile and required future work where mandated by the full specification.
No software is installed on the host or silently added to the image to satisfy it.

Prepare the image with its actual format and complete source/backing selection.
Keep the successful operation ID. Copy [the NoCloud example](../examples/creation/nocloud.json)
and replace its placeholder pool UUID, machine/hardware choices, public key and
source digest. `sourceDiskID` identifies the prepared boot disk and must be boot
order 1. `sourceSHA256` must equal that root source file's recorded digest, rather
than the converted disk digest or aggregate source-set digest. `sourceReference`
is a declared HTTPS provenance URL without credentials/query/fragment. It is
recorded, not downloaded, signature-verified or treated as publisher authentication.
The example source URL and key are placeholders, not usable publication destinations
or credentials.

Map the generated medium in `hardware.media` with the exact `mediaID`, SATA/SCSI
bus and `bootOrder: 0`. Its ID must differ from every prepared disk/media ID. The
seed is data, not an installer boot candidate. It receives a separate managed
read-only ISO volume and is verified with all guest disks before definition.

```sh
virmill vm create PREPARED_DISK_OPERATION_ID \
  --connection qemu:///session \
  --input "$(cat /absolute/path/to/reviewed-nocloud-creation.json)" \
  --plan --output json --non-interactive
```

Review the generated VM UUID/MACs, `provisioning`, exact seed/member hashes,
`seedStagingDirectory`, target volumes and all acknowledgements. Every clone gets
its own `virmill-VM_UUID` instance ID and MAC-matched network configuration. The
seed is reproducible for that particular plan; it is not shared across clones.
The preview creates and removes bounded private scratch files, with no managed
volume or VM mutation. Apply adds `guest-root-provisioning` and
`rotate-guest-host-keys` to creation's normal acknowledgements. Setting
`passwordlessSudo: true` also requires `guest-passwordless-sudo` and gives the
selected guest account root authority. It defaults to no such request only when
explicitly set false in the input.

The TUI uses **VMs → vm create** with
`{"id":"PREPARED_DISK_OPERATION_ID","input":{...the same creation JSON...}}`.
Its review/approval, Operations views and `vm creation result/resume` use the same
application services as the CLI. Guided provisioning forms remain unfinished.

The profile requires a hostname, non-root username and 1–32 canonical public SSH
keys. Ed25519, RSA of at least 2048 bits and NIST ECDSA keys are structurally checked.
Do not include authorized_keys options or comments. Plaintext password fields,
private keys and arbitrary YAML/shell user data are refused. Creation schema errors
withhold input values because the underlying validator can echo rejected secrets.
Accepted public keys, hostname and network configuration are intentionally visible
in the private plan and remain in the managed seed and potentially inside the guest.
This is not a secret-store or encrypted-seed implementation.

The generated user data requests public-key access for the selected account,
password authentication disabled, password locking, cloud-init's root-login
restriction and fresh SSH host keys. It explicitly disables cloud-init root
partition/filesystem growth, package updates/upgrades and automatic package reboot.
No existing credentials, users, machine ID, guest scripts or application identity
are claimed erased or generalized. Separate reviewed guest workflows must handle
those changes and verify their effects.

Every created NIC needs exactly one `interfaces` entry keyed by its `nicID`.
The seed uses generated MAC matches, without guessing guest interface names.
The initial profile explicitly requires `ipv6: disabled`, renders DHCPv6 and RA
disabled and no link-local addressing, and supports these IPv4 modes:

- `dhcp`: no static address/gateway; `defaultRoute` controls whether DHCP routes
  are accepted. Only the default NIC may accept DHCP DNS, and only if explicit DNS
  was not supplied. DHCP hostname/domain changes are disabled.
- `static`: canonical usable IPv4 address/prefix, with a gateway only when it is
  the normal default NIC. Gateway must be another usable address in that subnet.
- `none`: no address, gateway, DNS or default route.

Every entry includes `role` (`normal` or `protected`), `routeMetric` (1–65535),
`address`, `gateway`, `dns` and `defaultRoute`. Use empty strings/arrays where absent.
At most one normal default route is allowed. Protected NICs cannot set a default
route or normal guest DNS; protected DHCP suppresses both. A disconnected NIC
cannot be the normal default. Overlapping static subnets fail closed in this
profile. These are declared guest policies, not verified host isolation. Guest
forwarding is not modified or verified, so dual-homed guests still require explicit
routing/isolation qualification.

The example has no NICs and therefore an empty interfaces list. For a DHCP internet
NIC and a static lab NIC, add both hardware NICs with `sourceIndex: -1` and use:

```json
[
  {"nicID":"internet","role":"normal","mode":"dhcp","address":"","gateway":"","dns":[],"defaultRoute":true,"routeMetric":100},
  {"nicID":"lab","role":"protected","mode":"static","address":"10.23.4.10/24","gateway":"","dns":[],"defaultRoute":false,"routeMetric":200}
]
```

This does not create the selected networks or prove their address allocation is
safe on a real host. Review target networks and the complete topology separately.
NoCloud expects `user-data`, `meta-data` and optional `network-config` at the root
of labeled media; Virmill requires explicit network configuration for every seed.
See the [NoCloud contract](https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html)
and [network v2 boundaries](https://docs.cloud-init.io/en/latest/reference/network-config-format-v2.html).

Seed construction uses fixed root-owned xorriso inside unprivileged bubblewrap,
without host devices, sockets, credentials or original image mounts. ISO timestamps
are fixed. The service checks the CIDATA label and exact three-member readback,
then pins the resulting bytes. Execution must reproduce that proof before any
managed allocation. Private cache space and identity are checked; generation is
bounded by worker time/memory/file-size limits. Closing a client does not stop the
accepted coordinator job.

Cancellation during seed generation joins the worker, removes its private output
and can finish canceled before allocation. Generator failure produces an explicit
uncertain operation without allocating managed volumes. Later volume failure keeps
the identified partial set and uses existing creation cleanup/recovery. An already
verified complete set can resume definition without the generator or temporary
cache. Reconciliation never regenerates a seed or reuploads its volume. Temporary
execution seed files are removed after the worker returns; a daemon crash may leave
the reported private stage for future explicit cache disposition. The managed
read-only seed is retained; safe post-provision seed detachment/removal is still a
required lifecycle workflow.

Actual tests use generated files and a synthetic storage backend, with separate
native test-driver XML evidence. Cloud-init is not installed in this build host,
and no real cloud image was supplied or booted. `guestBootVerified`,
`setupVerified` and `connectivityVerified` remain false. Do not treat successful
seed or domain creation as a successfully provisioned guest.
