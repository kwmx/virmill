# Virmill generated CLI reference

Development build. Only implemented commands appear here; the full 1.0 contract remains mandatory.

## `virmill`

Virmill local Linux virtualization suite (development build)

```text
Usage:
  virmill [flags]
  virmill [command]

Available Commands:
  backup      Manage backup
  completion  Generate shell completions
  config      Manage config
  doctor      Read-only prerequisites; never applies repairs
  help        Help about any command
  host        Manage host
  import      Manage import
  lab         Manage lab
  network     Manage network
  operation   Manage operation
  plan        Manage plan
  plugin      Manage plugin
  storage     Manage storage
  tui         Open the keyboard interface
  version     Show application and build contracts
  vm          Manage vm

Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics

Use "virmill [command] --help" for more information about a command.
```

## `virmill backup verify-manifest`

Check recovery member completeness; not a boot test

```text
Usage:
  virmill backup verify-manifest PATH [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill completion`

Generate shell completions

```text
Usage:
  virmill completion SHELL [flags]

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill config validate`

Validate a bundled declarative document offline

```text
Usage:
  virmill config validate PATH [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill doctor`

Read-only prerequisites; never applies repairs

```text
Usage:
  virmill doctor [flags]

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill help`

Help about any command

```text
Usage:
  virmill help [command] [flags]

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill host capabilities`

Probe the selected local libvirt connection

```text
Usage:
  virmill host capabilities [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill host helper identity`

Show the coordinator's public helper key fingerprint and administrator-policy status

```text
Usage:
  virmill host helper identity [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill host inspect`

Inspect read-only local host prerequisites

```text
Usage:
  virmill host inspect [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill host pci list`

Discover native PCI identity, drivers and observed IOMMU groups without detachment or passthrough authority

```text
Usage:
  virmill host pci list [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill import inspect`

Inspect bounded OVA packaging; no extraction or guest execution

```text
Usage:
  virmill import inspect PATH [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill import prepare`

Plan independent conversion of every selected OVA disk; no VM is defined

```text
Usage:
  virmill import prepare PATH [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill import prepare-disks`

Plan independent copies of an explicitly selected existing disk set and its backing files

```text
Usage:
  virmill import prepare-disks PATH [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill import prepare-install`

Plan copied ISO installation media and verified empty guest disks

```text
Usage:
  virmill import prepare-install PATH [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill import result`

Show prepared artifact paths and verification by operation ID

```text
Usage:
  virmill import result ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill import verify`

Verify declared prepared-import artifact hashes; no guest boot claim

```text
Usage:
  virmill import verify PATH [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill lab validate`

Validate declarative schema, network intent and dependency graph

```text
Usage:
  virmill lab validate PATH [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill network cidr check`

Check candidate CIDRs against host addresses, every route table, defined networks and explicit planned allocations

```text
Usage:
  virmill network cidr check [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill network list`

List existing libvirt networks; no isolation verification implied

```text
Usage:
  virmill network list [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill network show`

Inspect live/persistent network configuration by stable UUID

```text
Usage:
  virmill network show ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill operation cancel`

Request cancellation at a safe boundary

```text
Usage:
  virmill operation cancel ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill operation list`

List durable jobs

```text
Usage:
  virmill operation list [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill operation reconcile`

Observe an uncertain effect without replaying it

```text
Usage:
  virmill operation reconcile ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill operation show`

Inspect durable job state

```text
Usage:
  virmill operation show ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill operation watch`

Read ordered events after a cursor

```text
Usage:
  virmill operation watch ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plan apply`

Apply an immutable plan with explicit digest and acknowledgements

```text
Usage:
  virmill plan apply PLAN_ID [flags]

Flags:
      --ack strings              Explicit plan acknowledgement IDs
      --detach                   Return after durable submission
      --digest string            Exact reviewed plan digest (required)
      --idempotency-key string   Stable request key (required)
      --wait                     Wait for terminal operation state

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plan show`

Show the exact immutable plan

```text
Usage:
  virmill plan show ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin call`

Plan a confined read-only action on explicitly selected VMs

```text
Usage:
  virmill plugin call PLUGIN_ID ACTION_ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin disable`

Plan blocking new invocations while preserving resource metadata

```text
Usage:
  virmill plugin disable ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin enable`

Plan enabling supported confined actions after permission checks

```text
Usage:
  virmill plugin enable ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin install`

Plan signed package installation with exact key and grants

```text
Usage:
  virmill plugin install PATH [flags]

Flags:
      --after int           Event cursor
      --input string        JSON parameters; secrets must be references (default "{}")
      --key-id string       Signing-key identifier
      --plan                Return preview (apply separately after review) (default true)
      --public-key string   Reviewed hexadecimal Ed25519 public key

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin list`

List local installations and active immutable versions

```text
Usage:
  virmill plugin list [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin new`

Plan buildable Go action source at a new destination

