# ADR 0058: Remove prepared copies after import

Status: accepted; native evidence recorded separately.

Successful preparation kept its job work folder, with extracted source members
and conversion files, and its published prepared images "until dependency-aware
cleanup is implemented" (ADR 0005). With creation copying the prepared images
into a pool, an OVA import used three to four times its size and large
appliances did not fit. This decision is that cleanup.

`import.discard` is an ordinary reviewed plan and durable job with one
acknowledgement, `delete-prepared-copy`. It names one successful preparation of
the same user. It removes the preparation's work folder and, unless
`--keep-images` (`keepImages`), its prepared images folder. It never touches the
original source files, created VMs or their pool volumes, and never removes the
shared parent folder.

Planning and every step refuse while any job whose plan names the preparation is
running, interrupted, partial or needs recovery. Matching the ID anywhere in a
plan input can only over-refuse. The plan binds the parent folder and each
removed folder by device and inode; folder times are not bound, because other
imports come and go beside them. A replaced or linked folder is refused, and
removal goes through an `os.Root` opened on the verified parent, so it cannot
leave it. Before the prepared images are removed, a durable record marks the
preparation removed. Create VM stops offering it even if removal is interrupted,
and a second removal is refused. Each step reconciles by observing that its
folder is absent.

The TUI's Create VM settings offer "Prepared copy: Remove after creation"
(default) or "Keep". Under the one approval of ADR 0057 the combined review
lists this consequence. After creation, and after start when chosen, the chain
requests removal of exactly the approved preparation and applies it only if
that plan names it and asks for nothing else. Otherwise it stops and the copy is
kept. The CLI offers `virmill import discard OPERATION_ID [--keep-images]`.

Peak space during an import was unchanged ([ADR 0060](0060-one-copy-import.md) later lowers it to about one copy): creation still copies the prepared
images. This contributes to IMP-06 and UX-01 without claiming them.
