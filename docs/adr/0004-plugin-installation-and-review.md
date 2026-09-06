# ADR 0004 — Immutable plugin installations and reviewable plans

Status: accepted implementation decisions within the existing v1 scope.

The normative requirements in documents 02, 10, 11 and 12 take precedence over
the supplied baseline schemas. A digest alone does not show the operator the
requested setting, destination or permission change. Operation plans therefore
gain an optional `review` object in the implementation schema. Each handler
constructs non-secret review details from its canonical execution input before
the plan digest is computed. Existing plans remain readable. The original package
schema is preserved unchanged. This is an additive schema clarification.

Plugin installation records use versioned schema 1 in the existing metadata
table. A newer record version is refused. Each package is stored under its signed
canonical-inventory digest at `$XDG_DATA_HOME/virmill/plugins/DIGEST`, together
with its original signed archive. Payloads and directories are flushed before
the active reference changes in a SQLite transaction. Hashes and signatures are
checked during planning, execution and each invocation. No installation hooks run.

Trust is package-scoped. Installation supplies the signing-key ID and public key;
the plan displays the key fingerprint, package digest and exact declared/granted
scopes. Its acknowledgements bind that review. A valid signature does not establish
ownership of an author's namespace. There is no implicit global trust-on-first-use
store, public registry, remote download or release-signing identity.

Updates retain earlier packages, reject replacing a known semantic version with
different bytes, and start disabled. Permission edits also disable new invocations.
Enable checks protocol, platform, extension and required permission support.
Rollback changes the active executable reference and stays disabled. This runtime
does not mount persistent plugin data or execute migrations, so it cannot claim
compatibility with a plugin data migration that it never performed.

Removal hides and disables the installation but keeps signed versions and recovery
metadata. It does not delete plugin-created VMs, disks, backups or external objects.
No package garbage collection is currently exposed. Active/uncertain invocation
jobs require explicit `activeJobs: finish` disposition or resolution through Jobs.
Their package versions remain pinned. The activation transaction rechecks active
job locks to close the race between preview and metadata commit.

The installed runtime currently executes confined read-only action plugins with
`network: none` and, when needed, `vm.read: selection`. Each invocation stages a
fresh verified package, binds that tree read-only, and receives only approved
selected VM facts. The core plan binds package, input, action schema, fingerprints
and expiring scopes. The execution result is schema-checked and durably bound to
the plan and operation before success. Lost acknowledgements reconcile from that
record; an absent result is not replayed automatically. All plugin-origin host API
requests remain denied in this runtime. No arbitrary helper authority is available.

This decision does not reduce v1. Unsigned/development trust override, quarantine,
typed mutating host mediation, artifact/secret/HTTP brokers, structured contribution
forms, persistent plugin-data migration and the complete extension/provider
conformance matrix remain mandatory implementation work.
