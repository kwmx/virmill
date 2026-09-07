# Captured boot/media configuration fixtures

These exact, nonsecure `virsh dumpxml --inactive` snapshots came from the
owner-authorized disposable Fedora 44 host on 2026-09-07. They contain generated
Virmill test identities and publicly described fixture metadata, without guest
disk contents or credentials. The runtime was `8a092b4e234abe118de079a99ac2c9e170d44ece`.

`boot-media-fixture-attached.xml` → `boot-order-applied.xml` records the native TUI
CD-first edit. `boot-order-applied.xml` → `boot-media-ejected.xml` records the CLI
3-vCPU/3072-MiB edit, disk-first order and ejection retaining the empty drive.
`boot-media-external-metadata.xml` → `boot-media-opaque-edited.xml` records an
unrelated 3→4 CPU edit retaining an unknown namespace. A subsequent reviewed 4→3
edit returned the definition byte-for-byte to the former snapshot.

The XML regression test does not run QEMU or validate hardware. Actual ISO-menu
and disk-login observations are recorded separately in the release ledger and
`docs/evidence/boot-media-run.md`.

The `disposable-recorded-run/` programs are the exact stdin sent over SSH for
this run, retained for audit with ledger SHA-256 digests. They include package
replacement, a read-only ISO copy, external CD-ROM fixture attachment and metadata
injection, and reviewed Virmill guest mutations. They are not general installers
or authorization to use other hosts, discovered resources or production storage.

For reproduction, designate a disposable host, pin its dependencies and media
hashes, prepare an independent managed guest, and generate fresh plans and IDs.
Never replay recorded plan digests. External virsh attachment is test preparation,
not qualification of a Virmill disk-attachment workflow. Preserve source media,
other guests and unresolved operations. Capture exact before/after XML and compare
only reviewed changes. Observe screenshots separately; no installer or guest login
was performed in this run.

`stop-boot-media-disk.py` exposed a harness error: its polling loop treated
`verifying` as terminal. The source/log are deliberately retained unchanged.
`check-boot-media-stop-final.py` observed the same successful operation without
replaying it. New harnesses must wait for explicitly terminal states, including
recovery-required as an attention outcome.

The later `upgrade-downtime-estimates.py` and `downtime-preview-tui.py` record
15a1f2c installation and CLI/TUI preview parity. They preserve existing jobs, guest
XML and old plan digests. No new guest mutation was applied in that preview run.
