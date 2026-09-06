# 15 — Packaging, installation and operations

## Distribution

Provide signed/checksummed Linux x86-64 RPM and DEB artifacts plus source and reproducible build instructions. Freeze exact supported Fedora and Ubuntu/Debian release/package combinations during implementation and publish the tested matrix. “Linux” is not evidence that every distribution, firewall stack and native dependency combination works.

The Go/libvirt binding has a native boundary. Declare runtime libvirt/QEMU dependencies and test the chosen binding strategy; do not advertise a static all-in-one binary. Separate required dependencies from optional feature dependencies: cloud-init seed generation, virt-v2v/libguestfs conversion, restic backup, SPICE/VNC viewer, TPM emulation and Linux confinement. A missing dependency disables only its relevant flow with an installation plan, not a generic unexplained error. [S01] [S02]

Do not auto-download operating systems, drivers or plugins at first launch. An approved source download records URL, redirect policy, digest and provenance. Packages/updates use configured trusted repositories or verified release artifacts. An optional script-catalog entry may install the standalone application; no catalog becomes the application codebase or a runtime dependency. All product packages and installation messages use the Virmill identity.

## Product identity and namespaces

These names are the v1 product contract, not tentative alternatives. Apply them consistently to source entrypoints, generated help, examples, packaging, tests and plugin tooling.

| Surface | Required identifier |
|---|---|
| Product display name | `Virmill` |
| Public executable / main package | `virmill` |
| Explicit TUI entrypoint | `virmill tui`; bare `virmill` opens the TUI when attached to a terminal |
| Private local coordinator | `virmilld`; `virmilld.service` as the Linux user service |
| Coordinator implementation alternative | The service may execute `virmill daemon` internally; that implementation choice does not rename the service |
| Bounded privileged helper | `virmill-host-helper`; system units `virmill-host-helper.service` and `virmill-host-helper.socket` when socket activation is used |
| Application-specific environment-variable prefix | `VIRMILL_`; document supported variables explicitly and never place secrets in examples |
| Declarative `apiVersion` | `virmill/v1` |
| Specification directory / archive root | `virmill-v1-spec/` |
| User files and private socket | The `virmill` paths in the following table; socket `$XDG_RUNTIME_DIR/virmill/control.sock` |
| Administrator policy directory | `/etc/virmill` |
| Schema identifier base in this package | `https://virmill.example/schemas/v1/` |
| Reference plugin ID | `example.virmill.vm-summary` |
| Reference plugin Go module | `example.invalid/virmill/vm-summary` |

The schema URLs are offline logical identifiers under an example domain. Bundle the schemas and resolve their IDs locally: validation MUST NOT depend on fetching these URLs. The reference plugin's example ID/module are illustrative only. No website, repository owner, signing identity, app-store ID or registry account is assigned by this specification. Do not turn example domains into download endpoints, telemetry destinations or ownership claims. If publication later needs real coordinates, select them explicitly and preserve established data-contract compatibility.

Display names and identifiers in optional third-party guest recipes remain the responsibility of their authors; they are not Virmill branding or core dependencies. Do not rewrite external software attribution or claim ownership of its source. This revision is pre-release specification work, not a migration of an installed product; no compatibility alias for the superseded draft command is required.

## Filesystem and service contract

| Location | Purpose |
|---|---|
| `$XDG_CONFIG_HOME/virmill` (default `~/.config/virmill`) | User settings, connection references and non-secret policy |
| `$XDG_STATE_HOME/virmill` (default `~/.local/state/virmill`) | SQLite journal, operation metadata and private diagnostic logs |
| `$XDG_DATA_HOME/virmill` (default `~/.local/share/virmill`) | User-owned plugin packages, lab/template metadata and artifact catalog |
| `$XDG_CACHE_HOME/virmill` (default `~/.cache/virmill`) | Disposable downloads/inspection cache with quotas |
| `$XDG_RUNTIME_DIR/virmill` | Private local coordinator socket and ephemeral handles; directory 0700/socket 0600 |
| Approved system storage pool | System-libvirt-accessible VM disks/staging; never inferred from user plugin directories |
| `/etc/virmill` | Administrator-installed helper policy, trust anchors and integration configuration |

