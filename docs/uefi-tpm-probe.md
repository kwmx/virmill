# Disposable UEFI / TPM cold-state probe

This original tiny x86_64 EFI application is a fixture for the firmware/TPM portion
of SNAP-01 and BAK-01. It writes a public marker into a newly created disposable
guest's emulated TPM and a private UEFI variable, then distinguishes preserved
state from first-time seeding and missing auxiliary state on later boots.

**Boot this image only in an explicitly created disposable QEMU/KVM guest with
fresh, private OVMF variable storage and a private swtpm TPM 2.0 instance. Never
boot it on host firmware, pass through a host TPM, or attach it to an unrelated
guest.** The application checks the CPUID hypervisor flag and QEMU TCG/KVM vendor
before guest COM1 port I/O, locating the TPM or reading/writing UEFI variables. This is an accident
guard, not authorization or protection against a spoofed virtual machine.

There is no host TPM API, `/dev/tpm*` access, efivarfs access, package installation,
network operation, or guest launcher in the build recipe. Only a synthetic wire
test executable runs locally. The EFI application has not been booted by this
fixture-building task; the parent owns disposable-host execution and qualification.

## Marker and behavior

| Item | Value |
| --- | --- |
| TPM NV index | `0x01564d50` |
| Public marker, exactly 32 bytes | `VIRMILL-COLD-TPM-NV-PROBE-V1-001` |
| NV name algorithm | SHA-256 (`0x000b`) |
| Initial NV attributes | `AUTHWRITE | AUTHREAD | NO_DA`, `0x02040004` |
| NV authorization | Empty password; public test data only |
| Define authorization | Empty owner-hierarchy password, suitable only for the fresh disposable emulator |
| UEFI variable | `VirmillColdProbe` |
| Variable vendor GUID | `e410eb9a-7533-4bd5-a02c-a79bcea88414` |
| Variable attributes | Nonvolatile and boot-service access (`3`); no OS runtime exposure |

The application first reads both markers. It does not clear the TPM, change a
hierarchy password, undefine an index, replace mismatched data, or repair partial
state automatically.

| Initial observation | Behavior / final console result |
| --- | --- |
| Both absent | Define the NV index, write the marker, set the UEFI variable, read both back, then `RESULT=SEEDED`. |
| Both present with exact expected metadata and bytes | Read-only probe result `RESULT=PRESERVED`. |
| Exactly one absent | `RESULT=FAIL_AUXILIARY_STATE_MISMATCH`, before any marker mutation. |
| Existing unwritten index, unexpected metadata, different marker, unavailable TPM, authorization failure, or malformed response | `FAILURE_STAGE=... STATUS=0x...`, followed by `RESULT=FAIL`. |
| Non-QEMU/KVM execution environment | `RESULT=REFUSED_GUEST_GUARD NO_TPM_OR_NVRAM_ACCESS`. |

Successful seeding is **not restore success**. If both auxiliary-state members were
lost, the same image will report `SEEDED`, which must fail a restore run expecting
`PRESERVED`. A failure between defining the index and setting the UEFI variable
leaves visible partial state; use a new explicitly disposable fixture rather than
changing the probe to silently erase it.

The marker is intentionally fixed and public. It demonstrates persistence of these
selected bytes, not an endorsement-key identity proof, anti-cloning, rollback
protection, PCR/sealing policy preservation, or an encryption-key recovery test.
The parser checks the NV public metadata and Name framing but does not recompute
the Name digest. Linux/Windows readiness, BitLocker/LUKS recovery, disk/config
completeness, restic encryption, removal of original files/database, and the full
acceptance scenario require separate evidence.

## Building without downloads

The source is in `tests/fixtures/protection/uefi-tpm-probe/`. Build into an absent
output directory:

```sh
/usr/bin/python3 -B tests/fixtures/protection/uefi-tpm-probe/build.py \
  --out build/uefi-probe-reviewed-serial-final-a
```

The recipe compiles native synthetic byte tests with GCC's undefined-behavior trap
instrumentation, runs only those tests, compiles freestanding EFI code with the
Microsoft x64 ABI at firmware entry/call boundaries, and uses GNU ld's `i386pep`
backend to produce PE32+. It does not need GNU-EFI, clang/lld, an SDK download, or
an OS inside the guest. GCC-generated code remains position independent except for
one deliberately relocatable 64-bit marker pointer.

