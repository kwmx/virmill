# Supplied Python specification validator — 2026-09-08

The unmodified supplied validator **ran and failed** with both the minimal pinned
dependency set and the final format-enabled dependency set:

```text
AssertionError: ['README.md -> START-HERE-AGENT-PROMPT.md']
```

This confirms the known intake discrepancy in
[ADR 0001, item 1](../adr/0001-intake-and-precedence.md): the kickoff document is
listed in the supplied inventory but absent. It is not a newly discovered product
defect or a missing Python dependency. No placeholder file, link exception,
assertion suppression or validator modification was used. This is REL-03
specification/document validation evidence only; no product or hardware acceptance
scenario passed as a result.

## Scope and immutable inputs

The executed source is `virmill-v1-spec/qa/validate_package.py`, SHA256
`82533fbc05f1176dd518f2a3638ea6b11205955a2bee2a562bea379ea3ca8caa`.
Observed repository HEAD was
`490b88cba5bf6e0837e2cb5780e56211c22a7d27`. The surrounding working tree remained
mutable; this execution is bound directly to the supplied specification inputs,
not described as a new frozen product build.

Before and after execution, all 49 existing regular files under the contracted
`virmill-v1-spec/` root were enumerated and hashed. No symlinks were present. The
7517-byte before/after JSON inventories are identical, both SHA256
`87cd721ca194c52c8c6a5710ea71f2813059ecbfd42590c1838812b0376c7e57`.
`git status --short -- virmill-v1-spec` produced no output afterward. The absent
kickoff file was not recreated, and no normative file or package inventory was
edited.

Only ignored `build/spec-qa/` outputs and this review were written. No product
dependency, normative contract, requirements tracker, evidence ledger, release
matrix or host configuration was changed. No SSH, VM, native virtualization,
privileged helper, PTY, device or IPC operation was run. Parent integration of
this failed evidence remains separate.

## Dependency discovery and isolation

The following available Python installations were inspected without installation:

| Executable | Observed version | Relevant discovery |
|---|---|---|
| `/usr/bin/python3` | 3.14.7, GCC 16.1.1 | PyYAML 6.0.3 present; jsonschema/referencing absent |
| `/home/linuxbrew/.linuxbrew/opt/python@3.14/bin/python3.14` | 3.14.6, GCC 13.3.0 | jsonschema/referencing absent |
| Bundled Codex dependency Python | 3.12.14, Clang 22.1.3 | PyYAML/jsonschema/referencing absent |

The bundled runtime was located through the workspace dependency locator, at
`/home/faisal/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3`.
The inspected pip wheel cache had no suitable validator dependency wheels.
`/usr/share/python-wheels/pip-26.0.1-py3-none-any.whl` was available for local venv
bootstrap; its SHA256 is
`e216ea059aa2d576d2c3535cba6257ee71f3657c19aa704e031073884ec691ed`.

The isolated environment is
`/home/faisal/project/virmill/build/spec-qa/venv`, created with
`/usr/bin/python3 -B -m venv build/spec-qa/venv`. Its `pyvenv.cfg` records
`include-system-site-packages = false`; it does not inherit the system PyYAML.
Installation used only wheels and never built a source distribution. The final
validator command uses `-I -B`, ignoring Python environment overrides and writing
no bytecode into the preserved specification.

The first direct PyPI fetch failed under the default sandbox with
`Temporary failure in name resolution`. Its command, empty stdout and traceback
are retained as `fetch-wheels-001.*`. The explicitly authorized network-only
escalation then retrieved exact versions from `https://pypi.org/pypi/NAME/VERSION/json`
and wheels from `https://files.pythonhosted.org/`; redirects were restricted to
those HTTPS hosts. The downloaded length and SHA256 of every wheel were checked
against the official release metadata before installation. Metadata JSON, its
hash, dependency declarations, selected wheel filename, URL and wheel hash are
retained in `metadata/`, `wheels.json` and `format-wheels.json`.