```text
Usage:
  virmill plugin new [PATH] [flags]

Flags:
      --after int              Event cursor
      --id string              Stable plugin ID for a new scaffold
      --input string           JSON parameters; secrets must be references (default "{}")
      --language string        Scaffold source language (go)
      --plan                   Return preview (apply separately after review) (default true)
      --sdk-directory string   Reviewed local SDK source directory
      --type string            Scaffold extension type (action)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin pack`

Plan a signed package; signing key stays outside the payload

```text
Usage:
  virmill plugin pack PATH [flags]

Flags:
      --after int                 Event cursor
      --destination string        New output archive path for pack
      --input string              JSON parameters; secrets must be references (default "{}")
      --key-id string             Signing-key identifier
      --plan                      Return preview (apply separately after review) (default true)
      --signing-key-file string   Private signing-key file outside package source

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin permissions grant`

Plan explicit additional declared scopes and disable until reviewed

```text
Usage:
  virmill plugin permissions grant ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin permissions revoke`

Plan scope revocation and stop new invocations

```text
Usage:
  virmill plugin permissions revoke ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin permissions show`

Inspect exact declared and installed permission scopes

```text
Usage:
  virmill plugin permissions show ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin remove`

Plan removal from active inventory; retain data and created resources

```text
Usage:
  virmill plugin remove ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin result`

Read a durable plugin result by operation ID

```text
Usage:
  virmill plugin result ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin rollback`

Plan activation of the retained prior executable; no data migration

```text
Usage:
  virmill plugin rollback ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin show`

Inspect retained versions, trust and activation state

```text
Usage:
  virmill plugin show ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin test`

Run confined summary conformance using synthetic selected VM records

```text
Usage:
  virmill plugin test PATH [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin update`

Plan verified update; preserve the previous version and disable until reviewed

```text
Usage:
  virmill plugin update PATH [flags]

Flags:
      --after int           Event cursor
      --input string        JSON parameters; secrets must be references (default "{}")
      --key-id string       Signing-key identifier
      --plan                Return preview (apply separately after review) (default true)
      --public-key string   Reviewed hexadecimal Ed25519 public key

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin validate`

Validate a development manifest; no trust or signature claim

```text
Usage:
  virmill plugin validate PATH [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill storage access grant`

Plan actor-only read access to one stopped VM's managed volume through the approved helper

```text
Usage:
  virmill storage access grant ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill storage access result`

Read the durable helper observation for an access operation

```text
Usage:
  virmill storage access result ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill storage access revoke`

Plan exact access restoration from a successful original grant operation

```text
Usage:
  virmill storage access revoke ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill storage pool list`

List existing local libvirt pools without adoption or activation

```text
Usage:
  virmill storage pool list [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill storage pool show`

Inspect pool XML and available capacity by stable UUID

```text
Usage:
  virmill storage pool show ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill tui`

Open the keyboard interface

```text
Usage:
  virmill tui [flags]

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill version`

Show application and build contracts

```text
Usage:
  virmill version [flags]

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm autostart`

Plan autostart policy

```text
Usage:
  virmill vm autostart ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm boot show`

Inspect separate live and next-boot device order using disk targets and NIC MACs

```text
Usage:
  virmill vm boot show ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm create`

Plan managed copies, optional NoCloud provisioning and a new powered-off VM from a prepared source

```text
Usage:
  virmill vm create ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm creation accept`

Plan explicit acceptance of retained BIOS chipset devices after complete disk readback; preserve the original partial job

```text
Usage:
  virmill vm creation accept ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm creation cleanup`

Plan explicit retention or guarded deletion of failed-creation volumes and close the original recipe

```text
Usage:
  virmill vm creation cleanup ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm creation result`

Observe active progress and durable volume/definition stages by creation operation ID

```text
Usage:
  virmill vm creation result ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm creation resume`

Plan definition recovery using complete reverified retained volumes; never allocate or upload again

```text
Usage:
  virmill vm creation resume ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm list`

List native libvirt inventory without adoption

```text
Usage:
  virmill vm list [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm pause`

Plan pause

```text
Usage:
  virmill vm pause ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm restore-saved`

Plan restoration of saved state

```text
Usage:
  virmill vm restore-saved ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm resume`

Plan resume

```text
Usage:
  virmill vm resume ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm save`

Plan managed save (not an independent backup)

```text
Usage:
  virmill vm save ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm set`

Plan next-boot CPU/RAM, boot order or retained-media ejection on a powered-off VM

```text
Usage:
  virmill vm set ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm show`

Inspect live and persistent state by UUID

```text
Usage:
  virmill vm show ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm start`

Plan VM start

```text
Usage:
  virmill vm start ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm stop`

Plan graceful stop; never escalate on timeout

```text
Usage:
  virmill vm stop ID [flags]

Flags:
      --after int      Event cursor
      --hard           Plan abrupt power-off, requiring data-loss acknowledgement
      --input string   JSON parameters; secrets must be references (default "{}")
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout; jobs continue after detach (default 30s)
      --verbose             Verbose diagnostics
```

