# Local restic adapter

`internal/backend/restic` supplies an ordinary-user Linux amd64 process adapter for the system `/usr/bin/restic`. Its zero-value `Tool` implements `Identity`, `Init`, `Backup`, `Observe`, `Restore` and `Check`, with `Repository{Path, PasswordFile}` and typed `Snapshot` results. Other platforms return `UNSUPPORTED_CAPABILITY`. The package adds no Go dependency and performs no runtime download.

This contributes repository prerequisites to BAK-01, BAK-04 and JOB-02. It does not implement retention or scheduling, authorize a job, certify a complete capture, define a VM, prove encryption recovery for a guest, or claim boot success. The coordinator must persist intent and reviewed executable identity, serialize its repository operations, verify the full capture manifest before upload and after extraction, and reconcile uncertain effects. It must not interpret a restic snapshot alone as a committed Virmill backup.

## Fixed command contract

All repository commands receive `--json --quiet --no-cache --repo /proc/self/fd/4 --password-file /proc/self/fd/5`. Arguments are individual `exec.Cmd` values. No caller command, environment, backend option, repository URL, password command, exclusion, unlock, repair, prune or forget override exists.

| API | Fixed command after global flags | Observable result |
| --- | --- | --- |
| `Identity` | `version` without repository flags | Original system path, held executable SHA-256 and exact bounded version line |
| `Init` | `init --repository-version 2` | Zero exit and a valid initialized repository ID |
| `Backup` | `backup --force --tag virmill-operation:UUID -- .` | Exactly one new full snapshot hash matching the operation tag and original absolute source path |
| `Observe` | `snapshots --tag virmill-operation:UUID` | Every exact matching snapshot, sorted by full hash; multiple matches remain explicit |
| `Restore` | `restore --target NEW_PRIVATE_PATH --overwrite never --verify -- FULL_HASH` | Zero exit, no skipped/deleted entries in the summary, ordinary extracted tree |
| `Check` | `check --read-data` | Zero exit and a summary without errors, broken packs or required index repair |

Backup first observes the exact nonzero canonical operation UUID and refuses to write if any matching snapshot already exists. It does not replay an uncertain operation. After successful backup output, it observes again and requires one snapshot matching the summary hash and source path. Missing, contradictory or multiple results return an error with no successful snapshot. Restore first reads metadata for the exact 64-character snapshot hash; prefixes, `latest`, subfolder selectors and snapshots without the exact Virmill operation-tag shape are refused.

The source is the child process's held working directory; the sole backup argument is `.`. The resulting archive contains the capture's members directly, while snapshot metadata retains the original canonical absolute source path. This follows restic's documented relative-path behavior. [Backup paths and tags](https://restic.readthedocs.io/en/stable/040_backup.html)

