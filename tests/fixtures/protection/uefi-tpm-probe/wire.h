/* Original bounded TPM wire codec for this disposable-guest fixture. */
#ifndef VIRMILL_PROBE_WIRE_H
#define VIRMILL_PROBE_WIRE_H
#include <stddef.h>
#include <stdint.h>

#define PROBE_NV_INDEX UINT32_C(0x01564d50)
#define PROBE_NV_ATTRIBUTES UINT32_C(0x02040004) /* AUTHWRITE | AUTHREAD | NO_DA */
#define PROBE_NV_WRITTEN UINT32_C(0x20000000)
#define PROBE_MARKER_SIZE 32u
#define PROBE_BUFFER_SIZE 256u
#define TPM_NO_SESSIONS 0x8001u
#define TPM_SESSIONS 0x8002u
#define TPM_NV_MISSING UINT32_C(0x0000018b) /* TPM_RC_HANDLE + handle 1 */

extern const uint8_t probe_marker[PROBE_MARKER_SIZE];
enum probe_command { PROBE_READ_PUBLIC, PROBE_DEFINE, PROBE_WRITE, PROBE_READ };
struct probe_response {
    const uint8_t *bytes;
    uint32_t size;
    uint32_t code;
    uint16_t tag;
};
size_t probe_command(enum probe_command command, uint8_t out[PROBE_BUFFER_SIZE]);
int probe_response(const uint8_t *bytes, size_t capacity, struct probe_response *out);
int probe_public(const struct probe_response *response, int require_written);
int probe_session_result(const struct probe_response *response, int with_marker);
int probe_equal(const uint8_t *a, const uint8_t *b, size_t size);
#endif
