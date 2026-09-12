# Starting Virmill's background service

Run Virmill as your normal user. The CLI and TUI connect to `virmilld`, which
coordinates work and keeps its durable job journal. If that connection is
unavailable, existing VMs may still be running; the error does not establish
their current state.

For a packaged installation, start the user service:

```sh
systemctl --user start virmilld.service
```

Then choose **Retry connection** in Overview or Settings, or press `r`. The
recovery card remains available without a working coordinator. **More help**
shows startup and troubleshooting instructions. These controls only retry reads
or show help; they do not start or enable services.

To start the coordinator automatically at future sign-ins, optionally run:

```sh
systemctl --user enable virmilld.service
```

This enables the user service at sign-in. It does not enable guest autostart or
keep the user's service manager running after logout. If startup fails, inspect
the service's diagnostic output:

```sh
systemctl --user status virmilld.service
```

For a source build without the installed systemd unit, run `virmilld` in another
terminal as the same ordinary user, using the same XDG configuration and runtime
environment as the CLI/TUI. A connection failure can also mean a different
runtime path or socket permissions; it does not by itself mean automatic startup
is disabled.
