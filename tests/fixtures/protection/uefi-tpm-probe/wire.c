#include "wire.h"

/* Fixed, public, non-secret test marker; deliberately not a cryptographic identity. */
const uint8_t probe_marker[PROBE_MARKER_SIZE] = {
    'V','I','R','M','I','L','L','-','C','O','L','D','-','T','P','M',
    '-','N','V','-','P','R','O','B','E','-','V','1','-','0','0','1'
};

static uint16_t get16(const uint8_t *p) { return ((uint16_t)p[0] << 8) | p[1]; }
static uint32_t get32(const uint8_t *p) {
    return ((uint32_t)p[0] << 24) | ((uint32_t)p[1] << 16) | ((uint32_t)p[2] << 8) | p[3];
}
static void put16(uint8_t *p, uint16_t n) { p[0] = n >> 8; p[1] = n; }
static void put32(uint8_t *p, uint32_t n) {
    p[0] = n >> 24; p[1] = n >> 16; p[2] = n >> 8; p[3] = n;
}
int probe_equal(const uint8_t *a, const uint8_t *b, size_t size) {
    for (size_t i = 0; i < size; ++i) if (a[i] != b[i]) return 0;
    return 1;
}
static size_t password(uint8_t *p) {
    put32(p, 9); /* authorizationSize: handle + empty nonce + attributes + empty password */
    put32(p + 4, UINT32_C(0x40000009)); /* TPM_RS_PW */
    put16(p + 8, 0);
    p[10] = 0;
    put16(p + 11, 0);
    return 13;
}
size_t probe_command(enum probe_command command, uint8_t out[PROBE_BUFFER_SIZE]) {
    static const uint32_t codes[] = {0x169, 0x12a, 0x137, 0x14e};
    if ((unsigned)command >= sizeof(codes) / sizeof(codes[0])) return 0;
    for (size_t i = 0; i < PROBE_BUFFER_SIZE; ++i) out[i] = 0;
    put16(out, command == PROBE_READ_PUBLIC ? TPM_NO_SESSIONS : TPM_SESSIONS);
    put32(out + 6, codes[command]);
    size_t n = 10;
    if (command == PROBE_DEFINE) {
        put32(out + n, UINT32_C(0x40000001)); n += 4; /* owner hierarchy, empty auth */
        n += password(out + n);
        put16(out + n, 0); n += 2; /* index authValue is empty */
        put16(out + n, 14); n += 2; /* TPMS_NV_PUBLIC size */
        put32(out + n, PROBE_NV_INDEX); n += 4;
        put16(out + n, 0x000b); n += 2; /* TPM_ALG_SHA256 */
        put32(out + n, PROBE_NV_ATTRIBUTES); n += 4;
        put16(out + n, 0); n += 2; /* empty authPolicy */
        put16(out + n, PROBE_MARKER_SIZE); n += 2;
    } else {
        put32(out + n, PROBE_NV_INDEX); n += 4;
        if (command != PROBE_READ_PUBLIC) {
            put32(out + n, PROBE_NV_INDEX); n += 4; /* authHandle is the index itself */
            n += password(out + n);
            put16(out + n, PROBE_MARKER_SIZE); n += 2;
            if (command == PROBE_WRITE) {
                for (size_t i = 0; i < PROBE_MARKER_SIZE; ++i) out[n++] = probe_marker[i];
            }
            put16(out + n, 0); n += 2; /* data offset */
        }
    }
    put32(out + 2, (uint32_t)n);
    return n;
}

int probe_response(const uint8_t *bytes, size_t capacity, struct probe_response *out) {
    if (!bytes || !out || capacity < 10 || capacity > PROBE_BUFFER_SIZE) return 0;
    uint32_t size = get32(bytes + 2);
    uint16_t tag = get16(bytes);
    uint32_t code = get32(bytes + 6);
    if (size < 10 || size > capacity || (tag != TPM_NO_SESSIONS && tag != TPM_SESSIONS)) return 0;
    if (code != 0 && (size != 10 || tag != TPM_NO_SESSIONS)) return 0;
    out->bytes = bytes; out->size = size; out->tag = tag; out->code = code;
    return 1;
}

int probe_public(const struct probe_response *r, int require_written) {
    /* Exact empty-policy SHA-256 NV public structure and 34-byte Name. */
    if (!r || r->code || r->tag != TPM_NO_SESSIONS || r->size != 62) return 0;
    const uint8_t *p = r->bytes;
    uint32_t attributes = get32(p + 18);
    return get16(p + 10) == 14 && get32(p + 12) == PROBE_NV_INDEX &&
        get16(p + 16) == 0x000b &&
        (attributes & ~PROBE_NV_WRITTEN) == PROBE_NV_ATTRIBUTES &&
        (!require_written || (attributes & PROBE_NV_WRITTEN)) &&
        get16(p + 22) == 0 && get16(p + 24) == PROBE_MARKER_SIZE &&
        get16(p + 26) == 34 && get16(p + 28) == 0x000b;
}

int probe_session_result(const struct probe_response *r, int with_marker) {
    uint32_t parameter_size = with_marker ? 2 + PROBE_MARKER_SIZE : 0;
    if (!r || r->code || r->tag != TPM_SESSIONS || r->size != 19 + parameter_size) return 0;
    if (get32(r->bytes + 10) != parameter_size) return 0;
    const uint8_t *auth = r->bytes + 14 + parameter_size;
    /* TPM_RS_PW response: empty nonce, continueSession set, empty HMAC. */
    if (get16(auth) != 0 || auth[2] != 1 || get16(auth + 3) != 0) return 0;
    return !with_marker || (get16(r->bytes + 14) == PROBE_MARKER_SIZE &&
        probe_equal(r->bytes + 16, probe_marker, PROBE_MARKER_SIZE));
}
