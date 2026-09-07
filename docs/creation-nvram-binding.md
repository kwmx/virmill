# Creation NVRAM declaration binding

New UEFI creation plans review and persist `nvramDeclarationVersion: 1` under
[ADR 0021](adr/0021-creation-nvram-declaration-binding.md). This version requires
the coordinator to bind the declared NVRAM destination before confirming VM
definition. The review also reports `nvramInitializationVerified: false` and
requires the `new-firmware-state` acknowledgement. That acknowledgement approves
the disclosed clone-creation effects; it does not certify fresh firmware or TPM
state. Creation still defines a powered-off VM.

After observing the exact definition, the coordinator obtains typed cold-state
inspection for the same VM identity and configuration fingerprint. The VM must
be stopped, without managed save or autostart. The declared loader, template and
formats must match the reviewed firmware, and the assigned NVRAM destination
must be a canonical, nonempty absolute path. A compare-and-put record binds the
original plan/input/job/native resource identity, creation binding, firmware
digest, firmware/NVRAM mapping and recorded observation fingerprint. Conflicting
observations cannot replace an existing record. Uncertain completion retains
resources for explicit reconciliation without replaying allocation or definition.

The binding records the **first durably observed declaration**. It cannot prove
the historically first path assigned by libvirt. For example, if libvirt assigned
path A and acknowledgement was lost before the journal recorded it, a later
observation of path B cannot reconstruct A. Once a declaration is durably bound,
subsequent reconciliation must match that mapping and retain the historical
observation fingerprint; unrelated later configuration changes do not rewrite
that history.

Canonical path validation is lexical. It does not establish file existence,
symlink resolution, ownership, generation, creator, byte contents or exclusive
access. Binding opens no auxiliary files, creates no NVRAM/TPM state and grants
no file capture or deletion authority. A result read reports durable evidence;
it does not perform a new native inspection. Fresh initialization, TPM identity,
complete cold capture and restore remain separate requirements under
[ADR 0020](adr/0020-cold-recovery-boundary.md).

## Reading creation results

Both interfaces display the shared `vm.creation.result` response. The
`operation.state`, `complete`, declaration flags and error envelope must be read
together:

| Declaration status | Meaning | Completion and initialization |
| --- | --- | --- |
| `pending` | Version 1 has no validated durable declaration available yet. | Active work remains `complete: false`; a defined receipt missing its required binding returns an error. Initialization stays unverified. |
| `declaration-bound` | A validated durable record supplies the exact declared path and creation identity. | This may appear while verification is still running or with a recovery error. Only a successful creation with its required receipt is `complete: true`; initialization remains unverified. |
| `legacy-unbound` | The historical recipe omitted the declaration version. | Its original completion semantics remain unchanged. Even a successful old definition has `nvramDeclarationBound: false` and `nvramInitializationVerified: false`. |

The bound record appears as `nvramDeclaration`, including
`firmware.nvram.path` and the historical `observedFingerprint`. Missing or
conflicting required records fail without claiming complete creation. A bound
record alone does not override an incomplete or failed operation. Transport and
cancellation failures must not reuse an earlier successful response.

Fieldless old recipes and receipts are not silently upgraded or rebound, and
their stored digests remain unchanged. Unsupported declaration versions fail.
BIOS creation acquires no NVRAM authority. The result's `guestBootVerified`,
`setupVerified` and `connectivityVerified` remain separate from volume and
definition completion. None of these declaration fields establishes a complete
backup or a recovered guest.

Definition recovery retains the original recipe's declaration policy. Its review
shows that version and the unverified initialization stage, and completion requires
the same original binding. Result reads validate the stored digests of both the
recovery child and original creation before reporting completion. A changed
recipe cannot silently select different completion rules.

## CLI and TUI

The following commands use the existing shared service. Supply the prepared
source operation ID and a valid creation JSON input containing explicit UEFI
code/template/format choices, as described in the
[creation input documentation](vm-creation.md). For example, with that
JSON already in `VIRMILL_CREATION_INPUT_JSON`:

```sh
virmill vm create PREPARED_OPERATION_ID --input "$VIRMILL_CREATION_INPUT_JSON" --connection qemu:///session --output json --non-interactive
virmill plan show PLAN_ID --connection qemu:///session --output json --non-interactive
```

Review the complete stored plan, risks, digest and acknowledgements. Apply is a
separate authorized action using the exact returned digest and every required
acknowledgement; repeat `--ack` for each ID. The example below has placeholders
for those reviewed values:

```sh
virmill plan apply PLAN_ID --digest EXACT_REVIEWED_DIGEST --idempotency-key UNIQUE_REQUEST_KEY --ack ACK_FROM_REVIEW --connection qemu:///session --output json --non-interactive
virmill vm creation result CREATION_OPERATION_ID --connection qemu:///session --output ndjson --non-interactive
```

Reuse an idempotency key only for the same intended apply request. `vm create`,
`plan show` and `vm creation result` do not submit apply. Result takes the
creation **operation** ID, not the plan ID. JSON/NDJSON carry one response
envelope per call; a result with an error returns a nonzero CLI status and may
include incomplete progress evidence. A successful result read during active
work reports progress, not completion. The current table output displays the
same envelope as indented text. Exact flags are in the
[generated CLI reference](cli-reference.md).

In the TUI, choose **VMs → vm create**, then enter
`{"id":"PREPARED_OPERATION_ID","input":{...}}` with the same complete input.
Use PgUp/PgDn to read the review at 80×24. **Jobs → plan show** retrieves that
stored review. Press `a`, inspect the displayed acknowledgements, and type the
full digest to submit apply; a wrong digest or Esc submits nothing.
**VMs → vm creation result** accepts the creation operation ID. Result and error
views clear earlier approval state; pressing `a` on a result cannot apply it.

## Test scope

`internal/ui/cli/creation_nvram_test.go` and
`internal/ui/tui/creation_nvram_test.go` use the actual application service,
creation coordinator, operation engine, SQLite journal and UI action dispatch.
Only source/backend observations and historical result records are synthetic.
All potentially mutating fixture backend calls refuse execution. The UI suite
does not execute a successful creation lifecycle or access firmware/state files.

The tests cover version 1 review and saved-plan reuse; exact digest and
acknowledgement forwarding; refusal of missing firmware acknowledgement or
inspection capability; canceled authorization; bound, active, pending and
fieldless legacy results; missing/conflicting required records; failed creation;
discarded stale success on cancellation/transport errors; clean table/JSON/NDJSON
output; visible TUI details; and unchanged journal evidence and lock ownership
after observations.

Run locally with the pinned offline toolchain:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -tags libvirt_dlopen ./internal/ui/cli ./internal/ui/tui -run CreationNVRAM -count=1
```

This is prerequisite evidence for IMP-07 (distinct reported success stages),
UX-03 (clean noninteractive machine output), JOB-02 (UI observation preserves
uncertain records without replay), SNAP-01 (visible firmware declaration limits)
and REL-03 (documented shared commands). It does not satisfy their full native,
crash/reboot, snapshot/revert, guest or release acceptance requirements.