The helper's root-owned journal/checkpoint area is separate from user-writable state, with strict ownership and no following user-controlled symlinks. System domain disk placement must respect libvirt permissions, SELinux/AppArmor and storage labeling. Never solve access by globally disabling a security framework or granting world write access.

`virmilld` runs as the user. The privileged helper is narrowly socket-activated or invoked through an authenticated service mechanism. Native authorization checks occur for each sensitive operation; authorizing a daemon once is not blanket approval for every future request. [S30]

## Setup and persistence

First-run doctor inspects virtualization support, backend access, free storage, network manager, firewall, viewer, conversion tools and guest setup prerequisites. It proposes changes with reasons and authorization requirements. Package installation does not silently create a bridge, change boot options, disable security, add broad sudoers rules or enable unattended persistence.

Offer explicit `service persistence enable` behavior for unattended scheduled work. Explain lingering, boot behavior, credential availability and storage mount readiness. A keyring locked at boot can prevent a backup; report `blocked-credentials` rather than dropping encryption or storing plaintext secrets. A mounted removable backup repository must be positively identified, not replaced with an empty directory on the root filesystem. [S08]

## Plugin package format 1

A release plugin is an archive containing `manifest.json`, executable/resource files, `package-index.json`, and `package-index.sig`. Reject links, device entries, absolute/traversing paths and duplicate normalized paths. Entries use a restricted portable relative-path alphabet, case-collision checks and declared size limits. Every payload file except the index/signature must be enumerated exactly once; there are no unsigned extra files.

`package-index.json` contains `formatVersion: "1"`, plugin ID, plugin version, signing-key ID and an array `files` of `{path, size, sha256, executable}` sorted by normalized path. SHA-256 covers raw file bytes; executable is a boolean applied by the installer, not arbitrary archive permission bits. The signature is Ed25519 over RFC 8785 canonical JSON bytes of that index. `package-index.sig` is the 64-byte raw signature. The index binds the manifest as an ordinary hashed file. Record the SHA-256 of the canonical index as the package identity bound into approvals. [S27]

Trust is explicit: an owner-approved key fingerprint, administrator-configured key or declared development override. A valid signature proves the package matches that key, not that its code is safe. Require trust review for a new key; package updates cannot add privileges silently. Verify inside staging before installation, then atomically select a version. Keep prior version for rollback; revoked grants remain revoked.

The included sample manifest is source/development metadata and has no fabricated binary digest or signature. It is not an installable trusted distribution package. The agent must implement `plugin pack`, signing/verification and SDK conformance as v1 features.

## Updates, migration and uninstall

Before database migration, quiesce new mutating jobs, create a consistent backup, check free space and record schema version. Use tested migrations and restore recovery; an old application may not open a newer schema. Do not promise automatic binary downgrade is safe after irreversible migration.

Upgrade coordinator/helper/protocol components compatibly. In-progress operations either finish under the existing compatible worker or enter documented recovery; replacing a binary does not make arbitrary conversion checkpoints reusable. Never allow old and new coordinators to write the same state store concurrently. SQLite WAL is an implementation choice with documented concurrency/backup requirements, not a multi-host database. [S31]

Uninstall defaults to removing application executables/services, not VMs, disks, backups, bridges or user data. Offer a separate reviewed cleanup plan listing every resource and dependency. Restic repositories remain independently recoverable. Removing a plugin does not delete resources it created without a separate resource-specific plan.

## Documentation required with the actual product

Ship a getting-started guide, distro setup guide, TUI navigation reference, generated CLI reference, OVA troubleshooting, multi-network lab tutorial, USB workflow, snapshot versus backup explanation, independent restore walkthrough, network rollback recovery, headless schedule/credentials guide and the plugin author tutorial/protocol/SDK reference. Every tutorial has a tested fixture and expected outcome.

Provide `diagnostics collect` with explicit scope, bounded log size and redaction preview. No telemetry by default. Report versions/capabilities without collecting unrelated home directories, browser data, SSH keys, guest secrets or host traffic. Operators can inspect the diagnostic archive before sharing it.
