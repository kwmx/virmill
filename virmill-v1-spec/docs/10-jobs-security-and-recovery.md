# 10 — Jobs, privileges, threat model and recovery

## Threat and trust boundaries

Treat guest images, OVF/XML/YAML input, template metadata, plugin output and guest-reported data as untrusted. The local operator and installed built-in backend are high-trust components. Broad libvirt management access can be a powerful host-administration capability: do not present membership in an administrative libvirt group as harmless or least-privilege isolation.

The product protects against accidental destructive operations and constrains untrusted parsing/extensions. It does not claim to defeat a malicious host administrator, a compromised kernel/hypervisor, or an intentionally unrestricted native plugin running as the operator.

## Process privileges

The CLI/TUI and coordinator run as the ordinary user. Built-in libvirt operations use the operator's authorized local libvirt connection. Prefer existing OS/libvirt authorization rather than adding blanket passwordless rules. Any persistent authorization required for unattended work is separately approved and documented.

`virmill-host-helper` is a small, socket-activated privileged mechanism. It authenticates the actual Unix peer, authorizes each operation through polkit or a narrowly scoped persisted policy, and validates typed parameters itself. Never trust a supplied UID/PID as the actor; use kernel peer credentials and race-resistant process identity where required by the authorization API. Polkit is an authorization mechanism, not a reason to trust arbitrary requested operations. [S30]

Allowed helper families:

- Checkpoint/apply/verify/rollback a supported host network plan.
- Create or move a managed storage path under a registered approved root, with safe permissions/labels.
- Capture/restore approved firmware/TPM auxiliary state for a stopped/otherwise safely captured VM.
- Maintain independent bounded recovery watchdogs, such as guest thaw and network rollback.

No generic `runCommand`, arbitrary filesystem write, arbitrary unit definition, user-supplied root script, or unrestricted file-copy endpoint. Paths are resolved beneath approved roots using race-resistant directory/file descriptors; reject symlink escapes and TOCTOU replacements. Revalidate VM stopped state where required.

Helper grants bind actor, operation, resource IDs, canonical plan digest, allowed parameters, expiry and job identity. Long-running accepted jobs may finish within their recorded grant bounds; new actions after expiry require reauthorization. A scheduled policy grants only its declared repeated operation, not every future host change.

## Local IPC

Private user socket: `$XDG_RUNTIME_DIR/virmill/control.sock`, mode 0600 in a 0700 directory. Verify peer UID on both sides when supported. Backend/helper sockets are never mounted into an untrusted plugin sandbox. No automatic HTTP server, LAN listener or unauthenticated debug endpoint.

Authenticated local clients share a coordinator; a second daemon for the same user/connection is rejected using a singleton lock. Reserve backend resources with stable IDs, not user-visible names. Cross-tool or cross-user modifications remain external-writer races: recheck before critical steps and reconcile uncertain results.

## Job states

```text
queued -> validating -> awaiting-approval -> running -> verifying -> succeeded
                                      \                 \-> failed
                                       \-> canceled      \-> partial
running -> cancel-requested -> canceling -> canceled
running/verifying -> interrupted -> reconciling -> succeeded|failed|recovery-required
```

An operation can have warnings without being partial. `partial` means actual committed effects remain but the requested result was not fully achieved. `recovery-required` means safe continuation cannot be inferred automatically.

Each step persists intent, preconditions, an idempotency classification and expected effect before execution; then persists observed results and artifact identities. External side effects can happen before an acknowledgement is recorded. Recovery must inspect reality rather than simply replay the last step.

## Locking and cancellation

Use ordered locks for domain, volume/backing graph, network, physical USB selector and backup repository. Detect conflicting backend jobs too. Impose per-pool I/O concurrency limits so a group of conversions does not exhaust disk space or stall every running VM.

Classify steps as safely cancelable, cancel-at-boundary, or irreversible-after-commit. Killing the interface never kills a VM. `operation cancel` requests cancellation; it does not promise immediate termination. Interrupting a block merge or final publish requires a safe backend-specific boundary and reconciliation.

Retries require unchanged immutable inputs and a known step state. Reuse an idempotency key only for the same intended operation. A forced new attempt after a partial apply produces a new reviewed plan. Export/import staging may be cleaned only after all backend references are ruled out.

## Recovery table

