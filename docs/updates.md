# Updates

Virmill checks GitHub for newer releases once a day and tells you when one is
available. `virmill update` downloads, verifies and installs it.

## Check

When a newer release exists, the TUI shows it on the Overview and under
Settings, and interactive commands print a line such as:

```text
Virmill 1.0.0-beta.4 is available (you have 1.0.0-beta.3). Run: virmill update
```

Check right away:

```bash
virmill update check
```

`--output json` prints the full result, including the release page and files.

The check is one request to GitHub's releases API. It sends no VM, host or
Virmill data, but GitHub sees your address. If you are on a beta you are offered
newer betas and releases; on a release you are offered releases only.

## Install

```bash
virmill update
```

It shows the new version and its release notes, then asks before continuing.
It downloads the RPM or DEB packages matching how Virmill is installed here,
including the host helper if you have it. Before installing it checks that:

- each file matches the release's `SHA256SUMS`, the SHA-256 digest GitHub
  recorded when it was uploaded, and the size GitHub lists;
- `rpm` or `dpkg-deb` reports the expected package name, version and
  architecture.

It then runs `sudo dnf install` or `sudo apt install`, which asks for your
password, and restarts your Virmill coordinator. Restart any open TUI to use
the new version.

| Option | Effect |
| --- | --- |
| `--download-only` | Download and verify, then print the install command instead of running it. |
| `--yes` | Do not ask before downloading and installing. dnf or apt may still ask for your password. |

The verified packages are kept in `~/.cache/virmill/updates/VERSION`.

`virmill update` refuses to install:

- while a Virmill job is unfinished. Let it finish, or cancel or reconcile it
  under Jobs.
- a Virmill that was not installed from its RPM or DEB package, such as a
  source build.
- a release without a beta suffix. Package versions are defined for betas only
  so far, so install those by hand.

Checksums catch damaged downloads, and the recorded digests catch a checksum
file that does not match the uploads. Neither can detect a tampered release,
because both come from GitHub.

## Turn checks off or on

```bash
virmill update checks off
virmill update checks on
virmill update checks
```

The last command shows the current setting. `VIRMILL_UPDATE_CHECK=0` turns
checks off for one environment and takes priority over the setting. With checks
off, Virmill contacts GitHub only when you run `virmill update check` or
`virmill update`.

Versions up to 1.0.0-beta.3 do not include the updater. Install the first
version that does by hand; see the [beta guide](beta-testing.md).
