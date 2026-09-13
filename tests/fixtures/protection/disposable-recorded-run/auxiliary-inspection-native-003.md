# Native auxiliary metadata inspection recipe 003

This independently numbered successor retains the scope and preservation guards
of [recipe 001](auxiliary-inspection-native-001.md) and the capability-query and
volume access-time corrections of [recipe 002](auxiliary-inspection-native-002.md).
Neither failed earlier native attempt is rerun, overwritten or called successful.

Recipe 002 created the never-started guest
`50a0583b-3587-495f-90f4-9a42b8ad2fa3` and returned the expected original-policy
permission denial. Its test then failed because it required empty stderr. The
frozen CLI main function writes `CODE: message` and a newline to stderr in addition
to the typed stdout envelope. Recipe 003 now requires that exact matching stderr
on expected refusals; unknown diagnostics, wrong exits, extra stdout records,
retained success data and nonempty successful stderr remain failures. This fixes
the fixture's expectation and makes no product change.

The parent recorded the actual earlier errors and bounded before/after metadata
in `auxiliary-native-failure-review-001`. Recipe 002 remains failed, its original
policy was never replaced, and its generated guest/state are retained. The new
recipe includes that stopped guest and state in its preservation baseline.

Only this new namespace may be created or temporarily authorized:

```text
output: <test-vm-home>/virmill-tests/run-65930c6-20260907/auxiliary-inspection-native-003
state: /var/lib/virmill-host-helper/auxiliary-fixture-003
root ID: auxiliary-fixture-003
domain name: virmill-auxiliary-fixture-003
```

The UUID is freshly chosen, and every output/state creation remains exclusive.
The guest has no disks/NICs, uses original generated dummy bytes, and is never
started. Exact administrator policy comparison/restoration, new-FIFO-only removal,
new-domain-only XML edits, failed-acknowledgement retention and all earlier
preservation checks remain unchanged. Dummy state is never a valid TPM/NVRAM or
capture/restore/boot claim. Existing private keys and auxiliary contents are never
read by the recipe. Only the parent performs remote operations.

Use the execution template from recipe 001 with the script name changed to
`auxiliary-inspection-native-003.py`, its actual uploaded SHA256, and the same
frozen revision `490b88cba5bf6e0837e2cb5780e56211c22a7d27`, deployment and binary
pins. `--execute-reviewed --exclusive-policy-window` remains mandatory. Never
reuse an existing output or remove a guest to retry this recipe.

Sixteen pure self-tests passed before native execution, including exact CLI
refusal diagnostics and refusal to overwrite changed or uncertain fixture XML.
The independent source review found the missing pre-redefine check before any
native source-removal phase ran. The parent added it and retained the same
exclusive-writer prerequisite; libvirt still provides no external compare-and-swap.
The FIFO case additionally requires its exact type diagnostic, so a generic
member-budget refusal cannot masquerade as special-file evidence. A separate review checks the fixture against the frozen
product source. Actual execution and preservation outcomes must be recorded in
the release ledger. SNAP-01, SEC-01, SEC-03, UX-01 and UX-03 remain required;
this is only a metadata/authentication prerequisite, with TUI and all complete
recovery qualification separately pending.
