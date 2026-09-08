# Generated swtpm FILE lock probe v2

This is an immutable successor to the executed
[v1 source](swtpm-file-lock-probe.py), SHA256
`ca9bfbd2d9863bc50caa6d6227e4171cea3a985db93e75f7604930d20282e947`.
The only runtime correction is the exact `swtpm: ` prefix in the FILE lock
diagnostic; report identity advances to `swtpm-file-lock-probe-v2`. All original
source-path, process, descriptor, metadata, timeout and output guards remain.

The preserved [native002 report](../../../docs/evidence/logs/swtpm-file-lock-native-002.log)
records v1's refusal-parser failure after three owned children were reaped and
scratch state was removed. Its child 3076470 returned status 1 and this stderr:

```text
swtpm: SWTPM_NVRAM_LinearFile_Lock: Could not lock backend-uri file:///tmp/vml-file-hin1kge5/initialized: Resource temporarily unavailable
```

V2 adds an exact regression using those bytes and rejects missing/changed
prefixes, changed roots/member paths, unknown errno text and appended errno
noise. Matching remains a complete diagnostic line; other bounded diagnostic
lines are retained in the report under the unchanged parser policy. The first
native run's operator-supplied dependency-hash typo and native002's parser failure
remain failed historical runs. Neither is promoted by this source correction.

This standalone fixture investigates upstream swtpm **0.10.2** regular-file
backend locking. It supplements the older directory/mixed-backend experiment;
it does not run guests, libvirt, a TPM device, a Virmill helper or capture code.
The [source review](../../../docs/reviews/swtpm-file-writer-exclusion.md) explains
why file preparation and pathname replacement need separate observations.

Run its pure self-tests without native tools or private IPC:

```text
python3 tests/fixtures/protection/swtpm-file-lock-probe-v2.py --self-test
```

The 23 self-tests explicitly block subprocess creation, Unix socket creation and
kernel `fcntl` calls. Synthetic seams cover source/argument rejection, clean
environment, lock-query shape, unsupported lock errors, exact diagnostics,
partial pipe output, bounds, deadlines, cancellation, descriptor setup failures
and cleanup. Their report has `mode=self-test` and `nativeProbeExecuted=false`.
They do not inspect installed dependencies or initialize TPM state.

Native execution is a separate, parent-operated experiment. It has **not been
run for this fixture**. Its fixed layout requires an ordinary non-root user on
Linux x86_64, Python with Linux OFD-lock constants, private Unix IPC, swtpm 0.10.2
with TPM 2 and regular-file backend support, and the three fixed public files:

```text
/usr/bin/swtpm
/usr/lib64/swtpm/libswtpm_libtpms.so.0.0.0
/usr/lib64/libtpms.so.0.10.2
```

An operator must review their exact SHA256 values and invoke this shape, replacing
the uppercase tokens with those lowercase 64-digit hashes:

```text
python3 tests/fixtures/protection/swtpm-file-lock-probe-v2.py --run-generated-probe \
  --swtpm-sha256 REVIEWED_SWTPM_SHA256 \
  --swtpm-libtpms-sha256 REVIEWED_SWTPM_LIBTPMS_SHA256 \
  --libtpms-sha256 REVIEWED_LIBTPMS_SHA256
```

There is no default native action, dependency download, existing source-path
argument, arbitrary executable option, sudo or remote operation. Hash and
metadata checks precede executing the pinned emulator inode through its held
descriptor. The hashes attest the named public artifacts at observation, not
every library actually loaded by the dynamic linker. Record installed package,
loader, filesystem and kernel provenance separately. A denied IPC preflight is
a failed prerequisite, never a passed or skipped native case.

The four native cases deliberately use only freshly generated scratch state:

| Case | Required observation |
|---|---|
| `initialized-file-guard` | An owned producer holds a POSIX whole-file write lock on the state file itself; an OFD reader conflicts. After that producer exits, the held OFD read guard refuses a second start with the exact FILE lock diagnostic, preserving the tested metadata. |
| `explicit-mode-before-lock` | A conflicting start with `mode=0640` changes a generated file from mode 0600 even though acquisition fails. |
| `short-file-before-lock` | A generated zero-length file grows before the producer reports lock refusal while its OFD guard remains held. |
| `replaced-file-bypasses-old-guard` | Substituting another freshly generated state inode admits a new producer; the original OFD remains held, and held-versus-named identity differs. |

These expected adverse effects are test observations, not permission to change
real state. If the exact effects, diagnostics or prerequisites differ, the
fixture fails visibly. The existing file is never an input: it is seeded by the
fixture's own terminated swtpm process. No state bytes are read, hashed, decoded
or exported by Python. The owned producer necessarily generates and loads its
own disposable state. Only the fixed control capability query is sent; there
are no state-blob, NV, migration, setup or guest commands.

Every run creates its own mode-0700 `/tmp/vml-file-*` tree, independent of ambient
TMPDIR. Children receive only PATH, LC_ALL and that scratch HOME. State paths and
IPC endpoints are fixed generated arguments. The overall work deadline is 75
seconds, per readiness/exit wait is 5 seconds, and native children are capped at
12 (the full case sequence currently uses 9, including the version command).
Each child has an 8 MiB per-file write limit, a 10-second CPU limit, no core dumps
and 64 descriptors maximum. Each child's combined stdout/stderr is limited to
8 KiB; the JSON report is limited to 256 KiB. These bounds do not promise to
interrupt an unresponsive kernel/filesystem operation.

SIGINT/SIGTERM cancel new work. Cleanup signals only the fixture's own Popen
children, first SIGTERM and then SIGKILL after one second, with one further
second to reap each. It never signals a process group or a PID obtained from a
lock query. Generated state is removed only after all owned children have been
reaped and the scratch root identity still matches. An unreaped child causes
failure and retained scratch state for the operator; the report identifies it.
Successful cases have drained bounded diagnostics. Failure-path diagnostics may
be partial because cleanup closes pipes after reaping; no success claim may be
derived from partial output.

The single JSON stdout report includes exact case observations, child identities
and exit codes, bounded base64 diagnostics, dependency hashes, cleanup results
and failures. Preserve it in a new exclusive evidence file, together with this
fixture's SHA256; do not overwrite earlier failures. Native `status=passed`
requires all four cases and successful cleanup. `completeCaptureVerified`,
`guestIdentityVerified`, `hardwareVerified` and `stateBytesReadByProbe` remain
false even then. This fixture does not prove state-content invariance, initialized
guest identity, restart prevention, real-VM mapping, block/shared storage support,
encrypted capture, restore or Virmill's producer-exclusion implementation.
