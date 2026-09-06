# 09 — Declarative labs and reusable templates

## Lab definition

A lab is a versioned desired-state document containing metadata, local network definitions/references, VM definitions/template references, guest setup, dependencies and readiness policies. It is not arbitrary host shell code. See the example and JSON schema supplied with this package.

Resources use a lab namespace plus stable IDs. A repeated apply with unchanged inputs should be a no-op after observation confirms the desired state. A changed template tag is not silently adopted: resolve tags to immutable versions/digests in the deployment lock record.

## Planning and apply

1. Parse schema; reject unknown unnamespaced fields and semantic errors.
2. Resolve templates, images, existing resource references and secrets without exposing secret values.
3. Detect duplicate IDs/MACs, CIDR conflicts, missing required networks, unsupported profiles and dependency cycles.
4. Produce a DAG: source acquisition → pools/networks → volumes → domains → start → guest setup → readiness checks.
5. Present creations, edits, recreations, downtime and retained/deleted resources before apply.
6. Execute independent nodes with bounded concurrency, respecting shared storage and network locks.
7. Reconcile per node and report complete, partially converged or recovery-required states.

Cross-VM operations are not a single ACID transaction. If one VM fails after another starts, preserve the successful resources unless an explicit rollback policy says otherwise. Compensations are best-effort and cannot reverse arbitrary guest side effects. Show residual state and a safe continue/rollback plan.

Edits that require replacement must identify exactly what will be recreated, and which disks/data are preserved. `apply` must never treat a changed label as permission to wipe a VM.

## Dependency and readiness semantics

Dependencies may target `defined`, `running`, `guest-agent-ready`, `ssh-ready`, or an approved health check. “Running” is a hypervisor state, not guest/application readiness. Each gate has a timeout, retry/backoff policy and failure action. Use instance identity to avoid accepting a stale service from another guest.

Guest health checks are local to the lab or explicitly declared endpoints. They do not run arbitrary host shell code. Secrets use scoped references. IP allocation can be deterministic through reservations; it is not deterministic merely because the first VM happened to receive the first DHCP address once.

## Lab networking

One VM can be a normal workstation with internet plus several lab NICs, while target VMs have only guest-only/lab connections. Router appliances are allowed but must declare their forwarding role, and the topology view warns that they connect trust boundaries. No automatic guest routing daemon is created without a requested router VM or supported guest configuration.

External networks are referenced, not adopted implicitly. Destroying a lab never removes a shared LAN bridge, an external NAT network, another lab's VM, or backups. Block teardown while a VM outside the lab references a lab-owned resource, and show the dependent VM.

## Lab restore points

Offer `coordinated-cold` capture for the baseline full-lab recovery workflow: stop dependent guests in reverse order, capture complete VM states/configurations and network definitions, then restart in dependency order if approved. Record the stop/capture window and guest hook status.

A group of sequential live VM snapshots is not an atomic distributed application checkpoint. If live multi-VM capture is offered, report per-VM timestamps, consistency class and skew, and explicitly disclose the lack of global application consistency. Databases clustered across VMs need application-aware orchestration, not a marketing label.

Restore defaults to a separate lab namespace and isolated network mappings. Preserve original network/identity only under a replacement recovery plan with conflict checks.

## Templates

A template version is immutable: disk set, minimal virtual hardware, guest preparation status, provenance and setup defaults. Creating a template from a running VM requires a safe capture; do not directly treat its live mutable disk as a reusable base.

For Linux templates, use supported generalization procedures for machine ID, SSH host keys and cloud-init instance state. Preserve the original VM by preparing a copy. For Windows, record whether the guest was generalized using a supported procedure; do not claim a copied disk is safe for unrestricted cloning. Guest licensing/provisioning requirements remain visible without embedding product keys.

Template versions can be marked `prepared`, `unprepared` or `verified`. Only verified tests justify a verified badge. Images with unknown secrets or fixed appliance identities are flagged before cloning.

## Clone semantics

- **Full clone:** independent copies or flattened disks; new VM/MAC/guest identity where supported; no hidden template dependency.
- **Linked clone:** new writable overlays referencing immutable template disks; faster/smaller initially but dependent on those bases.
- **Recovery restore:** preserves required guest/TPM/firmware identity according to the backup; it is not automatically equivalent to cloning.

Expose clone dependencies in UI and exports. Template deletion and pool relocation must account for every dependent clone. Flattening a linked clone is a planned operation with its own free-space requirements and interruption recovery.

Creating a new template version never mutates existing clones. Updating a clone to another template version is a separate guest/data migration workflow, not changing the backing-file pointer underneath it.

## Declarative input rules

Inputs can use explicit local template references or an approved source registry pinned to a digest. No arbitrary remote include, environment-variable expansion inside commands, executable YAML tags, or shell interpolation. Relative file references resolve under the lab file's approved directory; paths outside it require explicit authorization.

The included example assumes locally available template references; those references are not downloadable bundled operating systems. `lab validate` checks schema and semantics, while `lab plan` additionally checks the actual host, templates and capabilities.

## Acceptance

Require successful no-op second apply; a changed CPU/RAM setting with correct live/next-boot handling; two isolated labs without accidental shared names; multi-network workstation plus isolated target; injected mid-DAG failure and safe retry; external-resource-preserving destroy; full/linked clone identity tests; blocked deletion of a live base; and restore into a new namespace without address collisions.