The observed pinned releases are published on official PyPI:
[jsonschema 4.26.0](https://pypi.org/project/jsonschema/4.26.0/),
[referencing 0.37.0](https://pypi.org/project/referencing/0.37.0/) and
[PyYAML 6.0.3](https://pypi.org/project/PyYAML/6.0.3/).
The installed package set, including transitive dependencies, is exact:

| Package | Version | Selected wheel SHA256 |
|---|---|---|
| attrs | 25.4.0 | `adcf7e2a1fb3b36ac48d97835bb6d8ade15b8dcce26aba8bf1d14847b57a3373` |
| jsonschema | 4.26.0 | `d489f15263b8d200f8387e64b4c3a75f06629559fb73deb8fdfb525f2dab50ce` |
| jsonschema-specifications | 2025.9.1 | `98802fee3a11ee76ecaca44429fda8a41bff98b00a0f2838151b113f210cc6fe` |
| PyYAML | 6.0.3 | `c458b6d084f9b935061bc36216e8a69a7e293a2f1e68bf956dcd9e6cbcd143f5` |
| referencing | 0.37.0 | `381329a9f99628c9069361716891d34ad94af76e461dcb0335825aecc7692231` |
| rpds-py | 0.30.0 | `47e77dc9822d3ad616c3d5759ea5631a75e5809d5a28707744ef79d7a1bcfcad` |
| rfc3339-validator | 0.1.4 | `24f6ec1eda14ef823da9e36ec7113124b39c04d50a4d3d3a3c2859577e7791fa` |
| six | 1.17.0 | `4721f391ed90541fddacab5acf947aa0d3dc7d27b2e1e8eda2be8970586c3274` |

The two compiled wheels are CPython 3.14 Linux x86-64 wheels: PyYAML uses the
`manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64` tags; rpds-py uses
`manylinux_2_17_x86_64.manylinux2014_x86_64`. The other six are pure Python wheels.
This lock is not a promise of compatibility with another interpreter ABI or host.

`requirements.lock` retains the original six-package pins/hashes (593 bytes,
SHA256 `477152ca6d206449af0f6443a0ba28fc1e6072221782610d2af61c7ac083490b`).
`requirements-format-enabled.lock` retains all eight pins/hashes (788 bytes,
SHA256 `73012ff1849a94d8b4da435dc892b16d1a815efa466ff0cc328b853410dcf35e`).
There are no floating requirements in these build-only retained configurations.
Both installations used `--no-index --find-links build/spec-qa/wheels
--require-hashes --no-compile`. No runtime/product dependency download was added.

The first pip invocation omitted an explicit `--no-cache-dir` alongside
`--isolated`; pip ignored the environment cache setting and printed a warning
that the existing user cache was not writable, then disabled caching. It still
installed offline successfully. That actual stderr remains retained; subsequent
commands explicitly disable cache and version checking and produce no stderr.

## Actual execution and results

The recorder saves each exact argv, cwd, UTC start/end, exit code and separate
stdout/stderr files with SHA256. The cwd for every recorded command is
`/home/faisal/project/virmill`. Commands had a 240-second subprocess deadline and
retained output stayed below the recorder's 16 MiB check.

| Retained command prefix | Actual action | Result |
|---|---|---|
| `create-venv` | Create only the workspace venv | Exit 0 |
| `fetch-wheels-001` | Default-sandbox exact PyPI fetch | Exit 1, DNS unavailable |
| `fetch-wheels-002` | Authorized exact official wheel fetch | Exit 0 |
| `install-offline` | Hash-required offline six-package installation | Exit 0 |
| `validator-baseline` | Unmodified normative Python validator | Exit 1, known missing link |
| `fetch-format-wheels` | Exact rfc3339-validator/six official wheels | Exit 0 |
| `install-format-offline` | Hash-required final dependency installation | Exit 0 |
| `runtime-final` | Exact runtime/dependencies and available-format probes | Exit 0 |
| `validator-format-enabled` | Unmodified normative Python validator again | Exit 1, same known missing link |
| `dependency-check` | `pip check` in the isolated environment | Exit 0; `No broken requirements found.` |

Both validator runs executed this exact argv, without a wrapper that changes the
validator's behavior:

```text
build/spec-qa/venv/bin/python -I -B virmill-v1-spec/qa/validate_package.py
```

The baseline run finished at `2026-09-08T00:33:23.677299+00:00`; the final run
finished at `2026-09-08T00:34:50.026560+00:00`. Each has zero stdout bytes and the
same 365-byte stderr traceback, SHA256
`a6c5248c44fecd4673f4fad568fa13a1800d1ee0a92e614ec02f4a7f3e7d7507`.
Their command-record SHA256 values are respectively
`d981744f97dce19618400ccce0de05431ecc78e71f8014eac77ea0d2e0519cd8` and
`54a8949e9af8355cc75908d04d38d6d937edc8a32eac51800a50d4ad0034ac4e`.
The failure occurs at the broken-link assertion on line 142. The validator does
not reach its later source-label/requirements assertions or emit its final passed
JSON. Earlier schema/example/negative checks reaching that point do not make the
whole command pass.

The final interpreter is Python
`3.14.7 (main, Aug 10 2026, 00:00:00) [GCC 16.1.1 20260515 (Red Hat 16.1.1-2)]`,
with executable SHA256
`0d64bd6d66d68dac91cdadafc46e22d4f09f22bdc997aaae870f42865b024a3e`.
The observed platform is Linux `7.1.13-200.fc44.x86_64`, x86-64, glibc 2.43; pip is
26.0.1. System timezone data is 2026c, with `/usr/share/zoneinfo/tzdata.zi` SHA256
`c63188e9f5017bb86bf93bcc12613cf4f814f6864119333fae06881b5b603814`.
The 1160-byte `runtime-final.stdout` JSON has SHA256
`125a2a9d21711044b0653bf67542a8987a236d51ba333059cd492386042df81d`.

The only explicit format used by these schemas is `date-time`, in the operation
plan schema. The initial six-package environment's `FormatChecker` did not
register it. jsonschema documents optional format dependencies; constructing a
checker alone does not install them. The additional exact `rfc3339-validator`
and `six` packages enable it. [Official jsonschema PyPI documentation](https://pypi.org/project/jsonschema/4.26.0/)

The final build-only probe verifies that every declared schema format is
registered, accepts `2026-09-08T00:00:00Z`, and rejects `not-a-time` and
`2026-02-31T00:00:00Z`. This environment correction does not change the normative
validator, cure the broken link, or establish Go validator parity. The separate
Go review remains independently owned.

## Offline reproduction and retained artifacts

With the recorded Python/pip bootstrap available, create a fresh environment
under the same allowed build root; do not replace the retained first-run evidence:

```sh
/usr/bin/python3 -B -m venv build/spec-qa/replay-venv
build/spec-qa/replay-venv/bin/python -I -B -m pip \
  --isolated --no-cache-dir --disable-pip-version-check install \
  --no-index --find-links build/spec-qa/wheels --require-hashes --no-compile \
  -r build/spec-qa/requirements-format-enabled.lock
build/spec-qa/replay-venv/bin/python -I -B -m pip \
  --isolated --no-cache-dir --disable-pip-version-check check
build/spec-qa/replay-venv/bin/python -I -B virmill-v1-spec/qa/validate_package.py
```

The final command is expected to return the documented missing-link failure for
these unchanged supplied bytes. These replay commands are a recipe, not an
additional claimed execution. Wheel hashes can be verified from the retained
lock and manifests before reinstalling. The source package must not be rewritten
to make this result green.

Retained outputs are under `build/spec-qa/`: recorder/fetch/runtime scripts,
before/after source inventories, bootstrap/runtime metadata, exact official
metadata responses and wheels, both hashed requirements sets, and all command
records/stdout/stderr. No generated dependency bytes belong in the source
release. The parent may publish selected text evidence after its own review;
this author did not edit the public ledger or acceptance status.
