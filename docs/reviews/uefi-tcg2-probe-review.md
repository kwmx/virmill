# UEFI TCG2 probe ABI review

Reviewed 2026-09-07 against source at repository HEAD
`ad608076cd928e6c6c270e6442cbc2d8047e6e0c` and the source hashes below.
Scope: read-only investigation of the original disposable guest fixture's
`LOCATE_TCG2` failure. No runtime, fixture, image, VM or host configuration was
changed. The parent remains responsible for native investigation and evidence.

**No concrete ABI, LocateProtocol-slot, call-signature or TCG2-GUID defect was
found in the reviewed source.** The observed return is `EFI_NOT_FOUND` from the
protocol lookup. This is consistent with no matching installed protocol at that
call. It does not establish why the protocol was unavailable, nor that Fedora
firmware generally lacks TPM support. No fixture rebuild or runtime correction
is justified by this ABI review alone.

## Captured observation and provenance

The supplied local record is
`.virmill-local/logs/cold-probe-second-boot-001.log`. It reports successful
Virmill start, then three console attempts: stopped, PTY not assigned, and an
attached console with 795 stdout bytes, no stderr, and client exit zero.
The observer correctly classified the guest result as `probe_failed`, with
two identical `FAIL` result lines and:

```text
VIRMILL_PROBE FAILURE_STAGE=LOCATE_TCG2 STATUS=0x800000000000000e
```

The record reports `initial: null`, so it supplies neither SEEDED nor PRESERVED
evidence. It also records subsequent native shut-off, unchanged probe disk,
preserved XML for four earlier guests, and no retained locks. These are facts
from the parent's record, not native checks performed by this reviewer.

| Reviewed input | SHA-256 |
| --- | --- |
| `tests/fixtures/protection/uefi-tpm-probe/efi.h` | `92269ce2a6feb537260e3b12c2f923c6bcfa4c5d21c3cb5bf51c55ad88e93621` |
| `tests/fixtures/protection/uefi-tpm-probe/probe.c` | `ab52310dd807419bbe90b81a377f207881b8a28913a95a5b30430df740b5f1bf` |
| `tests/fixtures/protection/uefi-tpm-probe/build.py` | `616f6a4ff85d811354ad0edaafde1bb0603f80301e09f70a38cf6dc6214114bf` |
| Local second-boot aggregate record | `fb153d903e5b924b334d1063afb4b41eb5f5bc21937a3f2f44bb4685a1b8a79a` |
| Raw console stream, hash reported by that record | `6aee22d698403d3f1e28c953ddc39b3f32b776eb55d94b901ebbc4831498e1c1` |

The raw stream itself was not supplied as a local file for this review. Its
reported hash and parser result must not be confused with an independent
re-hash or re-parse of the remote raw artifact.

## Layout and signature checks

