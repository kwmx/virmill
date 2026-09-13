# Repository content policy

Keep source, pinned dependency manifests and vendored source, schemas, fixture
recipes, examples, generated CLI reference/completions and release evidence under
version control. The supplied specification remains preserved for audit.

`.gitignore` excludes local toolchains and builds, development executables,
packages, VM disks and installation media, firmware/TPM runtime files, databases,
logs, common private-key files, local environment files and editor/interpreter
caches. Release logs under `docs/evidence/logs/` are explicitly retained.
Public keys and `.env.example`, `.env.sample` and `.env.template` remain eligible
for review; they must contain no private credentials.

Store host connection details and private test inventory under the ignored
`.virmill-local/` directory. Keep fixture media there, in ignored `images/`, or on
the authorized disposable test host. Commit reproducible fixture recipes and
approved digests instead of guest disks or proprietary source media.

The 2026-09-07 audit checked 1,425 tracked files and reachable history. It found no
tracked build products, VM images, package archives or conventional credential
paths. Checks of binary signatures outside vendor found no executable, disk image,
database or archive payloads. Selected credential signatures found only deliberate
invalid private-key header strings in two NoCloud rejection tests. This was a
targeted audit, not proof that arbitrary secrets cannot exist.

All 45 ignore/retention checks passed, and no already tracked path matched the new
ignore rules. The vendored tree was reproduced offline with the pinned toolchain
and matched all 1,045 files byte-for-byte. Upstream data copied by `go mod vendor`
is retained so ordinary vendoring remains reproducible. No files needed untracking
or history rewriting, and nothing was pushed.

Before staging, inspect `git status --short` and `git diff --cached`. Gitignore does
not remove tracked files or prevent `git add --force`, and evidence logs still
require review for sensitive command output before they are committed.

## Private test-environment values

Test host names and addresses, home directory paths and owner media names must
never be committed. List them in the ignored `.virmill-local/private-values.json`:

```json
{"values": [
  {"match": "HOST.EXAMPLE", "placeholder": "<test-vm-host>", "wholeWord": true},
  {"match": "Owner-Image", "placeholder": "<owner-media-1>", "ignoreCase": true}
]}
```

`wholeWord` leaves longer names containing the value alone; `ignoreCase` also
catches renamed copies. Your home directory is always included.

- `scripts/record-evidence.py` replaces listed values and common credentials in
  command lines and logs before writing them, and counts them in `redactedValues`.
- `scripts/check-private.py` fails when a tracked file contains a listed value or a
  credential: tokens, private keys with key data, or passwords in URLs. `make verify`
  runs it. CI runs it too, but only for credentials, because the list is local.
- Enable the pre-commit hook once per clone with `git config core.hooksPath .githooks`.
  It runs the same check on staged files.
- `scripts/redact-tracked.py --dry-run` lists affected tracked files. With `--id ID`
  it redacts them and appends a ledger record holding each file's before/after SHA-256.

Native fixtures no longer name the test host. A disposable host is designated by
listing its own hostname in `~/.config/virmill-tests/authorized-hosts` on that host;
fixtures refuse to run anywhere else. Fixtures that use a storage pool read its name
from `~/.config/virmill-tests/storage-pool` and refuse when it is missing. Paths are
built from the test user's home directory.

On 2026-09-13 the tracked tree was redacted (`private-values-redaction-001`).
Earlier commits still hold the original values; history was not rewritten.
