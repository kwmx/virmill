# 0055: Update check and one-command install from GitHub releases

Accepted 2026-09-14 by the owner's request for updates from GitHub releases.
The owner chose a notice plus a one-command install, daily checks that are on
by default and can be turned off, and checksum verification without a release
signing key.

Until now Virmill made no outbound network connections. This decision adds one
bounded exception. It does not change the rule that builds and runtime
dependencies are never downloaded.

## Checks

The CLI and TUI check as the ordinary user; the coordinator and the host helper
never connect to the network. A check sends one request to the GitHub releases
API of `kwmx/virmill` with a `virmill/VERSION` user agent. It sends no
inventory, identifiers or other Virmill data, so it is not telemetry, but GitHub
sees the host's address. It runs at most once a day, or six hours after a failed
check, and the result is cached in `$XDG_CACHE_HOME/virmill/update-check.json`.
`virmill update checks off` (saved in `$XDG_CONFIG_HOME/virmill/updates.json`)
or `VIRMILL_UPDATE_CHECK=0` turns it off.

Users of a beta are offered newer betas and releases; users of a release are
offered releases only. Drafts and tags that are not Virmill versions are
ignored. An interactive CLI command prints a one-line notice on standard error
when a newer release is known. The TUI shows it on the Overview and in Settings.
It never downloads or installs, because installing needs a terminal for the
password prompt.

## Install

`virmill update` installs only the package format that owns `/usr/bin/virmill`
(`rpm -qf` or `dpkg-query -S`), and the host helper only when it is installed.
Source builds are refused. Downloads use HTTPS to GitHub's API and download hosts
only, with size limits. Each file must match the release's `SHA256SUMS` and the
size GitHub reports, and must name itself as the expected package, version and
architecture (`rpm -qp` or `dpkg-deb --show`). Files are kept in
`$XDG_CACHE_HOME/virmill/updates/VERSION` with private permissions.

The install asks first, unless `--yes` is given, and refuses while any job is
unfinished. It runs `sudo dnf install` or `sudo apt install` with the verified
paths as arguments, never through a shell, then
`systemctl --user try-restart virmilld.service`. `--download-only` stops after
verification and prints the command.

## Limits

Checksums from the same release catch damaged downloads, not a tampered
release or a compromised GitHub account. A release signing key remains possible
later without changing the commands. Package versions are defined for betas
only (`scripts/package.py`), so a future release without a beta suffix is
reported but must be installed by hand. Installations of 1.0.0-beta.3 and older
have no updater and are updated manually once. An update is an RPM or DEB
upgrade; it does not replace REL-01 upgrade evidence.