The recipe verifies the EFI subsystem, architecture, zero timestamp, absence of
imported functions, and DIR64 base relocations. It also rejects ELF GOT relocations
and linked RIP-relative indirect branches: this fixture calls its local functions
directly and uses register-based protocol pointers for firmware calls. In
particular, `-fno-plt` is forbidden here because GNU PE linking does not preserve
the required ELF GOT call semantics. It creates a new 32 MiB regular-file
FAT16 image containing `EFI/BOOT/BOOTX64.EFI`, fixes volume ID/timestamps, and reads
the executable back through mtools for a byte comparison. It never mounts an image
or formats a device. Existing output directories and paths inside the source
fixture are refused. Generated outputs belong under the repository's ignored
`build/` tree; source and observed tool versions remain reviewable.

Outputs are `BOOTX64.EFI`, `probe-fat.img`, and `build-manifest.json`. The manifest
records actual compiler/linker/image-tool paths and versions, source/artifact
SHA-256 hashes, PE checks, native instrumentation, and `guestExecuted: false`.
The EFI file and image are made read-only after generation. Build twice in different
new directories and compare those two artifact hashes for reproducibility under
the same toolchain.

Observed local tools on 2026-09-07 were GCC 16.2.1 (Red Hat 16.2.1-2), GNU binutils
2.46.1-1.fc44, dosfstools/mkfs.fat 4.2, mtools 4.0.49, and Python 3.14.7. The exact
observations, including unused available tools and unavailable optional components,
are in `toolchain.local.json`. The optional `--sanitize` mode requires installed
ASan/UBSan runtimes; the local attempt failed because `libasan.so.8.0.0` and
`libubsan.so.1.0.0` are absent. No packages were installed. Default trap-instrumented
byte tests do not need those libraries.

The two builds on 2026-09-07 in `build/uefi-probe-reviewed-serial-final-a/` and
`build/uefi-probe-reviewed-serial-final-b/` produced identical artifact bytes and
manifests. Both passed the synthetic wire tests, ELF/PE checks and FAT readback:

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| `BOOTX64.EFI` | 9,728 | `b50a5ef0a95feb2ee6824b1d5f353b41a4b2ed189c6974c76293592fa3109b33` |
| `probe-fat.img` | 33,554,432 | `1945000ea87359d92f6fcb3413549fe477884012d631c66a058c71a1dda9e483` |
| `build-manifest.json` | 2,348 | `1073902bf2cf0961127f00890ef3d07099fb93b5724380acbd0061373965ea39` |

Static inspection of that EFI image confirmed the Microsoft x64 entry/call stack
alignment and shadow space, firmware service offsets, direct local calls, and one
DIR64 marker-pointer relocation. These checks do not establish a successful
firmware load or native guest run. The earlier generated directories `out-a`
through `out-e`, `out-final-a` and `out-final-b` are retained under
`build/uefi-probe-initial/` for audit. The old `out-final-a` object and EFI image
are rejected by the new GOT/call-target checks; those pre-correction images must
not be used. No generated image or object remains in the fixture source tree.

The portable compile-only regression rebuilds an object with the former unsafe
`-fno-plt` flag and requires the ELF GOT check to reject it; a second object with
the reviewed flags must pass. It never executes either object or links an EFI
image. A retained old executable can also be checked with `--old-efi <path>`:

```sh
/usr/bin/python3 -B tests/fixtures/protection/uefi-tpm-probe/test_build.py \
  --out build/uefi-probe-reviewed-call-regression
```

## Authorized disposable-guest handoff

Use reviewed OVMF code plus a **new private writable** variables file, a newly
created swtpm 2.0 state directory, and a named disposable QEMU/KVM x86_64 guest.
Use the unsigned fixture only with the guest's explicitly reviewed Secure Boot
configuration; do not change a host policy to load it. Attach a private copy of the
FAT image as a raw boot disk, preferably read-only. No guest network or host TPM
passthrough is needed. Preserve the original source image and unrelated guests.

Configure the reviewed guest with one emulated `isa-serial` COM1 device at fixed
I/O base `0x3f8`, connected to a **fresh private regular-file** QEMU chardev log.
The relevant argument fragments are `-chardev file,id=probe_log,path=<fresh-log>`
and `-device isa-serial,chardev=probe_log,index=0`; integrate them into the reviewed
guest definition without adding a second COM1. Use a new log path on every run
because the file backend can overwrite its target. No host serial-device backend
or guest input is required. This is a handoff description, not a guest launcher.

