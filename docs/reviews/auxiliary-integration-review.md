# Auxiliary inspection integration review

Reviewed 2026-09-08: common auxiliary service, helper client/server dispatch,
`Client.connectRequest`, request/response binding, bundled input schemas and
[ADR 0022](../adr/0022-authenticated-auxiliary-inventory.md). Two source findings
were reported to the integration owner and corrected during this review. The
post-fix source addresses both; no further concrete integration defect was found
within this scope. These findings were source analysis, not pre-fix executable
reproductions. No production or test files were edited by this review.

## Corrected findings

1. **Final cancellation could retain inventory in an error response.** The initial
   `inspectAuxiliaryRequest` ended with `return response, ctx.Err()`. Cancellation
   after validation could therefore return both a populated inventory and an
   error. `Serve` retained that pointer while setting `success=false`, and the
   dedicated client correctly refused the mixed response as ambiguous
   `RECOVERY_REQUIRED`. This did not become a successful capture claim, but it
   violated the intended empty-payload failure contract and obscured the original
   failure. The current
   [server adapter, lines 33–36](../../internal/helper/auxiliary_server_linux.go#L33)
   returns nil on final cancellation. Additionally,
   [dispatch, lines 173–174](../../internal/helper/server_linux.go#L173) clears both
   access and auxiliary payloads for every error. The source path no longer
   constructs the contradictory response.

2. **The descriptor-aware receive path could extend the client timeout.**
   `connectRequest` sets the earlier of its ten-second socket deadline and the
   caller deadline, but
   [`receiveSnapshotFrame`, lines 176–178](../../internal/helper/sealed_linux.go#L176)
   applies the caller's read deadline directly. Before correction, a call with a
   five-minute context could replace the intended ten-second read cap. A helper
   stalled in native work could consequently hold the client beyond its transport
   bound; a socket deadline does not interrupt that native call. The current
   [dedicated client, lines 21–25](../../internal/helper/auxiliary_client_linux.go#L21)
   derives a ten-second context before connection setup and passes it through
   reception. A shorter caller deadline remains shorter. The receiver can no
   longer extend this call's cap. The shared sealed receiver's general contract
   remains unchanged.

The integration owner owns post-fix runtime/regression tests and their evidence.
This review verified the edited source, without duplicating the transport or UI
test matrices or claiming their executions.

## Authority and source trace

The [shared service](../../internal/auxiliary/service.go) accepts only a canonical
nonzero VM UUID and bounded `rootID`, on `qemu:///system`, from an ordinary actor.
The two [bundled input schemas](../../schemas/auxiliary-inspect-input.schema.json)
and [identity schema](../../schemas/auxiliary-vm-identity.schema.json) reject extra
properties and names in place of UUIDs. No public input supplies a state pathname,
expected inventory, apply request or action. The service gets the approved root
and signing-key ID from the helper client, observes the native VM, and requires
matching persistent stopped configuration without managed save or autostart
before requesting helper inspection (service lines 39–97).

[`connectRequest`](../../internal/helper/client_linux.go#L96) binds the actor to
the process UID, checks key identity, signs the typed request, reloads policy and
runs `Authorize`. It checks the root-owned socket and authenticates the actual
root peer through kernel credentials. The server independently obtains kernel
credentials, reloads the held administrator policy and authorizes the request
before dispatch (server lines 138–161). The
[auxiliary policy selection](../../internal/helper/auxiliary.go#L124) requires one
exact actor/key/VM/root match and bounded configured state ownership; legacy
actors, keys and roots alone grant no auxiliary authority. The already-corrected
root validator rejects NUL, control/format characters, invalid UTF-8, backslashes,
noncanonical paths and `/` in policy selection and response validation.

The helper selects source paths solely from its own fresh native observer through
[`AuxiliaryExecutor.Inspect`](../../internal/helper/auxiliary_inventory_linux.go).
The service's layout is a comparison value, not a helper-selected source path.
The helper independently checks native resource/fingerprint/persistence/state,
pins and repeats the approved filesystem observation, and does not read state
contents. See the separate [inventory review](auxiliary-inventory.md) for its
filesystem bounds and synthetic evidence.

[`AuxiliaryBinding`](../../internal/helper/auxiliary.go#L187) includes the actor,
operation, VM, root ID, plan digest, request nonce, key and full auxiliary payload.
The dedicated response validator checks that binding, nonce, `inspected` stage,
version, exact system VM, fingerprint, root ID, bounded collections and exact byte
sum; it forbids a capture artifact. The service additionally compares the root
pathname and full layout with its earlier observations (service lines 101–105).
The response validator is not a replacement for the executor's native and
filesystem derivation. A copied response is not an authenticated capture proof.

## Legacy calls, ownership and claim limits

The refactor retains legacy ACL actor/key/policy/root-peer checks, signed request
fields, access binding and response completion checks. The new optional
`Request.Auxiliary` field uses `omitempty`, preserving the old signed bytes when
absent. Existing
[`TestAuxiliaryAuthorizationLegacySignedBytesRemainExact`](../../internal/helper/auxiliary_authorization_test.go#L399)
compares the old representation for directory/grant/revoke requests and checks
that adding a freshly signed auxiliary payload is refused. This review inspected
that coverage rather than adding another matrix. Dedicated client and server
dispatch prevent auxiliary calls from reaching legacy directory or ACL execution.
As ADR 0022 states, software and helper should be upgraded together; this review
does not certify arbitrary mixed-version responses or policy files.

On connection failure, `connectRequest` retains ownership and closes its socket;
on success, each dedicated caller takes ownership and defers closing it. Context
callbacks advance deadlines without signaling another process or VM. The
descriptor-aware receiver uses `MSG_CMSG_CLOEXEC`, retains error-path ownership
of received descriptors, and transfers ownership only on a valid return. The
auxiliary decoder closes and rejects any returned descriptor, including a valid
sealed one. Legacy `ReceiveSealedSnapshot` still requires exactly one sealed
descriptor; permitting no descriptor is private to the typed metadata/error path.

This connected endpoint creates no coordinator job, helper intent, archive or
captured state. Its random wire `jobID` and request digest correlate a read; they
are not a durable job or reviewed capture plan. Server modes other than `inspect`
return `UNSUPPORTED_CAPABILITY` before native inventory or legacy execution.
The shared result leaves capture, independent-restore and guest-boot verification
false. Socket cancellation remains bounded, but cannot make blocking kernel/native
work interruptible or replace a producer guard.

## Compile checks and acceptance scope

Both commands used `./scripts/go`, `GOPROXY=off`, `GOSUMDB=off`, `-mod=vendor`,
`CGO_ENABLED=0` and `go test -c ./internal/auxiliary`, with generated output in
private temporary directories removed afterward:

- `GOOS=darwin GOARCH=arm64`: passed; Mach-O 64-bit arm64 test executable.
- `GOOS=windows GOARCH=amd64`: passed; PE32+ x86-64 test executable.

Neither binary was executed. These checks establish that the common service and
its neutral contracts compile without the Linux registration/native adapter;
they make no foreign-platform runtime or helper support claim.

This is SEC-01 authority-boundary and SNAP-01 inventory prerequisite review.
JOB-02 requires crash/reboot reconciliation of uncertain effects; this read has
no such effects or durable job and supplies no JOB-02 fault-injection evidence.
Future capture remains subject to those requirements. No acceptance scenario is
promoted, and no SSH, native guest call, privileged host operation or ledger edit
was performed here.
