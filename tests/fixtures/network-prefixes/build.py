#!/usr/bin/env python3
"""Generate synthetic little-endian Linux rtnetlink fixture datagrams, no host I/O."""
import ipaddress
from pathlib import Path
import struct

ROOT = Path(__file__).resolve().parent
SEQ, PORT = 7, 4242

def aligned(data):
    return data + bytes((-len(data)) % 4)

def attr(kind, data):
    return aligned(struct.pack('<HH', len(data) + 4, kind) + data)

def u32(kind, value):
    return attr(kind, struct.pack('<I', value))

def ip(kind, address):
    return attr(kind, ipaddress.ip_address(address).packed)

def msg(kind, payload, flags=2):
    return aligned(struct.pack('<IHHII', len(payload) + 16, kind, flags, SEQ, PORT) + payload)

def addr(family, bits, index, attributes, flags=0):
    return msg(20, struct.pack('<BBBBI', family, bits, flags, 0, index) + attributes)

def route(family, bits, table, attributes, source_bits=0, kind=1):
    return msg(24, struct.pack('<BBBBBBBBI', family, bits, source_bits, 0, table, 4, 0, kind, 0) + attributes)

def hop(index, attributes=b'', flags=0):
    return aligned(struct.pack('<HBBi', 8 + len(attributes), flags, 0, index) + attributes)

def write(name, messages):
    wire = b''.join(messages) + msg(3, struct.pack('<i', 0))
    (ROOT / name).write_text('\n'.join(wire[i:i+32].hex() for i in range(0, len(wire), 32)) + '\n')

write('ipv4-addresses.hex', [
    addr(2, 24, 2, ip(1, '192.0.2.129') + ip(2, '192.0.2.129')),
    addr(2, 32, 8, ip(1, '198.51.100.9') + ip(2, '203.0.113.7')),
    addr(2, 0, 3, ip(2, '192.0.2.6')),
    addr(2, 24, 2, ip(2, '192.0.2.130')),  # same canonical network, deduplicated
])
write('ipv6-addresses.hex', [
    addr(10, 64, 2, ip(1, '2001:db8:1::1234') + u32(8, 0x40)),  # tentative retained
    addr(10, 64, 2, ip(1, 'fe80::abcd')),
    addr(10, 128, 8, ip(1, '2001:db8:2::2') + ip(2, '2001:db8:3::1')),
])
write('ipv4-routes.hex', [
    route(2, 0, 254, u32(4, 2)),
    route(2, 24, 252, ip(1, '192.0.2.123') + u32(15, 1000) + u32(4, 8)),
    route(2, 24, 100, ip(1, '10.22.33.9') + attr(9, hop(8, ip(5, '198.51.100.1')) + hop(9, ip(5, '198.51.100.2'), flags=1))),
    route(2, 16, 0, ip(1, '10.44.99.1') + u32(15, 2000) + u32(30, 17)),
    route(2, 24, 254, ip(1, '203.0.113.0'), kind=6),  # blackhole retained
])
write('ipv6-routes.hex', [
    route(10, 0, 254, u32(4, 2)),
    route(10, 64, 254, ip(1, 'fe80::') + u32(4, 2)),
    route(10, 64, 0, ip(1, 'fd12:3456:789a::123') + ip(2, '2001:db8:1::') + u32(15, 500) + u32(4, 8), source_bits=64),
    route(10, 64, 100, ip(1, '2001:db8:9::123') + attr(9, hop(9, ip(5, '2001:db8::1')) + hop(11, ip(5, '2001:db8::2')))),
])
