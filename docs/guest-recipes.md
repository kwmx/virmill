# Guest recipes through verified SSH

The current existing-guest workflow runs a reviewed POSIX recipe as an explicit
non-root user over SSH. The CLI and TUI use `internal/guestsetup` and the same
durable operation engine. The Linux `internal/backend/guestssh` adapter uses the
administrator-installed `/usr/bin/ssh`; it does not execute recipe shell code on
the host. This workflow does not create the guest account, install its public
key, discover an address, start the VM, or enable a guest agent.

## Prepare the guest and inputs

Select the exact persistent, running VM UUID on `qemu:///system` or
`qemu:///session`. Managed-save state is refused. Supply a reachable canonical
literal IPv4 or IPv6 address and a port from 1 through 65535. Private IPv4
addresses are accepted. Hostnames, loopback, unspecified, multicast,
IPv4-mapped IPv6 and scoped/link-local addresses are not accepted.

Establish the address and SSH host key through a trusted channel. The plan
requires acknowledgement that **you bind this address and verified host key to
the selected VM**. Libvirt's VM fingerprint and a successful SSH connection do
not independently prove that mapping. The preview and result explicitly keep
`nativeAddressBindingVerified: false`. The separate `vm readiness show VM_UUID`
action observes guest-agent readiness; it supplies neither an SSH address nor
this identity proof. See [guest-agent readiness](guest-agent-readiness.md).

Both `identityFile` and `knownHostsFile` must be distinct, nonempty, ordinary
single-link files, each owned by the coordinator's ordinary user, with **exact
mode 0600**, and at most 1 MiB each. The known-hosts file needs the verified entry
for the supplied address and port. Use canonical absolute paths, without
symlinks in any component. Fields inside the input JSON do not expand `~`,
environment variables or relative paths. Keep these files in a private 0700
directory; that directory mode is operator guidance, while the adapter enforces
the file ownership, mode, generation and no-symlink requirements.

Authentication is noninteractive public-key authentication. Password prompts,
keyboard-interactive authentication, agents, certificates, hardware-key
providers, proxy commands/jumps, host-key auto-update, connection sharing, TTYs
and forwarding are disabled. A key that requires an interactive passphrase or
agent cannot work through this path. Missing or changed host keys fail; there
is no insecure fallback. Credential bytes are pinned, hashed and copied into
sealed anonymous files for OpenSSH. They are not put in the recipe, arguments,
operation response or log. The reviewed plan does retain credential paths and
digests. The local adapter requires Linux procfs, sealed anonymous files,
`openat2`, and filesystem `statx` birth identity support; unavailable safeguards
produce an error.

## Recipe document and stage policy

[The supplied example](../examples/guest-recipes/posix-readiness.json) is a
readiness-only recipe named `posix-readiness`, version `1.0.0`. Its check returns
0 for a non-root session, its apply script deliberately fails if called, and its
verify script also checks that the guest home directory exists. It installs
nothing and does not qualify an application's readiness.

The strict JSON document must include every field shown in that example.
Unknown fields, duplicates, case aliases, missing fields and null substitutes
are refused. The supported declaration is `apiVersion: virmill/v1`,
`kind: GuestRecipe`, `profile: linux-posix`, `privilege: non-root`,
`transport: ssh`, and `reboot: never`. Names use lowercase letters, digits and
hyphens with a leading letter, up to 63 characters. Versions have three numeric
components. `idempotent` is an explicit boolean; `timeoutSeconds` is 1–300 per
recipe stage. Each check/apply/verify script is nonblank UTF-8 text, at most
64 KiB, with no unsafe controls; the complete document is at most 256 KiB.
The selected recipe file must be ordinary, single-link and reached without
symlinks. A canonical relative recipe path is resolved by the CLI/TUI before
submission; credential paths must already be absolute.

