# Virmill specification-package validation report

**Date:** 2026-09-06. **Specification revision:** 1.0-draft.2. **Scope:** rebranded documentation, declarative contract aids and a synthetic read-only plugin example. All results below were rerun against the Virmill package. The actual VM-management application has not been implemented or hardware-tested by preparing this package.

| Check performed | Result |
|---|---|
| Draft 2020-12 schemas structurally valid | 8 passed |
| Supplied declarative examples and development manifest | 6 passed |
| Negative schema/semantic cases | 11 rejected as expected |
| Source-reference labels | 31 resolved; this is link/label consistency, not a fresh upstream review |
| Internal Markdown file links | Checked; no missing targets |
| Acceptance registry | All 71 original scenarios preserved unchanged; documented, not executed |
| Schema and example identity update | All schemas/examples match the prior contracts apart from approved identity changes |
| Product identity consistency | CLI, coordinator, helper, paths, schema namespaces and reference-plugin IDs agree |
| Superseded branding/command references | No matches in the delivered specification files |
| Go example unit tests | 4 test functions passed |
| Go example static checks | `go vet ./...` passed |
| Example protocol smoke checks | 13 passed |
| Example native build/execution | Linux/amd64 passed |
| Example macOS ARM64 cross-build | Build passed; not executed on macOS |
| Example Windows x64 cross-build | Build passed; not executed on Windows |
| Uploaded source archive digest | Matched the documented SHA-256; source scripts were not executed |

The local validation environment used `go1.23.2 linux/amd64`. That describes the environment used for the example, not the recommended production toolchain or a statement about current Go releases. The source module declares Go 1.22-compatible language features and uses no external Go modules. The checks used local tooling with automatic Go toolchain/module downloads disabled.

The sample is not a production SDK, signed plugin distribution or sandbox implementation. No real VM, guest image, USB device, network bridge, firmware/TPM capture, host privilege change or backup restore was tested. Cross-building this dependency-free reference plugin does not establish macOS or Windows support for the future application. Those checks remain mandatory application release gates with the appropriate physical/real-backend evidence.

Run `python qa/validate_package.py` from an environment with PyYAML and jsonschema/referencing to repeat contract checks. Build and test the example from `examples/plugins/vm-summary` as described in its README. Recorded results are in `qa/contract-checks.json`, `qa/example-build-tests.json` and `qa/identity-checks.json`. `PACKAGE-CONTENTS.json` enumerates the packaged payload files, excluding itself, with byte lengths and SHA-256 digests.
