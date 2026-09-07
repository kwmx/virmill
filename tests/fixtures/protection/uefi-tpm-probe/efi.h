/* Minimal ABI declarations for this original x86_64 UEFI application.
 * Layouts: UEFI 2.10 system/boot/runtime tables and TCG EFI protocol rev 1.3.
 * This is not a general-purpose EFI SDK. */
#ifndef VIRMILL_PROBE_EFI_H
#define VIRMILL_PROBE_EFI_H
#include <stddef.h>
#include <stdint.h>
#define EFIAPI __attribute__((ms_abi))
typedef uint64_t EFI_STATUS;
typedef uint16_t CHAR16;
typedef struct { uint32_t a; uint16_t b, c; uint8_t d[8]; } EFI_GUID;
typedef struct { uint64_t signature; uint32_t revision, size, crc32, reserved; } EFI_HEADER;
#define EFI_ERROR(n) (UINT64_C(0x8000000000000000) | (n))
#define EFI_NOT_FOUND EFI_ERROR(14)
#define EFI_DEVICE_ERROR EFI_ERROR(7)
#define PROBE_VARIABLE_ATTRIBUTES 3u /* NON_VOLATILE | BOOTSERVICE_ACCESS */

struct EFI_TEXT;
typedef EFI_STATUS (EFIAPI *EFI_OUTPUT)(struct EFI_TEXT *, const CHAR16 *);
struct EFI_TEXT { void *reset; EFI_OUTPUT output; };
typedef EFI_STATUS (EFIAPI *EFI_LOCATE)(EFI_GUID *, void *, void **);
typedef EFI_STATUS (EFIAPI *EFI_STALL)(uint64_t);
struct EFI_BOOT {
    EFI_HEADER header;
    void *before_stall[28];
    EFI_STALL stall;
    void *before_locate[8];
    EFI_LOCATE locate;
};
typedef EFI_STATUS (EFIAPI *EFI_GET_VARIABLE)(CHAR16 *, EFI_GUID *, uint32_t *, uint64_t *, void *);
typedef EFI_STATUS (EFIAPI *EFI_SET_VARIABLE)(CHAR16 *, EFI_GUID *, uint32_t, uint64_t, void *);
typedef void (EFIAPI *EFI_RESET)(uint32_t, EFI_STATUS, uint64_t, void *);
struct EFI_RUNTIME {
    EFI_HEADER header;
    void *before_get_variable[6];
    EFI_GET_VARIABLE get_variable;
    void *get_next_variable_name;
    EFI_SET_VARIABLE set_variable;
    void *get_next_high_monotonic_count;
    EFI_RESET reset;
};
struct EFI_SYSTEM {
    EFI_HEADER header;
    CHAR16 *vendor;
    uint32_t firmware_revision;
    void *console_in_handle, *console_in, *console_out_handle;
    struct EFI_TEXT *console_out;
    void *standard_error_handle, *standard_error;
    struct EFI_RUNTIME *runtime;
    struct EFI_BOOT *boot;
};
struct EFI_TCG2;
typedef EFI_STATUS (EFIAPI *EFI_SUBMIT)(struct EFI_TCG2 *, uint32_t, uint8_t *, uint32_t, uint8_t *);
struct EFI_TCG2 { void *get_capability, *get_event_log, *hash_log_extend; EFI_SUBMIT submit; };

_Static_assert(sizeof(void *) == 8 && sizeof(EFI_GUID) == 16, "x86_64 EFI ABI required");
_Static_assert(offsetof(struct EFI_SYSTEM, runtime) == 88, "runtime table offset");
_Static_assert(offsetof(struct EFI_SYSTEM, boot) == 96, "boot table offset");
_Static_assert(offsetof(struct EFI_BOOT, stall) == 248, "Stall offset");
_Static_assert(offsetof(struct EFI_BOOT, locate) == 320, "LocateProtocol offset");
_Static_assert(offsetof(struct EFI_RUNTIME, get_variable) == 72, "GetVariable offset");
_Static_assert(offsetof(struct EFI_RUNTIME, set_variable) == 88, "SetVariable offset");
_Static_assert(offsetof(struct EFI_RUNTIME, reset) == 104, "ResetSystem offset");
_Static_assert(offsetof(struct EFI_TCG2, submit) == 24, "SubmitCommand offset");
#endif