| Stage | Successful outcome | Other outcomes |
| --- | --- | --- |
| `readiness` | Fixed `id -u` exits 0, emits exactly a canonical nonzero decimal UID plus newline, and emits no stderr. | Root, malformed output or failure blocks recipe scripts. This stage has no supplied arguments and at most 10 seconds. |
| `check` | Exit 0 records `already-configured`; exit 3 records `needs-apply`. | Every other exit fails the stage. |
| `apply` | Runs only after check exit 3; exit 0 records `applied`. | After check exit 0, records `skip-apply` without SSH execution. Any executed nonzero exit fails. |
| `verify` | Always required after the successful check/apply path; exit 0 records `verified`. | Any nonzero exit fails completion. |

SSH exit 255 is a transport/authentication/host-key failure, not an accepted
guest exit status. The service accepts other exits only according to the table.
Each executed stage has a separate SSH session. Recipe scripts arrive on stdin
to the guest's `sh -s --`; supplied arguments are quoted positional data. Shell
variables and working-directory changes do not carry between stages. Scripts
and arguments are approved executable content and persist in the private
journal, so do not embed passwords, keys or tokens in either. There are at most
64 arguments, at most 4096 bytes each and 16 KiB combined; use `[]` when none
are needed.

## CLI: plan, review, apply, result

These examples use placeholder VM, address and local file paths. Replace them
with the selected guest's verified values. The JSON input has exactly the six
fields shown, including the empty argument array.

```sh
virmill config validate examples/guest-recipes/posix-readiness.json

virmill --connection qemu:///system --output json guest recipe run \
  VM_UUID /home/alice/recipes/posix-readiness.json --plan \
  --input '{"address":"192.168.122.50","port":22,"user":"alice","identityFile":"/home/alice/.ssh/virmill_guest","knownHostsFile":"/home/alice/.ssh/virmill_known_hosts","arguments":[]}'

virmill --connection qemu:///system --output json plan show PLAN_ID

virmill --connection qemu:///system --output json plan apply PLAN_ID \
  --digest REVIEWED_PLAN_DIGEST --idempotency-key UNIQUE_REQUEST_KEY \
  --ack guest-execution,guest-host-key-binding,non-root-guest-setup --detach

virmill --connection qemu:///system --output ndjson --timeout 16m \
  operation watch OPERATION_ID --follow

virmill --connection qemu:///system --output json \
  guest recipe result OPERATION_ID
```

`guest recipe run` only plans. It observes the VM, local SSH executable and
credential files, but does not connect to the guest. Review the exact recipe,
arguments, resource UUID, connection, target, tool version/SHA-256, recipe and
script digests, risks and acknowledgement list. The recipe digest represents
the canonical decoded document; the script digests represent the script bytes.
Changing the source recipe file later does not replace the frozen reviewed
content. Changes to the VM fingerprint, SSH executable or credential bytes are
refused on apply and at execution boundaries.

Use the returned plan ID and exact digest. A non-idempotent recipe additionally
requires `--ack non-idempotent-recipe`; always copy the actual reviewed list.
The apply response supplies the operation ID, which differs from the VM and
plan IDs. Preserve the request's idempotency key when recovering a lost apply
response. `--plan=false` is refused; `run` never doubles as an apply command.
`--wait` can replace `--detach` on `plan apply` with an appropriate client
timeout. Detaching or reaching that timeout leaves an accepted job running.

## TUI access

Start `virmill --connection qemu:///system` in a terminal. In **VMs**, select
`guest recipe run`, or press `/`, enter the action name, leave search and select
it. Enter submits this form for planning:

```json
{"id":"VM_UUID","path":"/home/alice/recipes/posix-readiness.json","input":{"address":"192.168.122.50","port":22,"user":"alice","identityFile":"/home/alice/.ssh/virmill_guest","knownHostsFile":"/home/alice/.ssh/virmill_known_hosts","arguments":[]}}
```

Page through the returned preview with PgUp/PgDn. Press `a` to open approval,
review its acknowledgements, then enter the complete plan digest. The TUI
submits that exact plan and its displayed acknowledgement list only after a
matching digest. Tab switches input/review focus within the dialog; Esc closes
the form or approval without submitting. Navigation and opening the approval
dialog do not execute scripts. `guest recipe result` takes the operation UUID;
the **Jobs** actions expose operation state, cancellation and reconciliation.
Quitting the TUI detaches and does not cancel an accepted job.

