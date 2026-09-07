/* Virmill BIOS disk-order probe. Reads only; never writes a disk sector.
 * Assemble with GNU as --32; link at 0x7c00 as a flat binary. */
.code16
.text
.global _start
_start:
    ljmp $0, $start16
start16:
    cli
    xorw %ax, %ax
    movw %ax, %ds
    movw %ax, %es
    movw %ax, %ss
    movw $0x7000, %sp
    cld
    sti
    movw $0x0003, %ax
    int $0x10
    call serial_init
    movw $intro, %si
    call print
    /* CHS 0/0/1 on BIOS hard drive 0x81, into 0000:8000. */
    xorw %ax, %ax
    movw %ax, %es
    movw $0x8000, %bx
    movw $0x0201, %ax
    movw $0x0001, %cx
    movw $0x0081, %dx
    int $0x13
    jc failed
    xorw %ax, %ax
    movw %ax, %ds
    movw %ax, %es
    movw $expected, %si
    movw $0x8000, %di
    movw $(expected_end-expected), %cx
    cld
    repe cmpsb
    jne failed
    movw $passed, %si
    call print
    jmp stopped
failed:
    xorw %ax, %ax
    movw %ax, %ds
    movw $failure, %si
    call print
stopped:
    cli
    hlt
    jmp stopped

serial_init:
    movw $0x3fb, %dx
    movb $0x80, %al
    outb %al, %dx
    movw $0x3f8, %dx
    movb $3, %al
    outb %al, %dx
    incw %dx
    xorb %al, %al
    outb %al, %dx
    movw $0x3fb, %dx
    movb $3, %al
    outb %al, %dx
    movw $0x3fa, %dx
    movb $0xc7, %al
    outb %al, %dx
    movw $0x3fc, %dx
    movb $0x0b, %al
    outb %al, %dx
    ret
print:
    lodsb
    testb %al, %al
    jz printed
    call putchar
    jmp print
printed:
    ret
putchar:
    pushw %ax
    pushw %bx
    pushw %dx
    pushw %si
    movb %al, %bl
    movw $0x3fd, %dx
serial_ready:
    inb %dx, %al
    testb $0x20, %al
    jz serial_ready
    movb %bl, %al
    movw $0x3f8, %dx
    outb %al, %dx
    movb $0x0e, %ah
    movw $7, %bx
    int $0x10
    popw %si
    popw %dx
    popw %bx
    popw %ax
    ret
intro: .asciz "VIRMILL BIOS MULTIDISK PROBE\r\n"
passed: .asciz "VIRMILL SECOND DISK PASS\r\n"
failure: .asciz "VIRMILL SECOND DISK FAIL\r\n"
expected: .ascii "VIRMILL-DATA-DISK-v1\0"
expected_end:
.org 510
.word 0xaa55