After the guest guard and required EFI service checks, the probe configures only
COM1 (`0x3f8`–`0x3fd`) to 115200 baud, 8 data bits, no parity, one stop bit, with
interrupts disabled. It continues `ConOut.OutputString` and mirrors complete text
lines directly to that guest UART. Firmware serial routing may duplicate complete
lines. The mirror uses a 192-byte line buffer, at most 4,096 output bytes and
200,000 line-status reads for the entire boot, including final transmitter drains.
An unmapped port (`0xff`), exhausted budget or overlong line disables the mirror;
EFI console output continues. A complete
`VIRMILL_PROBE SERIAL=COM1_115200_8N1_GUEST_ONLY` line precedes the TPM probe when
initialization succeeds. If initialization fails, only the EFI console reports
`SERIAL=UNAVAILABLE EFI_CONSOLE_ONLY`. A missing or truncated final log result is
missing evidence, even if the guest shuts down.

Each probe run ends with a complete `VIRMILL_PROBE` result line and requests
`EfiResetShutdown` after a bounded UART drain and a one-second display interval.
If guest shutdown is unsupported, the application halts; a powered-off state is
not inferred from a console result alone. Confirm the actual guest and emulator
are stopped before copying any NVRAM or swtpm files.

1. On fresh auxiliary state, retain the `INITIAL_TPM=ABSENT INITIAL_NVRAM=ABSENT`
   and final `RESULT=SEEDED` lines and observe shutdown.
2. Start the same guest again and require `RESULT=PRESERVED`, then observe shutdown.
3. Create the reviewed cold capture, preserving the image, configuration, matching
   NVRAM, and the complete private swtpm state. Copy only while both guest and
   emulator are stopped; retain source members.
4. Restore into another explicitly disposable, isolated definition with only the
   selected restored members. Require `RESULT=PRESERVED`, and record the member
   hashes, source/restored configuration identities, code/firmware/tool versions,
   operation/guest IDs, logs and actual shutdown observations.
5. As separately created negative-control guests, omit only restored TPM state or
   only restored NVRAM and require `FAIL_AUXILIARY_STATE_MISMATCH`. With both replaced
   by fresh state, require `SEEDED` and treat it as failed preservation evidence.

This is a future execution recipe, not evidence that any guest has run. Full
SNAP-01 and BAK-01 remain open until the real workflow requirements are met.

## Primary references

The original ABI declarations follow [UEFI 2.10 boot services](https://uefi.org/specs/UEFI/2.10_A/07_Services_Boot_Services.html)
and [runtime variable/reset services](https://uefi.org/specs/UEFI/2.10/08_Services_Runtime_Services.html).
`SubmitCommand` is defined in [TCG EFI Protocol revision 1.3, section 6.7](https://trustedcomputinggroup.org/wp-content/uploads/EFI-Protocol-Specification-rev13-160330final.pdf);
the protocol GUID and slot order were cross-checked against [TianoCore's protocol declaration](https://github.com/tianocore/edk2/blob/master/MdePkg/Include/Protocol/Tcg2Protocol.h).

Command numbers/layouts and response semantics follow [TCG TPM Library 1.83, Part 3](https://trustedcomputinggroup.org/wp-content/uploads/TPM-2.0-1.83-Part-3-Commands.pdf),
sections 31.3, 31.6, 31.7 and 31.13; constants were cross-checked against
[TianoCore's TPM declarations](https://github.com/tianocore/edk2/blob/master/MdePkg/Include/IndustryStandard/Tpm20.h).
Password response framing follows [TCG TPM Library 1.59, Part 1, tables 12–13](https://trustedcomputinggroup.org/wp-content/uploads/TCG_TPM2_r1p59_Part1_Architecture_11feb20.pdf).
Reproducible timestamp and base relocation switches are documented in the
[GNU linker manual](https://sourceware.org/binutils/docs/ld/Options.html).
The fixed COM1 mapping and UART status/configuration bits were cross-checked
against QEMU's [ISA serial device](https://raw.githubusercontent.com/qemu/qemu/master/hw/char/serial-isa.c)
and [16550A emulation](https://raw.githubusercontent.com/qemu/qemu/master/hw/char/serial.c).
The regular-file console backend is documented in
[QEMU's invocation reference](https://www.qemu.org/docs/master/system/invocation.html).