## Durable evidence and interruption

Before each stage, the coordinator records an exclusive intent bound to the
operation, plan/input digests, VM resource, target digest, arguments digest,
script digest and SSH executable digest. A completed stage records its exit
status, outcome, timestamps, output byte counts and SHA-256 digests. Readiness
also records the observed UID. Raw stdout/stderr and transport diagnostics are
withheld from service results and durable records. The transport bounds stdout
to 1 MiB and stderr to 256 KiB per invocation; overflow cancels the process and
does not produce a successful stage receipt. Hashes and byte counts are
completion evidence, not a readable output log or proof that secret material
could never be inferred from a low-entropy hash.

`guest recipe result OPERATION_ID` returns stage intents, receipts, `complete`
and `effectUnknown`, including available failure/interruption evidence for the
owning actor. Overall `complete` requires a succeeded job and successful bound
receipts for all four stages, including a recorded skipped apply. It does not
upgrade the guest's state into general application, network or boot readiness.

Use `operation cancel OPERATION_ID` for a durable cancellation request. The
service polls cancellation while SSH runs and cancels the owned local SSH
process group. It cannot roll back or prove cessation of a command that already
ran remotely. A timeout, disconnect, coordinator interruption or lost receipt
may therefore leave an intent with `effectUnknown: true`. Retain the job and
inspect the guest through an independently authorized method before deciding
on further work.

`operation reconcile OPERATION_ID` checks existing bound receipts only: it
does not reconnect, rerun scripts, apply cleanup or automatically retry even an
`idempotent: true` recipe. Missing or failed receipts remain recovery-required.
The shared engine can recover overall success only when the final stage is
already proven; a proven earlier stage does not authorize execution of later
stages. A fresh attempt requires a new reviewed plan, and unresolved operation
locks may require explicit recovery first. Do not erase the journal to bypass
that boundary.

## Supported scope and checks

The non-root declaration and readiness UID check are not a guest sandbox.
Approved shell code still has the selected guest account's authority; the
coordinator cannot prevent a malicious recipe from invoking guest-side
privilege tools or rebooting. Review the code and the account's permissions.
Only the declared Linux/POSIX, non-root, SSH, no-reboot profile is implemented.
There is no Windows/appliance recipe executor, automated reboot/resume,
cleanup/uninstall framework, recipe catalog list/show command, unattended secret
store integration or bundled shell-profile archive recipe in this slice.
The supplied example verifies a POSIX session and home directory only.

On 2026-09-08, the following local generated-fixture/schema check passed:

```text
./scripts/go test -mod=vendor ./internal/guestsetup ./internal/validation -run '^TestGuestRecipe' -count=1
ok  virmill.local/core/internal/guestsetup  1.117s
ok  virmill.local/core/internal/validation 0.021s
```

It exercises both check branches, frozen recipe content, withheld output,
failed/root/uncertain/canceled stages, no replay during reconciliation, stale
VM/key/tool refusal, and declaration rejection. These tests use a generated
journal and an injected SSH boundary; this documentation task made no SSH
connection and does not claim real-guest qualification. The example file
SHA-256 at review was
`15c6199af75f0b7c73caf73d3a5557b50e439dbea8d9d74ec645fd75b55fb840`.

The normative [requirements](../virmill-v1-spec/contracts/requirements.json)
map this implementation primarily to **GUEST-02** (approved context, hashes,
privilege, retries and completion evidence), with **GUEST-03** support for
explicit credentials/address and verified host keys. Its non-root readiness
gate contributes to **GUEST-01**, but does not establish a user or install keys;
first-boot provisioning remains a separate workflow. All three scenarios
require real-guest evidence. This document neither promotes acceptance status
nor substitutes local tests for that evidence. The normative
[guest provisioning contract](../virmill-v1-spec/docs/08-devices-and-guest-setup.md)
remains the broader mandatory scope.
