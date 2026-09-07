/* Original disposable-QEMU-guest fixture. Never boot on physical firmware.
 * No host TPM API, OS process entry point, OS disk access, or network code. */
#include "efi.h"
#include "wire.h"

static EFI_GUID tcg2_guid = {0x607f766c, 0x7455, 0x42be, {0x93,0x0b,0xe4,0xd7,0x6d,0xb2,0x72,0x0f}};
static EFI_GUID marker_guid = {0xe410eb9a, 0x7533, 0x4bd5, {0xa0,0x2c,0xa7,0x9b,0xce,0xa8,0x84,0x14}};
static CHAR16 marker_name[] = {'V','i','r','m','i','l','l','C','o','l','d','P','r','o','b','e',0};
static struct EFI_SYSTEM *system_table;
static struct EFI_TCG2 *tcg2;
/* A real absolute pointer ensures PE base relocation records are exercised. */
static const uint8_t *volatile marker_pointer = probe_marker;
/* Only the guarded EFI entry enables this fixed, emulated guest COM1 mirror.
 * The native wire test does not link this file or execute any port instruction. */
static int serial_enabled;
static unsigned serial_polls = 200000;
static unsigned serial_bytes = 4096;
static char serial_line[192];
static size_t serial_length;

static void port_write(uint16_t port, uint8_t byte) {
    __asm__ volatile("outb %0, %1" : : "a"(byte), "Nd"(port));
}
static int serial_ready(uint8_t mask) {
    while (serial_enabled && serial_polls) {
        uint8_t status;
        --serial_polls;
        __asm__ volatile("inb %1, %0" : "=a"(status) : "Nd"((uint16_t)0x3fd));
        if (status == 0xff) break; /* Unmapped guest port. */
        if ((status & mask) == mask) return 1;
        __asm__ volatile("pause");
    }
    serial_enabled = 0;
    return 0;
}
static void serial_init(void) {
    serial_enabled = 1;
    if (!serial_ready(0x60)) return; /* Drain any firmware text before setup. */
    port_write(0x3fb, 0x03); /* DLAB clear; 8 data bits, no parity, one stop bit. */
    port_write(0x3f9, 0x00); /* Disable interrupts. */
    port_write(0x3fb, 0x80); /* Divisor latch access. */
    port_write(0x3f8, 0x01); /* 115200 baud. */
    port_write(0x3f9, 0x00);
    port_write(0x3fb, 0x03);
    port_write(0x3fa, 0x07); /* Enable and clear FIFOs. */
    port_write(0x3fc, 0x03); /* DTR and RTS; no interrupt output. */
}
static void serial_text(const char *text, size_t size) {
    for (size_t i = 0; serial_enabled && i < size; ++i) {
        if (serial_length == sizeof(serial_line)) { serial_enabled = 0; return; }
        serial_line[serial_length++] = text[i];
        if (text[i] != '\n') continue;
        if (serial_length > serial_bytes) { serial_enabled = 0; return; }
        /* Mirror whole lines after ConOut completes its line. Firmware serial
         * routing may duplicate a line, but cannot interleave its fragments. */
        for (size_t j = 0; j < serial_length; ++j) {
            if (!serial_ready(0x20)) return;
            port_write(0x3f8, (uint8_t)serial_line[j]);
            --serial_bytes;
        }
        serial_length = 0;
        if (!serial_ready(0x60)) return; /* Bound final drain before shutdown. */
    }
}

static int qemu_guest(void) {
    uint32_t a, b, c, d;
    __asm__ volatile("cpuid" : "=a"(a), "=b"(b), "=c"(c), "=d"(d) : "a"(1), "c"(0));
    if (!(c & (UINT32_C(1) << 31))) return 0;
    __asm__ volatile("cpuid" : "=a"(a), "=b"(b), "=c"(c), "=d"(d) : "a"(0x40000000), "c"(0));
    uint32_t vendor[3] = {b, c, d};
    static const uint8_t kvm[12] = {'K','V','M','K','V','M','K','V','M',0,0,0};
    static const uint8_t tcg[12] = {'T','C','G','T','C','G','T','C','G','T','C','G'};
    return probe_equal((uint8_t *)vendor, kvm, 12) || probe_equal((uint8_t *)vendor, tcg, 12);
}

