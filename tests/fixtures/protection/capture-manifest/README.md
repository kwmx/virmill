# Synthetic cold declaration fixture

These small text files test declaration and checksum validation only. They are
not QEMU disks, firmware, TPM state, a captured VM, or recoverable guest data.
The deliberately self-declared `independentlyRecoverable: true` must never make
service/CLI/TUI verification report independent recovery or a tested boot.
`nativeVersions` contains labeled synthetic values, not an observed environment.
The empty TPM member exercises the explicit zero-length ordinary-file case.

Tests copy these files into private temporary directories before tampering.
No source path under `/fixture` is opened. The XML is schema-shaped synthetic
configuration; exact native inventory/provenance is a separate requirement.
