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
  operation   Manage operation
  plan        Manage plan
  plugin      Manage plugin
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

Plan a lossless powered-off vCPU edit

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

