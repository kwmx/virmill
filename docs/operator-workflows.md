# Operator workflows and current boundaries

## Inspect existing storage pools and networks

Use `storage pool list` and `network list` with an explicit `--connection
qemu:///system` or `--connection qemu:///session`. `storage pool show UUID` and
`network show UUID` return the selected resource's configuration and stable key.
The TUI exposes the same commands in **Storage** and **Networks**. Names remain
labels; system and session resources keep separate connection identities.

These are read-only native libvirt calls. They do not adopt, activate, refresh,
create or delete resources. Pools show the backend type, configuration XML,
active/persistent/autostart state and observed capacity when active. Capacity fields
are null when unavailable, rather than fabricated zero space. Pool fingerprints
bind the returned XML and activation settings; any XML change invalidates that
fingerprint, including dynamic fields a backend includes in its XML.

Network observations keep live and persistent XML separate and explicitly report
`isolationVerification: not-run`. A NAT/isolated declaration is not packet evidence.
If a backend cannot read persistent configuration, the command reports its error
instead of copying live XML into that field. Large inventories fail at the protocol
response bound; bounded pagination remains required. Read-only commands have no
durable side effects to recover; detach or retry a failed observation as needed.

The installed libvirt test driver exercises pool reads but rejects the inactive
network XML flag. Tests preserve that error and use separately labeled synthetic
records for network state separation. Actual system/session resource inventory and
the complete storage/network mutation workflows remain unqualified.

## OVA inspection and configuration preservation

`import inspect FILE.ova` reads a bounded, uncompressed tar appliance without
extracting, registering or booting it. It checks names, entry types, byte/member
budgets, descriptor structure, file references and provided SHA checksums. Reports
list every virtual system and disk, controller parent/position and NIC connection
hints. Multiple systems require explicit selection in the eventual import workflow.
A valid checksum is not publisher authenticity or guest readiness. The separate
[`import prepare` workflow](import-preparation.md) performs confined source-format,
backing-chain and extent inspection, independent qcow2 conversion and content
comparison for every explicitly selected disk. It preserves the descriptor and
publishes the complete set under a durable receipt. Explicit NIC/controller mapping
and registration use the separate [creation adapter](vm-creation.md).
Guest adaptation and boot remain unqualified. Preparation
produces disk artifacts, not a defined VM.

Powered-off vCPU previews patch the original XML byte spans and retain unknown
namespaces, comments, attributes and device definitions. Ambiguous or structured
fields are refused. A changed domain fingerprint invalidates apply. Full live/
next-boot editing, advanced XML authorization and complete hardware controls remain
required. Current state tests cover the patch algorithm; only a real adopted VM
can satisfy the complete preservation acceptance scenario.

## Multiple networks and labs

`lab validate` resolves the declarative DAG and in-document network references.
It rejects duplicate identities, cycles, overlapping lab CIDRs, inappropriate DHCP,
multiple normal IPv4 defaults and inconsistent static addressing. It warns about a
VM joining both an externally reachable and a protected segment. The cloud-init
renderer and actual guest route verification are not implemented. Neither validation
nor a disabled DHCP declaration enforces a firewall or proves isolation.

Mandatory network creation, IPv6 policy, forwarding, existing bridges, NetworkManager
and Netplan rollback, lab apply/teardown and packet diagnostics remain incomplete.
No code in the shipped development command registry applies host network changes.
When a future bridge plan is available, review exact interfaces and use a disposable
host with an independent console; do not use the developer host for rollback tests.

## USB and guest setup

The selector library matches vendor/product and a unique serial or approved port,
resolves current bus numbers, refuses ambiguous devices and refuses known mounted
or critical host-use devices. Unknown host use fails closed. These are synthetic
selector tests; actual udev ownership, persistent binding/start hooks, USB attach/
detach/replug and both live/persistent recovery are not implemented or certified.

Cloud-init seed generation, SSH/guest-agent transports, approved recipe execution,
sharing/viewer integrations and first-boot verification remain required work. No
unknown appliance is automatically modified, and no shell-profile recipe is fetched.
The audited optional recipe archive was not supplied.

## Snapshots, backups and recovery

A snapshot depends on a managed disk graph; it is not an independent backup.
The graph library refuses deleting referenced or uncertain bases. The manifest
checker verifies its declared member inventory and SHA-256 hashes and refuses
missing declared NVRAM/TPM members, external-secret claims of independent recovery,
and unproven live auxiliary-state completeness. It does not capture a VM or establish
that the manifest includes every actual backend dependency.

`backup verify-manifest FILE --input '{"root":"/approved/artifact/directory"}'`
checks the supplied member files. `manifest-checked` is the only implemented
verification level; no repository or boot verification is implied. Actual capture,
restic repositories, independent guest restore, retention and scheduling are not
implemented. Never remove original disks or the application DB based on this check.
The mandatory independent-recovery tutorial must be completed and exercised against
the real implementation before release; no speculative restic/TPM restore commands
are provided as if they were validated procedures.

## Service, credentials and cleanup

The coordinator socket is `$XDG_RUNTIME_DIR/virmill/control.sock`, mode 0600 in a
0700 directory. The journal lives in `$XDG_STATE_HOME/virmill/journal.db`, with WAL,
FULL synchronization and a singleton lock. Database versions newer than the
application are refused. Schemas 1 and 2 upgrade to schema 3 after a private, flushed
consistent backup; the new version guards inherited locks, retained-volume pins
and closed creation recipes. See [creation cleanup](creation-cleanup.md).
Generated fixture tests cover migration, retained locks and rollback. Production
package upgrade qualification and scheduled credential-store access remain incomplete.

Standard XDG variables choose directories. `VIRMILL_SDK_DIRECTORY` optionally
selects reviewed SDK source for scaffolding (default `/usr/share/virmill/sdk/go`).
Plugin signing uses an explicitly supplied private-key file reference; key bytes
are not journaled or included in distribution payloads. `VIRMILL_TEST_CONFORMANCE` and
`VIRMILL_TEST_REQUIRE_IPC` and `VIRMILL_TEST_DISK_TOOLS` are test-only switches and
never select a runtime fake.
Backup repository credentials are not requested by current implemented workflows.

The development installer/uninstaller acts only on listed package files. It does
not remove VMs, disks, backup repositories, bridges, user data or enable persistence.
Use a native package manager for later reviewed upgrades. Host cleanup must be a
separate resource/dependency plan; that workflow is not yet implemented.
