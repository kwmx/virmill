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