static void output(const char *text) {
    CHAR16 line[192];
    size_t i = 0;
    while (text[i] && i < sizeof(line) / sizeof(line[0]) - 1) { line[i] = (uint8_t)text[i]; ++i; }
    line[i] = 0;
    if (system_table && system_table->console_out && system_table->console_out->output)
        system_table->console_out->output(system_table->console_out, line);
    serial_text(text, i);
}
static void hex(uint64_t n) {
    static const char digits[] = "0123456789abcdef";
    char out[19] = "0x0000000000000000";
    for (unsigned i = 0; i < 16; ++i) out[17 - i] = digits[(n >> (4 * i)) & 15];
    output(out);
}
static void marker_output(void) {
    char marker[PROBE_MARKER_SIZE + 1];
    for (size_t i = 0; i < PROBE_MARKER_SIZE; ++i) marker[i] = marker_pointer[i];
    marker[PROBE_MARKER_SIZE] = 0;
    output(marker);
}
static __attribute__((noreturn)) void finish(const char *result) {
    output("VIRMILL_PROBE RESULT="); output(result); output(" INDEX="); hex(PROBE_NV_INDEX);
    output(" MARKER="); marker_output(); output("\r\n");
    /* Shutdown only the explicitly supplied guest. Never reboot into another OS. */
    system_table->boot->stall(1000000);
    system_table->runtime->reset(2 /* EfiResetShutdown */, EFI_DEVICE_ERROR, 0, NULL);
    for (;;) __asm__ volatile("hlt");
}
static __attribute__((noreturn)) void failure(const char *stage, uint64_t status) {
    output("VIRMILL_PROBE FAILURE_STAGE="); output(stage); output(" STATUS="); hex(status); output("\r\n");
    finish("FAIL");
}
static struct probe_response submit(enum probe_command command, uint8_t response[PROBE_BUFFER_SIZE]) {
    uint8_t request[PROBE_BUFFER_SIZE];
    for (size_t i = 0; i < PROBE_BUFFER_SIZE; ++i) response[i] = 0;
    size_t size = probe_command(command, request);
    EFI_STATUS status = tcg2->submit(tcg2, (uint32_t)size, request, PROBE_BUFFER_SIZE, response);
    if (status != 0) failure("TCG2_SUBMIT", status);
    struct probe_response parsed;
    if (!probe_response(response, PROBE_BUFFER_SIZE, &parsed)) failure("TPM_RESPONSE_BOUNDS", 0);
    return parsed;
}
static int nv_present(void) {
    uint8_t bytes[PROBE_BUFFER_SIZE];
    struct probe_response response = submit(PROBE_READ_PUBLIC, bytes);
    if (response.code == TPM_NV_MISSING) return 0;
    if (response.code != 0) failure("NV_READ_PUBLIC", response.code);
    if (!probe_public(&response, 1)) failure("NV_PUBLIC_OR_UNWRITTEN_INDEX", 0);
    response = submit(PROBE_READ, bytes);
    if (response.code != 0) failure("NV_READ", response.code);
    if (!probe_session_result(&response, 1)) failure("NV_MARKER_OR_RESPONSE", 0);
    return 1;
}
static int variable_present(void) {
    uint8_t value[PROBE_MARKER_SIZE];
    uint64_t size = sizeof(value);
    uint32_t attributes = 0;
    EFI_STATUS status = system_table->runtime->get_variable(marker_name, &marker_guid, &attributes, &size, value);
    if (status == EFI_NOT_FOUND) return 0;
    if (status != 0) failure("GET_VARIABLE", status);
    if (size != sizeof(value) || attributes != PROBE_VARIABLE_ATTRIBUTES || !probe_equal(value, probe_marker, sizeof(value)))
        failure("NVRAM_MARKER_OR_ATTRIBUTES", 0);
    return 1;
}

EFI_STATUS EFIAPI efi_main(void *image, struct EFI_SYSTEM *table) {
    (void)image;
    /* Reject non-QEMU/KVM firmware before any port I/O, TPM or variable access. */
    if (!table || table->header.signature != UINT64_C(0x5453595320494249)) return EFI_DEVICE_ERROR;
    system_table = table;
    output("VIRMILL_PROBE VERSION=1 DISPOSABLE_GUEST_ONLY\r\n");
    if (!qemu_guest()) {
        output("VIRMILL_PROBE RESULT=REFUSED_GUEST_GUARD NO_TPM_OR_NVRAM_ACCESS\r\n");
        return EFI_DEVICE_ERROR;
    }
    if (!table->boot || !table->runtime || !table->boot->locate || !table->boot->stall ||
        !table->runtime->get_variable || !table->runtime->set_variable || !table->runtime->reset)
        return EFI_DEVICE_ERROR;
    serial_init();
    output(serial_enabled ? "VIRMILL_PROBE SERIAL=COM1_115200_8N1_GUEST_ONLY\r\n" :
        "VIRMILL_PROBE SERIAL=UNAVAILABLE EFI_CONSOLE_ONLY\r\n");
    EFI_STATUS status = table->boot->locate(&tcg2_guid, NULL, (void **)&tcg2);
    if (status || !tcg2 || !tcg2->submit) failure("LOCATE_TCG2", status);
    int nv = nv_present();
    int variable = variable_present();
    output("VIRMILL_PROBE INITIAL_TPM="); output(nv ? "PRESENT" : "ABSENT");
    output(" INITIAL_NVRAM="); output(variable ? "PRESENT\r\n" : "ABSENT\r\n");
    if (nv != variable) finish("FAIL_AUXILIARY_STATE_MISMATCH");
    if (nv) finish("PRESERVED");
    uint8_t bytes[PROBE_BUFFER_SIZE];
    struct probe_response response = submit(PROBE_DEFINE, bytes);
    if (response.code) failure("NV_DEFINE", response.code);
    if (!probe_session_result(&response, 0)) failure("NV_DEFINE_RESPONSE", 0);
    response = submit(PROBE_WRITE, bytes);
    if (response.code) failure("NV_WRITE", response.code);
    if (!probe_session_result(&response, 0)) failure("NV_WRITE_RESPONSE", 0);
    status = table->runtime->set_variable(marker_name, &marker_guid, PROBE_VARIABLE_ATTRIBUTES,
        PROBE_MARKER_SIZE, (void *)probe_marker);
    if (status) failure("SET_VARIABLE", status);
    if (!nv_present() || !variable_present()) failure("SEED_READBACK", 0);
    finish("SEEDED");
    return EFI_DEVICE_ERROR;
}