Version 2 is an explicit repository format, distinct from the installed executable version. The password-file interface and local repository initialization are documented by restic. [Repository preparation](https://restic.readthedocs.io/en/stable/030_preparing_a_new_repo.html)

The adapter checks exact known JSON fields and rejects duplicate keys, case aliases of consumed fields, malformed values and ambiguous result identities. Additional restic statistics can be ignored for forward compatibility. A successful backup requires a final summary and a full snapshot ID; all nonzero exits fail, including exit 3 for incomplete source reads. Exit 11 maps to `RESOURCE_BUSY`, and credential refusal maps to `PERMISSION_DENIED`. [Structured output and exit statuses](https://restic.readthedocs.io/en/stable/075_scripting.html)

The fixed restore verification and overwrite flags were checked against the official 0.19.1 command implementation and restore documentation. [Restore command source](https://github.com/restic/restic/blob/v0.19.1/cmd/restic/cmd_restore.go), [restore semantics](https://restic.readthedocs.io/en/stable/050_restore.html)

## Files, credentials and process lifetime

Paths must be absolute, canonical, nonroot UTF-8 paths without symlinks, dot/doubled/trailing segments, controls, Unicode format controls, backslashes or surrounding whitespace. Repository URLs cannot meet this syntax. Source, repository and restore destination cannot overlap; the credential cannot be inside an uploaded source or repository. A local pathname does not prove that its mounted filesystem is local storage; choosing and qualifying the mounted repository is the caller's responsibility.

Existing repository roots must be owned by the current ordinary user and private 0700 directories. Sources accept private 0500 or 0700 directories, including coldstore's 0400 single-link members. Init repositories and restore destinations must be absent under an existing private writable parent. The adapter creates only that requested new directory, at 0700, and refuses any existing file, link or directory. Failed attempts retain their partial directories and content. It never removes or resets them.

The password must be an ordinary current-user-owned single-link 0600 file, 1–4096 bytes, beneath a private writable 0700 parent. An O_PATH open and complete statx identity/type observation precede its readable reopen through the held inode. The original held read-only descriptor is inherited; password bytes are never placed in arguments or environment variables and are not read into adapter diagnostic strings. Root and password identities/path bindings are checked before and after commands. Source entry generations, metadata and exact membership are compared before and after backup.

The executable must be a single-link ELF file, root-owned and executable, without other-user write or special privilege bits, beneath root-owned non-writable system directory ancestors. It is hashed from a held descriptor and executed through that descriptor, with identity rechecked after execution. Executable size is capped at 256 MiB. A reviewed identity across different API calls remains the coordinator's responsibility; this adapter does not silently treat an upgraded executable as a previously approved identity.

The subprocess environment is replaced by `PATH=/usr/bin`, `LC_ALL=C`, `TZ=UTC`. No ambient RESTIC, proxy, loader or password environment is inherited. Standard input is closed. Each command has a 30-minute ceiling, shortened by its caller's deadline; identity has a five-second ceiling. Cancellation kills the new process group, waits for its worker, and bounds pipe cleanup at two seconds. Parent death requests SIGKILL. These are process lifecycle controls, not guest or parser confinement.

Stdout is capped at 4 MiB; stderr is counted and discarded with a 64 KiB cap. Exceeding either cancels execution and returns no output. Raw process and JSON diagnostics are withheld. Snapshot responses allow at most 4,096 records, one exact operation tag and one canonical source path per snapshot. Tree checks allow at most 100,000 ordinary entries and 128 directory levels, refusing symlinks, special files and file hardlinks. Those limits can refuse a larger repository; they do not silently truncate a result.

This is an ordinary-user trust boundary. The source must already be an immutable, fully verified capture protected by coordinator leases. Metadata rechecks are observations, not an atomic filesystem snapshot against a malicious same-UID writer. Repository, credential and source descriptors retain their selected roots. Restic refuses procfs descriptor links as extraction destinations, so restore uses the newly created canonical private path while retaining inode and parent pins for rechecks. Same-user destination replacement, concurrent subtree mutation and malicious repository archive content are not sandboxed here. System restic is trusted to implement its repository and safe extraction operations. The coordinator still verifies every extracted member, excludes symlinks/special entries and publishes durably before accepting recovery. Restic's repository locking remains enabled; the adapter never bypasses it. The adapter's nil error is not a separate power-loss durability or whole-repository concurrency proof.

## Executed checks and remaining qualification

Local `command -v restic` found no executable. No restic repository was created or opened by the real executable locally. Primary command semantics were checked against the official 0.19.1 documentation/source. The parent separately reported the approved disposable VM's installed output as `restic 0.19.1 compiled with go1.26.5-X:nodwarf5 on linux/amd64`; this report is context, not execution evidence produced by these tests.

Using pinned Go 1.27.1 and vendored dependencies:

```text
./scripts/go test -mod=vendor -race ./internal/backend/restic -count=1
PASS (2.183s); optional TestResticGeneratedRepository skipped without opt-in
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 ./scripts/go build -mod=vendor ./internal/backend/restic
PASS
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 ./scripts/go build -mod=vendor ./internal/backend/restic
PASS
```

Generated-file tests exercise the command seam, exact tags/paths/hash correlation, ambiguous observations, no replay/overwrite, retained partials, changed source/password/root identity, structured corruption, malformed/oversized snapshots and path/type/credential refusal before execution. A real child test executable exercises inherited password/source descriptors, exclusion of poisoned RESTIC environment variables, partial/unknown exit codes, stdout/stderr flood cancellation, and cancellation with worker reaping. Its executable fixture bypasses only system-restic selection by calling the internal process runner directly; it does not relax production ownership rules or claim restic interoperability.

The optional installed-tool test is ready for parent-owned execution on the approved disposable VM:

```text
VIRMILL_TEST_RESTIC=1 ./scripts/go test -mod=vendor -race ./internal/backend/restic -run '^TestResticGeneratedRepository$' -count=1 -v
```

It requires the real system executable, logs its measured identity, and uses only newly generated private temporary source/repository/restore directories. It exercises init, backup, observe, check, restore and refusal of repeated backup/existing restore. Actual encrypted repository interoperability, corruption recovery, full-disk failures, independent manifest recovery, capture service integration and hardware/guest acceptance remain separate evidence.
