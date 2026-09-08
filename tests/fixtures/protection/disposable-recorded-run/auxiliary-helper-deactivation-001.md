# Auxiliary helper deactivation 001

Parent-only, single-use restoration on the already authorized disposable VM.
The two installed helper units were inactive before parent qualification and
were started in `auxiliary-helper-activation-native-001`. This recipe checks that
ownership receipt, exact running helper PID/start time/executable and successful
positive policy-window report before stopping only those two units together.
It never starts or stops a guest, changes policy, removes artifacts, reads state
payloads, retries a stop or restarts a service after an uncertain result.

The recipe reuses hash-pinned native003 and policy-window001 definitions. Current
policy must match the positive wrapper's restored generation and original public
bytes/access metadata. All nine stopped persistent XMLs, 301 journal rows/schema,
prior helper artifact metadata and generated fixture metadata must match the
positive window's final saved observations. All are checked again after stopping.
The private coordinator must remain active with PID 30897 and its exact executable.

An exclusive mode-0700 `ROOT/auxiliary-helper-deactivation-001` output directory
retains command receipts, before/after observations and a report. Intent and its
directory are fsynced before the only mutation. Unit activity, PID 31125,
start timestamp 51386551107 and policy generation are rechecked immediately before
the fixed `sudo -n /usr/bin/systemctl stop` command. External systemd/policy
writers must remain idle; this is not an atomic compare-and-stop API.

The inherited collector limits each command to 30 seconds and 2 MiB output,
aggregate output to 32 MiB, command count to 2,048 and execution to 20 minutes.
Parent recording uses a 600-second outer timeout. Failed or uncertain completion
retains all evidence and requires parent comparison; there is no blind retry.
The helper service/socket are returned to their prior inactive state. The
existing transient coordinator retains its original bounded runtime deadline.

After reviewing and exclusively uploading the exact source, the parent invokes:

```text
python3 -B -u /ACTUAL/UPLOADED/auxiliary-helper-deactivation-001.py \
  --recipe-sha256 EXACT_REVIEWED_SHA256 --execute-reviewed
```

This supports SEC-01, SEC-03 and REL-02 operational evidence only. It does not
qualify capture, independent restore, hardware, uninstall, or the full release.
