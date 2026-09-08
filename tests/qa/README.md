# Supplied specification QA environment

`spec-python-requirements.lock` records the eight exact wheels used with observed
CPython 3.14.7 on Linux x86-64. Compiled-wheel hashes are specific to that ABI;
another platform needs its own reviewed lock and execution evidence. These are
build-only validator dependencies, not Virmill runtime dependencies.

The [recorded environment](../../docs/evidence/environments/specification-python-20260908.json)
and [review](../../docs/reviews/specification-python-validator.md) include exact
versions, Python/pip bootstrap, wheel hashes, official provenance and limitations.
No downloaded wheel, venv, media or runtime state belongs in the source tree.

For an explicitly requested network preparation, use an isolated environment and
download only the hash-pinned binary wheels from official PyPI. Subsequent
installation is offline. The following replay commands have not themselves been
executed; the review records the actual equivalent setup commands.

```sh
/usr/bin/python3 -B -m venv build/spec-qa-replay/venv
build/spec-qa-replay/venv/bin/python -I -B -m pip \
  --isolated --no-cache-dir --disable-pip-version-check download \
  --index-url https://pypi.org/simple --only-binary=:all: --require-hashes \
  --dest build/spec-qa-replay/wheels -r tests/qa/spec-python-requirements.lock
build/spec-qa-replay/venv/bin/python -I -B -m pip \
  --isolated --no-cache-dir --disable-pip-version-check install \
  --no-index --find-links build/spec-qa-replay/wheels --require-hashes --no-compile \
  -r tests/qa/spec-python-requirements.lock
build/spec-qa-replay/venv/bin/python -I -B -m pip \
  --isolated --no-cache-dir --disable-pip-version-check check
build/spec-qa-replay/venv/bin/python -I -B virmill-v1-spec/qa/validate_package.py
```

For the preserved supplied package, the final command returns nonzero because
`README.md` links to the absent `START-HERE-AGENT-PROMPT.md`, as recorded in
[ADR 0001](../../docs/adr/0001-intake-and-precedence.md). Do not fabricate that
input, suppress the assertion or report the validator as passed. This environment
also includes the optional date-time checker so format assertions actually run.
