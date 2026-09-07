# Creation NVRAM destination syntax

The creation XML matcher validates every nonempty observed `domain/os/nvram`
destination with the existing cold-state `coldPath` predicate. Validation uses
raw decoded XML text before any whitespace normalization. A matching nonempty
expected string cannot bypass validation.

The accepted syntax is a canonical absolute POSIX path other than `/`, with at
most 4,096 UTF-8 bytes. Relative paths, `.`/`..` components, doubled or trailing
separators, surrounding whitespace, controls and Unicode format characters are
refused. XML character references and CDATA do not bypass the decoded-text
check; malformed XML, including a literal NUL, is refused by the XML parser.
Spaces inside a legitimate filename remain accepted, including the captured
native probe's `Virmill UEFI TPM cold probe_VARS.fd` name.

Exact empty expected and observed text remains structural equality for an
unresolved NVRAM declaration. It is not an assigned path or an existing/fresh
file. Whitespace-only text is nonempty and invalid. A nonempty expected path
requires exact observed text; changing it to another valid path or empty text
is refused. With the renderer's empty expected destination, another
syntactically valid absolute observed path is still accepted.

The NVRAM comparison also rejects duplicate observed attribute names, including
equal duplicate values, and rejects foreign attributes or namespace declarations
on the NVRAM element. This check is local to `domain/os/nvram`; it does not change
generic attribute matching elsewhere. Existing structural checks continue to
reject duplicate/foreign NVRAM elements and structured or mixed source children.
These refusals apply both with the reviewed firmware-normalization cohort and
without it, so normalization is not required to detect ambiguous NVRAM fields.

This change preserves the existing firmware-normalization policy and does not
render a destination, bind the first assigned path, change receipts or inspect
the filesystem. It cannot establish that the destination exists, is an ordinary
file, has no symlink components, belongs to the operation, or contains fresh
firmware state. Source generation, approved roots, file ownership and lifecycle
readiness need the separate contract described in
[the NVRAM binding review](/home/faisal/project/virmill/docs/reviews/creation-nvram-binding-review.md).
It grants no capture, restore, reset or deletion authority.

Reproduce the focused checks with the pinned toolchain and offline dependencies:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -race -tags libvirt_dlopen ./internal/backend/libvirt -run '^(TestCreationNVRAM.*|TestCreationFirmwareNormalization.*|TestCreationXML.*|TestColdStateXML.*)$' -count=1
```

The new tests exercise the captured native XML and the same configuration
without firmware normalization, valid paths with interior spaces, malformed
decoded text, and comparisons with an already nonempty expected destination.
They also cover duplicate/conflicting attributes, foreign namespaces and
structured source substitutions with and without firmware normalization.
They preserve exact empty structural equality and acceptance of another valid
but unverified path. Existing cold-state and firmware tests remain required.
The simulated native roundtrip uses libvirt's `test:///default` driver only;
it starts no QEMU guest or firmware.

The final focused race suite passed locally. Before the respective corrections,
the new tests reproduced 21 accepted malformed nonempty/whitespace-only path
cases and three accepted duplicate-attribute/namespace-declaration cases.

This is parser/matcher prerequisite evidence for IMP-07, SNAP-01 and JOB-02.
It does not qualify first-path binding, native file safety, first boot, fresh
NVRAM, complete creation or cold recovery.
