# Complete cold capture and new restore recipe

`cold_capture_native.py` runs only on the owner-authorized disposable
`<test-vm-login>` host as its ordinary UID1000 account. Supply a new private run
root containing exact `bin/virmill`, `bin/virmilld` and their SHA256 mapping in
`binaries.json`. Required arguments are `--execute-disposable`, `--root`,
`--source-vm`, `--source-root` and `--pool`.

Use the retained two-disk BIOS marker fixture created by
`tests/fixtures/import/multidisk-probe`; both files must belong to that source
UUID in the selected directory pool. The recipe hashes every earlier stopped
definition and both source disks. It temporarily grants read ACL entries to the
ordinary actor, restores the exact original ACLs before clone boot, and verifies
all originals again on exit. These temporary grants are test setup, not evidence
for the separate product storage-access workflow.

The tested product path uses a private ordinary coordinator, CLI reviewed capture,
all-member catalog verification, CLI reviewed restore into new independent
volumes, definition last, and a separate reviewed start. Only the new restored VM
is force-stopped during cleanup; the synthetic marker has no OS shutdown agent.
The new definition, disks, catalog and journals are retained. Original guests are
never redefined or started. The source images are never extracted, rewritten or
deleted.

The script records native running state and a screenshot but deliberately leaves
`bootMarkerVerified` false. Independently inspect the actual screenshot for
`VIRMILL SECOND DISK PASS` and record that observation separately. A matching
marker demonstrates this generated BIOS disk pair; it does not validate UEFI,
TPM, encrypted repository backup, independent-host recovery or all supported OSs.