| Interrupted action | Recovery requirement |
|---|---|
| Download/extract | Verify bounded partial source; resume only under unchanged validator/digest or restart staging |
| Disk conversion | Keep original; validate complete staged artifacts; restart incomplete output rather than claiming resumable conversion |
| VM define | Query by intended UUID; compare config/artifacts; adopt only an exact matching result |
| Live/persistent edit | Read both views; expose divergence and offer a reconciliation plan |
| Snapshot/merge | Query block jobs and backing graph; never delete files based on missing local state |
| Backup capture | Thaw independently; determine actual capture completion; reject incomplete manifests |
| Backup repository write | Query repository result/object inventory; do not count a partial snapshot as complete |
| Restore publish | Check target artifacts and definition; leave old VM intact until approved verified swap |
| USB assign | Inspect actual backend/host ownership; resolve unique selector again |
| Host bridge change | Independent watchdog restores checkpoint; verify actual addresses/routes/config files |
| Plugin side effect | Ask its reconciliation method when supported; otherwise report uncertain state and block blind replay |

A host reboot terminates in-progress processes; the journal enables recovery, not uninterrupted execution across power loss.

## Image/converter confinement

Run image inspection/conversion in a confined worker with explicit read-only source and writable staging paths, no home/config/runtime sockets, no network by default, bounded CPU/memory/process count and sanitized environment. Use kernel/user namespaces plus an audited Linux runner such as bubblewrap with an explicit mount/seccomp policy. Bubblewrap is a low-level building block; the application must supply the actual policy. [S28]

Guest conversion tools needing a helper VM may require a separately approved `/dev/kvm` capability and managed temporary resources. Never give a generic converter `/dev` or host block-device access. Lack of safe worker confinement is a capability failure for untrusted imports, not permission to run them as root.

## Plugin confinement

Normal third-party plugins use read-only executable/runtime files, an isolated per-plugin/per-job writable workspace, isolated PID/IPC/mount namespaces, no-new-privileges, process/resource limits and a denied network namespace. Close inherited file descriptors except protocol pipes and explicitly scoped artifact streams. Do not mount D-Bus, libvirt, SSH-agent, credential-store, helper or coordinator sockets.

File grants expose only approved paths and directions. Core inventory is supplied via a scoped RPC, not a database mount. Network is `none`, `brokered`, or explicitly `unrestricted`. In brokered mode the host mediates approved requests with TLS validation, endpoint policy, redirect checks and DNS rebinding/private-network rules appropriate to the permission. In unrestricted mode disclose that fine-grained hostname restrictions are not enforced; do not claim otherwise.

If user namespaces/sandbox dependencies are unavailable, normal third-party execution fails closed. An explicit trusted-development bypass may run an unsigned plugin unconfined, but it must be conspicuously labeled full user-code trust, never root, and disabled for unattended execution. Process separation without confinement is not a security guarantee.

## Data and command hygiene

No shell concatenation of untrusted input. Use argument arrays, `--` where supported, and explicit format/backend allowlists. Reject control characters in display labels and strip terminal escape/control sequences from logs, guests and plugins. Limit log sizes and protocol frames to prevent memory exhaustion.

Do not accept untrusted imported `qemu:commandline`, host filesystem devices, host executable paths or arbitrary hooks as automatically approved libvirt configuration. Preserve legitimate existing expert settings without granting them to an unrelated import context.

Secrets are references; pass resolved values through protected memory/FD mechanisms where practical. Avoid arguments visible in process listings, redact diagnostics and never write secrets into the job journal. Document unavoidable trust limits of the local OS user and host administrator.

## Durability and scheduling

SQLite WAL is an implementation choice, not a backup for the database. Configure appropriate synchronous durability and test crash recovery; checkpoint/backup SQLite using supported methods. Do not store the journal on unsupported network filesystems. [S31]

Full unattended functionality requires the user service to persist, necessary credentials to be unlocked/available, destination storage mounted and any standing grants valid. The onboarding and policy wizard tests these conditions. Missed runs are recorded, not silently erased. Notifications are local by default; external notification integrations are optional plugins.

## Security release gates

Fuzz XML/archive/network/schema parsers and RPC decoders. Include path traversal, malformed Unicode, duplicate JSON keys, symlink swaps, inherited-FD leaks, plugin privilege escalation attempts and network-policy bypass tests. Verify failure with SELinux enforcing on the Fedora target and normal AppArmor policy on the Ubuntu target. Never disable host defenses to obtain a green test result.
