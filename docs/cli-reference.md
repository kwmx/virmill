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
  device      Manage device
  doctor      Check host prerequisites and show how to install what's missing (read-only)
  guest       Manage guest
  help        Help about any command
  host        Manage host
  import      Manage import
  lab         Manage lab
  network     Manage network
  operation   Manage operation
  plan        Manage plan
  plugin      Manage plugin
  snapshot    Manage snapshot
  storage     Manage storage
  tui         Open the keyboard interface
  update      Install the newest Virmill release from GitHub
  version     Show application and build contracts
  vm          Manage vm

Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics

Use "virmill [command] --help" for more information about a command.
```

## `virmill backup create`

Plan an encrypted backup of a complete capture with independent restore and full member verification

```text
Usage:
  virmill backup create ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup policy preview`

Preview scheduled instants in the declared timezone without creating capture jobs

```text
Usage:
  virmill backup policy preview PATH [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup policy validate`

Validate backup policy selectors, cron and timezone without installing a schedule

```text
Usage:
  virmill backup policy validate PATH [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup receipt export`

Save a portable backup recovery receipt without credentials

```text
Usage:
  virmill backup receipt export OPERATION_ID PATH [flags]

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup receipt read`

Read a saved recovery receipt; repository contents are verified during recovery

```text
Usage:
  virmill backup receipt read PATH [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup receipt show`

Read a portable recovery receipt for a verified backup operation

```text
Usage:
  virmill backup receipt show ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup receipts`

List recent verified backups available for recovery

```text
Usage:
  virmill backup receipts [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup repository check`

Plan a full local repository data-integrity check with durable evidence

```text
Usage:
  virmill backup repository check PATH [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup repository init`

Plan a new private encrypted local restic repository using an explicit credential file

```text
Usage:
  virmill backup repository init PATH [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup restore`

Plan recovery from an exact repository snapshot into an absent local capture identity

```text
Usage:
  virmill backup restore ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup result`

Read durable repository and recovery proof by operation UUID

```text
Usage:
  virmill backup result ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill backup verify-manifest`

Check declared recovery metadata and optional member integrity; no complete-capture or boot proof

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill device usb list`

Discover USB identity, serial and observed physical port; discovery does not authorize attachment

```text
Usage:
  virmill device usb list [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill doctor`

Check host prerequisites and show how to install what's missing (read-only)

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill guest recipe result`

Inspect durable guest recipe stages and completion without exposing guest output

```text
Usage:
  virmill guest recipe result ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill guest recipe run`

Plan an exact non-root guest recipe through explicit SSH credentials and verified host keys

```text
Usage:
  virmill guest recipe run VM_UUID RECIPE_PATH [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill guest tools catalog`

Show supported guest tools and installation requirements

```text
Usage:
  virmill guest tools catalog [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill guest tools install`

Plan built-in guest tools over verified SSH; guest administrator privileges are explicit

```text
Usage:
  virmill guest tools install ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill import describe`

Read appliance settings quickly; payload checksums are verified during preparation

```text
Usage:
  virmill import describe PATH [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill import discard`

Plan removing a finished import's work folder and, unless --keep-images, its prepared images to free space

```text
Usage:
  virmill import discard ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --keep-images    Remove only the work folder; keep the prepared images for more VMs
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill import source describe`

Detect supported appliance, disk or ISO metadata without importing

```text
Usage:
  virmill import source describe PATH [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill import sources`

List your completed image preparations for VM creation

```text
Usage:
  virmill import sources [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill network create`

Plan a new NAT/lab network with explicit host access or a guest-only segment without host L3

```text
Usage:
  virmill network create [PATH] [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill network creation result`

Inspect retained network identity, subnet reservation and live creation state by operation ID

```text
Usage:
  virmill network creation result ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill network creation resume`

Plan activation of the exact retained inactive network from an unresolved creation; never redefine

```text
Usage:
  virmill network creation resume ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill operation watch`

Read ordered events after a cursor

```text
Usage:
  virmill operation watch ID [flags]

Flags:
      --after int      Event cursor
      --follow         Follow ordered events and terminal state with --output ndjson; timeout detaches
      --input string   JSON parameters; secrets must be references (default "{}")

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill plugin test`

Run confined action or reference-provider conformance with generated state

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill snapshot create`

Plan a complete stopped capture with independent disks, original configuration and required firmware/TPM state

```text
Usage:
  virmill snapshot create ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill snapshot list`

List retained local recovery set identities

```text
Usage:
  virmill snapshot list [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill snapshot restore`

Plan independent new volumes and a new disconnected VM from a complete cold recovery set

```text
Usage:
  virmill snapshot restore ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill snapshot show`

Verify and inspect every member of a retained local recovery set

```text
Usage:
  virmill snapshot show ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill storage pool create`

Plan a new folder-backed pool for VM disks; defaults to libvirt's standard images folder

```text
Usage:
  virmill storage pool create [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --name string    Pool name (default: default)
      --no-autostart   Do not start the pool automatically when the host starts
      --path string    Folder for VM disks (default: libvirt's standard images folder for the connection)
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill storage pool start`

Plan starting an existing stopped pool; it also starts with the host unless --no-autostart

```text
Usage:
  virmill storage pool start ID [flags]

Flags:
      --after int      Event cursor
      --input string   JSON parameters; secrets must be references (default "{}")
      --no-autostart   Leave automatic start with the host unchanged
      --plan           Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill update`

Install the newest Virmill release from GitHub

```text
Usage:
  virmill update [flags]
  virmill update [command]

Available Commands:
  check       Check GitHub for a newer Virmill release (read-only)
  checks      Show or change the daily update check

Flags:
      --download-only   Download and verify the packages, then print the install command instead of running it
      --yes             Do not ask before downloading and installing (dnf or apt may still ask for your password)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics

Use "virmill update [command] --help" for more information about a command.
```

## `virmill update check`

Check GitHub for a newer Virmill release (read-only)

```text
Usage:
  virmill update check [flags]

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill update checks`

Show or change the daily update check

```text
Usage:
  virmill update checks [on|off] [flags]

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm boot set`

Review next-boot device order and eject retained installer media

```text
Usage:
  virmill vm boot set ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm console open`

Open the guest display or a configured serial console

```text
Usage:
  virmill vm console open VM_UUID [flags]

Flags:
      --choice string   Console choice ID from vm console show; optional when exactly one is available

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm console show`

Inspect available local graphical and serial console access

```text
Usage:
  virmill vm console show ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm creation options`

Show supported local CPU, memory, machine and firmware choices

```text
Usage:
  virmill vm creation options [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm guest-agent enable`

Review enabling the guest-agent channel on a stopped VM; tools install separately

```text
Usage:
  virmill vm guest-agent enable ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm guest-agent show`

Check whether guest-agent integration is configured

```text
Usage:
  virmill vm guest-agent show ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm readiness show`

Observe a running VM's guest-agent channel and one bounded guest ping; no application readiness claim

```text
Usage:
  virmill vm readiness show ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm reboot`

Plan one graceful reboot verified by a selected-domain event; no hard-stop fallback

```text
Usage:
  virmill vm reboot ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm recovery auxiliary inspect`

Inspect complete explicit firmware/TPM member metadata through VM-specific administrator policy; no state bytes are read or captured

```text
Usage:
  virmill vm recovery auxiliary inspect ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm recovery inspect`

Inspect configured disks, backing sources, firmware, TPM and unresolved dependencies without reading source bytes

```text
Usage:
  virmill vm recovery inspect ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm remove`

Plan removal of a stopped VM; keep disks by default or explicitly select disks for deletion; backups are retained

```text
Usage:
  virmill vm remove ID [flags]

Flags:
      --after int                 Event cursor
      --delete-disk stringArray   Permanently delete selected guest disk targets (vda,vdb); repeatable; omitted keeps all disks
      --input string              JSON parameters; secrets must be references (default "{}")
      --plan                      Return preview (apply separately after review) (default true)

Global Flags:
      --config string       Configuration path (reserved; nonempty input is rejected)
      --connection string   Explicit local libvirt connection (default "qemu:///system")
      --no-color            Disable color (output is plain by default)
      --non-interactive     Never prompt
      --output string       table, json or ndjson (default "table")
      --quiet               Suppress human output
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

## `virmill vm resources show`

Show current and next-boot CPU/RAM values and supported edits

```text
Usage:
  virmill vm resources show ID [flags]

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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
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
      --timeout duration    Client wait timeout (import checks and plan submission default to 20m); accepted jobs survive detach (default 30s)
      --verbose             Verbose diagnostics
```

