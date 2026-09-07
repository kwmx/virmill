# Cold source integration and native UEFI probe

This is development evidence for revision `44a46b6222a601661eb993ba905b63d2396a2154`,
source digest `a79ff4e353be11670a90f49d00369c0c1c6094d28bd095147f7b35f4e1863508`.
All 71 full acceptance scenarios remain required and unaccepted. The earlier
automatic approval usage-limit blockage was resolved; scoped escalated checks
and the owner-authorized disposable-host upgrade subsequently ran.

The clean archive passed the full Go race suite with IPC required, vet, portable
builds, two-build binary/RPM/DEB reproducibility, Go plugin SDK tests, and bounded
source-XML and capture-manifest fuzz runs. The packaged artifact suite passed
with private IPC and generated disk tools enabled, including ordinary-user
daemon/CLI/scaffolder execution and confined generated image/OVA/ISO workflows.
Those checks did not mutate local host virtualization or system services.

Actual installed QEMU 10.2.2 file-lock tests passed on generated raw and QCOW2
files: a writer was allowed before the guard, refused while a transferred
descriptor survived, still refused after the sender closed, and allowed after
the final receiver closed. This establishes cooperative file-lock lifetime for
the tested tools, not VM state, host permissions or complete capture authority.
The ledger and individual logs record commands, hashes and remaining opt-in
paths; a successful suite does not qualify skipped hardware workflows.

The disposable Fedora 44 VM received hash-verified development RPMs. All 24
existing job bodies, four stopped guest definitions, and zero resource locks
were preserved across that upgrade. The privileged helper stayed inactive and
SELinux stayed enforcing. This run used the previously recorded libvirt 12.0.0,
QEMU 10.2.2, OVMF 20260812 and swtpm 0.10.2 package versions; it did not update
the host virtualization stack.

A fresh original 32 MiB FAT UEFI fixture was prepared through Virmill and copied
into a new dedicated pool. Creation verified the QCOW2 bytes and defined the
new stopped VM, then returned `RECOVERY_REQUIRED`: libvirt added
`os firmware='efi'` and two disabled firmware-selection declarations to the
explicit non-secure pflash configuration. The original recipe, verified disk
receipt and three locks remain retained. No guest start or auxiliary-state
capture occurred in this run. The failed operation is preserved as evidence.

Read-only comparison isolated that firmware cohort; removing both additions
from the comparison fixture makes the remainder match the reviewed policy.
Production correction and a later native reconciliation need their own revision
and evidence. This observation does not justify accepting arbitrary firmware
attributes or enabled Secure Boot/enrolled keys, and does not prove freshness
of libvirt's first assigned NVRAM path.

Two other defects were identified: the creation estimate reported zero despite
a reviewed 125,829,120-byte pool budget, and the cold inventory misclassified
ordinary `domain/features/acpi` as an external dependency. Their corrections
and CLI/TUI coverage are tracked separately from this frozen runtime.

The new probe remains shut off. An independent read-only hash of its disk
matched the prepared artifact. An existence check of the NVRAM path declared by
native XML returned absent before first boot, but that check follows links and
does not establish path freshness or absence of aliases. The TPM state path
remains unresolved; no auxiliary bytes were read. One observation collector
failed on an incorrect SQL column after capturing XML; the corrected collector
passed without changing guest or Virmill state. Both logs are retained.

A subsequent descriptor-relative directory observation opened every NVRAM
ancestor without following symlinks and checked the final entry without following
links. It also found the entry absent, with root/qemu-owned ancestors and the
same stopped XML and three locks. This stronger point-in-time fixture check still
does not provide a durable creation receipt binding or resolve the TPM source.

The shared CLI/TUI can inspect cold dependencies and check versioned recovery
declarations/member integrity. These do not constitute authenticated capture
provenance. The typed helper capture endpoint, complete durable publication,
encrypted repository workflow and identity-safe independent restore remain
unfinished. Firmware/TPM persistence, full snapshot/backup acceptance, physical
USB and packet-level network isolation claims remain blocked by their specific
missing implementations or evidence.
