# Development packaging and reproducibility

`make build` uses the exact local toolchain plus vendored dependencies, disables
network/module resolution, strips build paths, clears Go build IDs and supplies
revision/build-time metadata. `SOURCE_DATE_EPOCH` defaults to 0 for deterministic
local snapshots. Reproduce under the recorded native compiler/libc combination;
a different native toolchain is not claimed byte-identical. No static binary claim.

`make packages` creates separate core and helper RPM/DEB files under `dist/`, plus
SHA-256 metadata. Version is `0.0.0` development; the artifacts are unsigned and
not release-qualified. Building a DEB on Fedora does not establish Debian/Ubuntu
installation compatibility. No package is uploaded, signed or installed by the build.

The helper package installs no authorization policy and its socket defaults to root
access only. The example grants nothing. The implemented helper action prepares
one UUID-named directory; network/auxiliary-state/labeling/watchdog adapters and
normal authorization issuance are unfinished, so the helper is not operationally
ready for production use. Service files never enable lingering or change host bridges.

The stage installer accepts an explicit `--stage build/package-stage/virmill`
and `--destdir /absolute/disposable/root`. `plan` prints files; `install` refuses
existing files; `uninstall` removes only unchanged manifest files. It does not delete
VMs, images, backups, user state or host networking. Native package-manager upgrade
and clean-host migration testing remain required before actual distribution.

`make release-check` deliberately fails while required acceptance evidence or
mandatory code is missing. Do not weaken it to package a smaller 1.0.
