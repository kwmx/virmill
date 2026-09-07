/* Host-executed synthetic byte tests. No EFI entrypoint, device, or TPM access. */
#include "wire.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

static size_t unhex(const char *hex, uint8_t *out) {
    size_t count = 0;
    while (*hex) {
        unsigned byte;
        assert(sscanf(hex, "%2x", &byte) == 1);
        out[count++] = (uint8_t)byte;
        hex += 2;
    }
    return count;
}
static void command_vectors(void) {
    uint8_t actual[PROBE_BUFFER_SIZE], expected[PROBE_BUFFER_SIZE];
    /* TPM Part 3 command layouts, independently encoded as fixed byte vectors. */
    const char *vectors[] = {
        "80010000000e0000016901564d50",
        "80020000002d0000012a40000001000000094000000900000000000000000e01564d50000b0204000400000020",
        "8002000000430000013701564d5001564d500000000940000009000000000000205649524d494c4c2d434f4c442d54504d2d4e562d50524f42452d56312d3030310000",
        "8002000000230000014e01564d5001564d500000000940000009000000000000200000"
    };
    for (unsigned i = 0; i < 4; ++i) {
        size_t wanted = unhex(vectors[i], expected);
        size_t size = probe_command((enum probe_command)i, actual);
        assert(size == wanted && memcmp(actual, expected, size) == 0);
    }
    assert(probe_command((enum probe_command)99, actual) == 0);
}
static void response_vectors(void) {
    uint8_t bytes[PROBE_BUFFER_SIZE] = {0}, bad[PROBE_BUFFER_SIZE];
    struct probe_response response;
    size_t size = unhex("80010000000a0000018b", bytes);
    assert(probe_response(bytes, size, &response) && response.code == TPM_NV_MISSING);
    assert(!probe_public(&response, 0) && !probe_session_result(&response, 0));
    /* Public Name is a SHA256-algorithm prefix plus 32 opaque digest bytes. */
    size = unhex("80010000003e00000000000e01564d50000b22040004000000200022000b", bytes);
    memset(bytes + size, 0x5a, 32);
    size += 32;
    assert(size == 62);
    assert(probe_response(bytes, size, &response) && probe_public(&response, 1));
    for (size_t capacity = 0; capacity < size; ++capacity)
        assert(!probe_response(bytes, capacity, &response));
    for (unsigned i = 0; i < 29; ++i) {
        if (i == 3 || i == 4) continue;
        memcpy(bad, bytes, size); bad[i] ^= 0x80;
        if (probe_response(bad, size, &response)) assert(!probe_public(&response, 1));
    }
    memcpy(bad, bytes, size); bad[18] &= (uint8_t)~0x20;
    assert(probe_response(bad, size, &response));
    assert(probe_public(&response, 0) && !probe_public(&response, 1));

    size = unhex("80020000001300000000000000000000010000", bytes);
    assert(probe_response(bytes, size, &response) && probe_session_result(&response, 0));
    bytes[16] = 0;
    assert(probe_response(bytes, size, &response) && !probe_session_result(&response, 0));
    size = unhex("800200000035000000000000002200205649524d494c4c2d434f4c442d54504d2d4e562d50524f42452d56312d3030310000010000", bytes);
    assert(size == 53 && probe_response(bytes, size, &response) && probe_session_result(&response, 1));
    for (size_t capacity = 0; capacity < size; ++capacity)
        assert(!probe_response(bytes, capacity, &response));
    for (size_t i = 0; i < size; ++i) {
        memcpy(bad, bytes, size); bad[i] ^= 0x80;
        if (probe_response(bad, size, &response)) assert(!probe_session_result(&response, 1));
    }
    bytes[2] = 0xff; bytes[3] = 0xff; bytes[4] = 0xff; bytes[5] = 0xff;
    assert(!probe_response(bytes, sizeof(bytes), &response));
    assert(!probe_response(NULL, sizeof(bytes), &response));
    assert(!probe_response(bytes, sizeof(bytes), NULL));
}
int main(void) {
    command_vectors();
    response_vectors();
    puts("PASS: synthetic TPM command vectors and bounded malformed-response parsing; no EFI/TPM execution");
    return 0;
}