The fixture explicitly targets x86_64: 64-bit status/size/pointer fields,
16-bit `CHAR16`, a 16-byte GUID, normal structure alignment and a 24-byte table
header. Its partial structs preserve the prefixes actually accessed; they are
not complete SDK definitions. Offsets below were independently derived from
the published table member order and compared with `efi.h`'s static assertions.
Sources: [UEFI 2.10 A system and service tables](https://uefi.org/specs/UEFI/2.10_A/04_EFI_System_Table.html),
[EDK2 UefiSpec.h](https://raw.githubusercontent.com/tianocore/edk2/master/MdePkg/Include/Uefi/UefiSpec.h)
and [EDK2 x64 type definitions](https://raw.githubusercontent.com/tianocore/edk2/master/MdePkg/Include/X64/ProcessorBind.h).

| Access | Expected byte offset | Fixture | Assessment |
| --- | ---: | ---: | --- |
| System table console output | 64 | 64 | Matches |
| System table runtime services | 88 | 88 | Matches |
| System table boot services | 96 | 96 | Matches |
| Boot services Stall | 248 | 248 | 24 + 28 × 8 |
| Boot services LocateProtocol | 320 | 320 | 24 + 37 × 8 |
| Runtime GetVariable | 72 | 72 | Matches |
| Runtime SetVariable | 88 | 88 | Matches |
| Runtime ResetSystem | 104 | 104 | Matches |
| TCG2 SubmitCommand | 24 | 24 | Fourth function pointer |

`before_locate[8]` is correct: it skips the eight services after Stall and
before LocateProtocol. The reserved boot-services slot earlier in the table
is included in the 28 slots before Stall. There is no missing revision field
at the beginning of the TCG2 function table.

`EFI_LOCATE` has the required status return and three pointer arguments:
protocol GUID, optional registration, and output interface address. The call
passes `&tcg2_guid`, `NULL`, and `(void **)&tcg2`, respectively. NULL registration
is valid. There is no implicit table/`This` argument for this boot service.
[UEFI 2.10 A §7.3.16](https://uefi.org/specs/UEFI/2.10_A/07_Services_Boot_Services.html#efi-boot-services-locateprotocol)
defines that signature and lookup behavior.

All EFI entry and callback types used here carry `__attribute__((ms_abi))`.
The entry accepts image handle and system-table pointers; the three lookup
arguments use RCX, RDX and R8, with the status returned in RAX. This agrees with
the [UEFI x64 handoff and calling convention](https://uefi.org/specs/UEFI/2.10/02_Overview.html#x64-platforms).
The build uses `-maccumulate-outgoing-args` and `-mno-red-zone`; no assumption
that the host's default System V convention is the EFI convention is needed.

The TCG2 identifier exactly matches
`607f766c-7455-42be-930b-e4d76db2720f`. The fixture's scalar initializer produces
the expected x64 memory bytes `6c767f605574be42930be4d76db2720f`; it does not
copy the textual UUID's byte order into the first three scalar fields.
SubmitCommand follows three pointers and takes a protocol pointer, 32-bit
input length, input byte pointer, 32-bit output capacity and output byte
pointer. Both order and widths match the
[TCG EFI Protocol Specification, revision 00.13, §§6, 6.2 and 6.7](https://trustedcomputinggroup.org/wp-content/uploads/EFI-Protocol-Specification-rev13-160330final.pdf)
and [upstream EDK2 Tcg2Protocol.h](https://raw.githubusercontent.com/tianocore/edk2/master/MdePkg/Include/Protocol/Tcg2Protocol.h).

## Meaning of the failure

The low code is 14 with the x64 error bit set. Upstream maps `EFI_NOT_FOUND`
to `RETURN_NOT_FOUND`, encoded with that error bit and code 14.
[EDK2 UefiBaseType.h](https://raw.githubusercontent.com/tianocore/edk2/master/MdePkg/Include/Uefi/UefiBaseType.h),
[EDK2 Base.h](https://raw.githubusercontent.com/tianocore/edk2/master/MdePkg/Include/Base.h).
With NULL registration, LocateProtocol returns this status when its handle
database has no matching protocol instance.
[UEFI §7.3.16](https://uefi.org/specs/UEFI/2.10_A/07_Services_Boot_Services.html#efi-boot-services-locateprotocol).

The source copies the returned status into `failure("LOCATE_TCG2", status)`.
Because the first condition is the nonzero status, it short-circuits the
later interface/SubmitCommand pointer checks. A successful lookup followed by
a null interface would have printed status zero; that is not this observation.
The source does not reach `nv_present()`, `variable_present()`, TPM submission
or marker writes on this failure path. This says nothing about state an earlier
boot or firmware itself may have changed.

The source and compiler evidence make a wrong slot, GUID or argument convention
an unsupported explanation here. They do not independently verify the exact
deployed PE image or the firmware's internal protocol database. The parent
still needs the actual loader/template identity and build features, native TPM
device exposure and initialization evidence, and the booted image's provenance
to distinguish an unavailable protocol from a deployment or firmware issue.
A configured emulator TPM or a package name alone does not establish an
installed guest EFI TCG2 protocol. No firmware substitution, TPM reset or
additional boot was performed or authorized by this review.

Two non-causal limits remain visible. The source comment calls its TCG reference
“rev 1.3”; the consulted publication is revision **00.13**, dated 2016-03-30.
The fixture also does not call GetCapability, which that publication recommends
for capability discovery. Neither affects a lookup that already returned
EFI_NOT_FOUND; adding capability inspection cannot repair this failed lookup.

## Checks actually run and evidence level

Read-only installed-header/package inspection found no GNU-EFI SDK headers or
startup objects (`gnu-efi` and `gnu-efi-devel` are not installed). An installed
local OVMF package was observed but was not used to infer anything about the
disposable host's selected firmware. Primary documents and EDK2 source were
read through the browser; no SDK or runtime dependency was installed. EDK2
`master` references are a cross-check retrieved on the review date, not a pin
or identification of the native firmware build.

The current compiler is GCC 16.2.1 20260819, Red Hat 16.2.1-2. This command
passed without warnings or artifact generation and checked the existing ABI
static assertions:

```sh
gcc -std=c11 -Wall -Wextra -Werror -O2 -ffreestanding -fno-builtin \
  -fno-stack-protector -fpie -mno-red-zone -maccumulate-outgoing-args \
  -fno-asynchronous-unwind-tables -fno-unwind-tables -fno-ident \
  -fsyntax-only tests/fixtures/protection/uefi-tpm-probe/probe.c
```

Repeating those compilation options with `-S -o -` instead of `-fsyntax-only`
produced assembly only on stdout. Inspection confirmed the lookup instruction
`call *320(%rax)` after loading the boot table from system offset 96, clearing
RDX, placing `&tcg2` in R8 and `&tcg2_guid` in RCX. The generated SubmitCommand
call uses offset 24, the prescribed first four argument registers and the
fifth argument's stack slot. A read-only Python assertion also compared all
GUID scalar fields and little-endian memory bytes with the published value.
Both checks passed. Assembly stdout SHA-256:
`8594120e88becddde3bbd3f39dd083333bd0e65675d95d768c4ebf47c9a5c72f`.

No compiled EFI code, TPM instructions, native console, guest or host mutation
was executed in these checks. This is source/ABI review and compile-only
evidence: SNAP-01 and BAK-01 prerequisites, plus documentation evidence relevant
to REL-03. The observed native probe failure remains a failure. Full cold
capture, firmware/TPM preservation and independent backup recovery remain
unproved by this review. No ADR or runtime change is proposed.
