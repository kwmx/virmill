# Disposable native reboot fixture

`reboot_native.py` runs only as an ordinary user on an explicitly authorized
disposable libvirt host with passwordless sudo. It is not a development-host test.
Prepare an exclusive directory under `~/virmill-tests/` containing `bin/virmill`,
`bin/virmilld`, and `binaries.json` mapping those two names to the frozen SHA-256
hashes. Pass its absolute path with `--root`, the selected stopped BIOS/QCOW2
fixture UUID with `--source-vm`, and the explicit `--execute-disposable` switch.

The recipe requires every existing guest to be stopped. It hashes their public
persistent XML and the source disk, copies that disk into a new UUID-specific
file, and defines a separate guest without NICs or source metadata. Its private
daemon/state directories do not reuse an existing coordinator journal. It starts
the copy through Virmill, allows 45 seconds for boot, and performs exactly one
reviewed reboot. A pass requires the native event receipt, preserved source bytes
and prior definitions, and a stopped test guest at cleanup. No guest readiness
or full lifecycle claim follows from an event alone.

Cleanup first requests graceful shutdown and may hard-stop only this newly
generated UUID after 60 seconds; it never retries an uncertain reboot. The new
definition, copied disk, isolated journal, exact command outputs and receipt stay
on the disposable host for inspection. Failures and partial copies are retained.
The parent coordinator is the sole remote mutation owner for this fixture.
