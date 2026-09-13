#!/usr/bin/env python3
"""Parent-operated, single-use packet observations on a NEW disposable network.

Importing this module performs no native operations. See docs/network-packet-fixtures.md.
"""

import argparse
import errno
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
import socket
import stat
import struct
import subprocess
import sys
import threading
import time
import uuid
import xml.etree.ElementTree as ET


APPROVED_PARENT = Path("<test-vm-home>/virmill-tests")
MAX_FRAME = 2048
MAX_COMMAND_OUTPUT = 262144
COOKIE = b"\x63\x82\x53\x63"
BR_STATE_FORWARDING = 3  # Linux uapi linux/if_bridge.h; read-only brport/state.


class Refusal(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Refusal(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def canonical_uuid(value):
    parsed = uuid.UUID(value)
    require(str(parsed) == value and parsed.int != 0, "UUID must be canonical lowercase and nonzero")
    return parsed


def canonical_ip(value, version):
    parsed = ipaddress.ip_address(value)
    require(parsed.version == version and str(parsed) == value, "noncanonical IP address")
    require(not parsed.is_unspecified and not parsed.is_multicast and not parsed.is_loopback,
            "unspecified, multicast and loopback targets are refused")
    return parsed


def selected_root(value):
    path = Path(value)
    require(0 < len(value) <= 4096 and value == str(path) == os.path.normpath(value) and
            value == value.strip() and all(ord(char) >= 32 and ord(char) != 127 for char in value),
            "run root must be a canonical absolute path without controls or edge whitespace")
    require(path.is_absolute() and path.parent == APPROVED_PARENT, "run root must be a direct child of the approved parent")
    return path


def checksum(data):
    if len(data) % 2:
        data += b"\0"
    total = sum(struct.unpack("!%dH" % (len(data) // 2), data))
    while total >> 16:
        total = (total & 65535) + (total >> 16)
    return (~total) & 65535


def parse_options(data):
    require(len(data) <= MAX_FRAME, "DHCP options exceed bound")
    options = {}
    pos = 0
    while pos < len(data):
        tag = data[pos]
        pos += 1
        if tag == 0:
            continue
        if tag == 255:
            require(not any(data[pos:]), "non-padding bytes follow DHCP end")
            return options
        require(pos < len(data), "missing DHCP option length")
        size = data[pos]
        pos += 1
        require(pos + size <= len(data), "truncated DHCP option")
        require(tag not in options, "duplicate DHCP option")
        options[tag] = data[pos:pos + size]
        pos += size
    raise Refusal("missing DHCP end option")


def parse_dhcp(frame, xid, mac, checksum_status="wire"):
    """Return our DHCP reply, None for unrelated traffic, refuse malformed replies.

    Deliberately excludes VLANs, fragments, option overload and concatenation.
    Those unsupported shapes never establish router-option absence.
    """
    require(len(frame) <= MAX_FRAME, "oversized Ethernet frame")
    if len(frame) < 14 or frame[12:14] != b"\x08\x00":
        return None
    require(len(frame) >= 34 and frame[14] >> 4 == 4, "truncated/invalid IPv4 header")
    ihl = (frame[14] & 15) * 4
    require(20 <= ihl <= 60 and len(frame) >= 14 + ihl, "invalid IPv4 header length")
    ip = frame[14:]
    if ip[9] != 17:
        return None
    size = int.from_bytes(ip[2:4], "big")
    require(ihl + 8 <= size <= len(ip), "invalid IPv4 total length")
    require(checksum(ip[:ihl]) == 0, "bad IPv4 header checksum")
    require(int.from_bytes(ip[6:8], "big") & 0x3fff == 0, "fragmented DHCP unsupported")
    udp = ip[ihl:size]
    sport, dport, udp_size, udp_sum = struct.unpack("!HHHH", udp[:8])
    if (sport, dport) != (67, 68):
        return None
    require(udp_size == len(udp), "invalid DHCP UDP length")
    require(checksum_status in ("wire", "kernel_valid", "kernel_partial"), "unknown checksum status")
    if udp_sum and checksum_status == "wire":
        pseudo = ip[12:20] + b"\0\x11" + struct.pack("!H", udp_size)
        require(checksum(pseudo + udp) == 0, "bad DHCP UDP checksum")
    bootp = udp[8:]
    require(len(bootp) >= 240, "truncated BOOTP reply")
    if int.from_bytes(bootp[4:8], "big") != xid or bootp[28:34] != mac:
        return None
    require(bootp[:3] == b"\x02\x01\x06", "invalid BOOTP operation or Ethernet shape")
    require(bootp[236:240] == COOKIE, "bad DHCP cookie")
    options = parse_options(bootp[240:])
    require(52 not in options, "DHCP option overload unsupported")
    require(53 in options and len(options[53]) == 1, "missing/invalid DHCP message type")
    require(options[53][0] in (2, 5, 6), "unexpected DHCP response phase")
    require(54 in options and len(options[54]) == 4, "missing/invalid DHCP server ID")
    for tag in (1, 51):
        if tag in options:
            require(len(options[tag]) == 4, "invalid DHCP fixed-width option")
    for tag in (3, 6):
        if tag in options:
            require(len(options[tag]) > 0 and len(options[tag]) % 4 == 0,
                    "invalid DHCP router/DNS option")
    addresses = lambda tag: [str(ipaddress.IPv4Address(options[tag][i:i + 4]))
                             for i in range(0, len(options.get(tag, b"")), 4)]
    return {"messageType": options[53][0], "address": str(ipaddress.IPv4Address(bootp[16:20])),
            "server": str(ipaddress.IPv4Address(options[54])),
            "routerOptionPresent": 3 in options, "routers": addresses(3), "dns": addresses(6),
            "subnetMask": str(ipaddress.IPv4Address(options[1])) if 1 in options else None,
            "leaseSeconds": int.from_bytes(options[51], "big") if 51 in options else None,
            "optionCodes": sorted(options), "udpChecksumStatus": checksum_status,
            "frameSHA256": digest(frame),
            "frameHex": frame.hex()}


def lease_hostname(run_id, role):
    require(role in ("a", "b"), "unknown DHCP fixture role")
    return "vp" + canonical_uuid(run_id).hex + "-" + role


def dhcp_request(mac, xid, offered=None, server=None, hostname=None):
    bootp = struct.pack("!BBBBIHH4s4s4s4s16s64s128s", 1, 1, 6, 0, xid, 0, 0x8000,
                        bytes(4), bytes(4), bytes(4), bytes(4), mac + bytes(10), bytes(64), bytes(128))
    options = b"\x35\x01" + bytes([3 if offered else 1]) + b"\x3d\x07\x01" + mac
    options += b"\x37\x06\x01\x03\x06\x33\x36\x79"
    if hostname is not None:
        require(isinstance(hostname, str) and re.fullmatch(r"[a-z][a-z0-9-]{0,61}[a-z0-9]", hostname),
                "DHCP hostname must be one bounded lowercase ASCII label")
        encoded = hostname.encode("ascii")
        options += bytes([12, len(encoded)]) + encoded
    if offered:
        options += b"\x32\x04" + ipaddress.IPv4Address(offered).packed
        options += b"\x36\x04" + ipaddress.IPv4Address(server).packed
    payload = (bootp + COOKIE + options + b"\xff").ljust(300, b"\0")
    udp = struct.pack("!HHHH", 68, 67, 8 + len(payload), 0) + payload
    ip = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(udp), 0, 0, 64, 17, 0,
                     bytes(4), b"\xff" * 4)
    ip = ip[:10] + struct.pack("!H", checksum(ip)) + ip[12:]
    return b"\xff" * 6 + mac + b"\x08\x00" + ip + udp


def validate_lease(reply, network, gateway, ranges):
    address = canonical_ip(reply["address"], 4)
    require(address in network and address not in (network.network_address, network.broadcast_address,
                                                   ipaddress.IPv4Address(gateway)), "unsafe offered address")
    require(any(int(low) <= int(address) <= int(high) for low, high in ranges),
            "lease is outside the pinned DHCP ranges")
    require(reply["server"] == gateway, "DHCP server differs from pinned gateway")
    require(reply["subnetMask"] == str(network.netmask), "DHCP subnet mask differs from pinned network")
    require(reply["leaseSeconds"] is not None and reply["leaseSeconds"] >= 300,
            "missing or too-short fixture DHCP lease")
    return str(address)


def parse_ra(frame):
    require(len(frame) <= MAX_FRAME, "oversized Ethernet frame")
    if len(frame) < 14 or frame[12:14] != b"\x86\xdd":
        return None
    require(len(frame) >= 54 and frame[14] >> 4 == 6, "truncated/invalid IPv6 header")
    ip = frame[14:]
    if ip[6] != 58:  # Extension-header traffic is not interpreted as a validated RA.
        return None
    size = int.from_bytes(ip[4:6], "big")
    require(size >= 1 and 40 + size <= len(ip), "invalid IPv6 payload length")
    packet = ip[40:40 + size]
    if packet[0] != 134:
        return None
    require(size >= 16 and packet[1] == 0 and ip[7] == 255, "invalid RA header or hop limit")
    source = ipaddress.IPv6Address(ip[8:24])
    require(source.is_link_local, "RA source is not link-local")
    pseudo = ip[8:40] + struct.pack("!I", size) + b"\0\0\0\x3a"
    require(checksum(pseudo + packet) == 0, "bad RA checksum")
    prefixes = []
    pos = 16
    while pos < size:
        require(pos + 2 <= size and packet[pos + 1] != 0, "invalid RA option length")
        end = pos + packet[pos + 1] * 8
        require(end <= size, "truncated RA option")
        if packet[pos] == 3:
            require(end - pos == 32 and packet[pos + 2] <= 128, "invalid RA prefix option")
            prefixes.append({"prefix": str(ipaddress.IPv6Address(packet[pos + 16:end])),
                             "prefixLength": packet[pos + 2], "flags": packet[pos + 3]})
        pos = end
    return {"source": str(source), "routerLifetime": int.from_bytes(packet[6:8], "big"),
            "prefixes": prefixes, "frameSHA256": digest(frame), "frameHex": frame.hex()}


def router_solicitation(mac, source):
    source = ipaddress.IPv6Address(source)
    require(source.is_link_local, "RS requires own link-local source")
    dest = ipaddress.IPv6Address("ff02::2")
    packet = b"\x85\0\0\0" + bytes(4) + b"\x01\x01" + mac
    pseudo = source.packed + dest.packed + struct.pack("!I", len(packet)) + b"\0\0\0\x3a"
    packet = packet[:2] + struct.pack("!H", checksum(pseudo + packet)) + packet[4:]
    ip = struct.pack("!IHBB", 6 << 28, len(packet), 58, 255) + source.packed + dest.packed
    return b"\x33\x33\0\0\0\x02" + mac + b"\x86\xdd" + ip + packet


def dns_name(value):
    require(0 < len(value) <= 253 and value.isascii(), "DNS name must be bounded ASCII")
    labels = value.removesuffix(".").split(".")
    require(all(re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?", label)
                for label in labels), "invalid DNS label")
    return ".".join(labels).lower()


def dns_query(name, xid):
    labels = dns_name(name).split(".")
    question = b"".join(bytes([len(label)]) + label.encode("ascii") for label in labels) + b"\0\0\x01\0\x01"
    return struct.pack("!HHHHHH", xid, 0x0100, 1, 0, 0, 0) + question


def parse_dns(packet, xid, name):
    require(12 <= len(packet) <= 4096, "DNS response outside bounds")
    ident, flags, qd, an, ns, ar = struct.unpack("!HHHHHH", packet[:12])
    require(ident == xid and flags & 0x8000 and not flags & 0x7800 and not flags & 0x0200,
            "DNS identity/opcode/truncation mismatch")
    require(qd == 1 and an + ns + ar <= 128, "DNS record count exceeds bounds")

    def read_name(pos):
        labels, end, seen = [], None, set()
        while True:
            require(pos < len(packet) and pos not in seen and len(seen) < 128, "DNS name loop/bound")
            seen.add(pos)
            length = packet[pos]
            if length & 0xc0 == 0xc0:
                require(pos + 1 < len(packet), "truncated DNS pointer")
                target = ((length & 63) << 8) | packet[pos + 1]
                require(12 <= target < pos, "DNS pointer must reference prior message name")
                if end is None:
                    end = pos + 2
                pos = target
                continue
            require(length <= 63 and pos + 1 + length <= len(packet), "invalid DNS label length")
            pos += 1
            if length == 0:
                return ".".join(labels).lower(), end or pos
            require(packet[pos:pos + length].isascii(), "non-ASCII DNS label")
            labels.append(packet[pos:pos + length].decode("ascii"))
            require(sum(len(label) + 1 for label in labels) <= 254, "DNS name exceeds bound")
            pos += length
    question, pos = read_name(12)
    require(question == dns_name(name) and packet[pos:pos + 4] == b"\0\x01\0\x01", "DNS question mismatch")
    pos += 4
    addresses, answers = [], []
    for index in range(an + ns + ar):
        owner, pos = read_name(pos)
        require(pos + 10 <= len(packet), "truncated DNS record")
        kind, family, _ttl, size = struct.unpack("!HHIH", packet[pos:pos + 10])
        pos += 10
        require(pos + size <= len(packet), "truncated DNS record data")
        if index < an and kind == 1 and family == 1:
            require(size == 4, "invalid DNS A record")
            address = str(ipaddress.IPv4Address(packet[pos:pos + size]))
            addresses.append(address)
            answers.append({"name": owner, "address": address})
        pos += size
    require(pos == len(packet), "trailing DNS response bytes")
    rcode = flags & 15
    return {"status": "resolved" if rcode == 0 and addresses else "no_A_answer", "rcode": rcode,
            "addresses": addresses, "answers": answers, "responseSHA256": digest(packet), "responseHex": packet.hex()}


def private_subnet(value):
    network = ipaddress.IPv4Network(value, strict=True)
    require(str(network) == value and 8 <= network.prefixlen <= 29 and
            any(network.subnet_of(ipaddress.IPv4Network(cidr)) for cidr in
                ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")),
            "static subnet must be canonical RFC1918 /8 through /29")
    return network


def validate_profile(args):
    require((args.kind in ("nat", "lab") and args.host_access in ("allow", "services-only")) or
            (args.kind == "guest-only" and args.host_access == "deny" and args.dhcp == "off"),
            "unsupported kind/host-access/DHCP combination")
    protected = args.host_access != "allow"
    require(bool(args.forward4_target) == bool(args.forward4_port), "forward target and port must be supplied together")
    require(not protected or args.forward4_target, "protected profiles require an explicit forward target control")
    if args.forward4_target:
        target = canonical_ip(args.forward4_target, 4)
        require(not target.is_link_local and 1 <= args.forward4_port <= 65535, "invalid forward target or TCP port")
    if args.dns_name:
        dns_name(args.dns_name)
    dns_modes = int(bool(args.dns_name)) + int(bool(args.dns_own_lease))
    require(dns_modes <= 1 and (args.dhcp == "on" or dns_modes == 0),
            "select at most one DNS mode and only with DHCP enabled")
    if args.host_access == "services-only":
        require(dns_modes == int(args.dhcp == "on"),
                "services-only requires a DNS query exactly when paired DHCP/DNS is enabled")
    if args.kind == "guest-only":
        require(args.static_cidr4 and args.static_a4 and args.static_b4 and args.host4_target,
                "guest-only requires static CIDR, two static endpoints and explicit assigned host target")
        network = private_subnet(args.static_cidr4)
        for value in (args.static_a4, args.static_b4):
            address = canonical_ip(value, 4)
            require(address in network and address not in (network.network_address, network.broadcast_address),
                    "guest-only static endpoint is outside its usable logical subnet")
        host = canonical_ip(args.host4_target, 4)
        require(not host.is_link_local and host not in network, "guest-only host target must be outside its logical subnet")
        require(not args.dns_name and args.forward4_target != args.host4_target,
                "guest-only has no managed DNS query and distinct host/forward targets are required")
    else:
        require(not args.static_cidr4 and not args.host4_target,
                "static CIDR and host target overrides are guest-only flags")
    if args.dhcp == "off":
        require(args.static_a4 and args.static_b4 and args.static_a4 != args.static_b4,
                "DHCP-off requires two distinct explicit static addresses")
        canonical_ip(args.static_a4, 4)
        canonical_ip(args.static_b4, 4)
    else:
        require(not args.static_a4 and not args.static_b4, "DHCP profiles require own leases, not static overrides")


def native_network(raw, network_id, bridge, kind, dhcp, host_access="allow", static_cidr4=None):
    require(len(raw) <= 65536 and not re.search(br"<!(?!--)", raw) and b"<?" not in raw,
            "oversized XML or XML declarations/entities refused")
    root = ET.fromstring(raw)
    require(root.tag == "network" and len(root.findall("uuid")) == 1 and root.findtext("uuid") == network_id,
            "native network UUID mismatch")
    require(len(root.findall("name")) == 1 and root.findtext("name") == "virmill-" + network_id,
            "network is not the new Virmill name")
    markers = root.findall("metadata/{urn:virmill:v1}networkCreation")
    require(len(markers) == 1 and markers[0].get("apiVersion") == "virmill/v1" and
            markers[0].get("version") == ("1" if host_access == "allow" else "2") and
            re.fullmatch(r"[0-9a-f]{64}", markers[0].get("intent", "")),
            "missing current Virmill creation marker; marker is not an ownership grant")
    require(bridge == "vm" + canonical_uuid(network_id).hex[:12] and re.fullmatch(r"vm[0-9a-f]{12}", bridge),
            "bridge is not the UUID-bound Virmill bridge")
    bridges = root.findall("bridge")
    require(len(bridges) == 1 and bridges[0].get("name") == bridge, "native bridge mismatch")
    forwards = root.findall("forward")
    require((kind == "nat" and len(forwards) == 1 and forwards[0].get("mode", "nat") == "nat") or
            (kind in ("lab", "guest-only") and not forwards), "native forwarding mode mismatch")
    # These two omitted defaults are the same equivalences used by the parent's
    # networkxml.Match. They say nothing about actual bridge-frame filtering.
    require(root.get("ipv6") in (None, "no"), "this fixture profile requires declared IPv6 disabled")
    if host_access != "allow":
        dns = root.findall("dns")
        require(len(dns) <= 1, "ambiguous native DNS configuration")
        require(len(dns) == 1 and dns[0].attrib == {"enable": "yes" if dhcp == "on" else "no"} and
                not list(dns[0]) and not (dns[0].text or "").strip(),
                "protected native DNS must explicitly match paired DHCP/DNS")
    ips = root.findall("ip")
    if kind == "guest-only":
        require(host_access == "deny" and dhcp == "off" and not ips and not root.findall(".//dhcp") and
                not root.findall("route"), "guest-only must have no native IP, route or DHCP")
        return private_subnet(static_cidr4), None, []
    require(len(ips) == 1 and ips[0].get("family", "ipv4") == "ipv4", "ambiguous native IP configuration")
    ip = ips[0]
    gateway = str(canonical_ip(ip.get("address", ""), 4))
    require(("prefix" in ip.attrib) != ("netmask" in ip.attrib), "native IP must have exactly one prefix or netmask")
    prefix = ip.get("prefix") if "prefix" in ip.attrib else ip.get("netmask")
    network = ipaddress.IPv4Network(gateway + "/" + prefix, strict=False)
    if "prefix" in ip.attrib:
        require(prefix == str(network.prefixlen), "noncanonical IPv4 prefix")
    else:
        require(prefix == str(network.netmask), "native netmask is not an ordinary contiguous IPv4 mask")
    require(network.prefixlen <= 29 and network.is_private, "fixture requires two endpoints and a private /29 or larger subnet")
    ranges = []
    dhcps = ip.findall("dhcp")
    require(len(dhcps) == int(dhcp == "on"), "native DHCP profile mismatch")
    if dhcps:
        require(not dhcps[0].findall("host"), "new-network fixture refuses existing DHCP reservations")
        for item in dhcps[0].findall("range"):
            low, high = canonical_ip(item.get("start", ""), 4), canonical_ip(item.get("end", ""), 4)
            require(low in network and high in network and int(low) <= int(high), "invalid native DHCP range")
            ranges.append((low, high))
        require(1 <= len(ranges) <= 8, "missing or excessive DHCP ranges")
    return network, gateway, ranges


def ordinary_path(path, directory=False):
    require(path.is_absolute() and str(path) == os.path.normpath(str(path)), "noncanonical absolute path")
    for component in [*reversed(path.parents), path]:
        state = component.lstat()
        require(not stat.S_ISLNK(state.st_mode), "symlink path component refused: " + str(component))
    state = path.stat()
    require(stat.S_ISDIR(state.st_mode) if directory else stat.S_ISREG(state.st_mode),
            "nonordinary path: " + str(path))
    return state


def veth_pair_snapshot(endpoint, links):
    """Inspect a new DOWN, unattached pair while both ends share this namespace."""
    pair = {}
    for side in ("host", "peer"):
        matches = [item for item in links if item.get("ifname") == endpoint[side]]
        require(len(matches) == 1, "new veth endpoint missing or ambiguous")
        item = matches[0]
        require(item.get("linkinfo", {}).get("info_kind") == "veth" and
                type(item.get("ifindex")) is int and item["ifindex"] > 0,
                "new endpoint is not an identified veth")
        require(type(item.get("flags")) is list and "UP" not in item["flags"] and
                item.get("operstate") == "DOWN" and "master" not in item,
                "new veth is no longer DOWN and unattached")
        address = item.get("address", "")
        require(re.fullmatch(r"[0-9a-f]{2}(?::[0-9a-f]{2}){5}", address) and
                not int(address[:2], 16) & 1 and address != "00:00:00:00:00:00",
                "new veth MAC is not canonical unicast")
        alias = item.get("ifalias", "")
        require(type(alias) is str, "invalid veth alias shape")
        pair[side] = {"name": endpoint[side], "ifindex": item["ifindex"], "address": address, "alias": alias}
    require(pair["host"]["ifindex"] != pair["peer"]["ifindex"], "veth endpoint indices are not distinct")
    for side, other in (("host", "peer"), ("peer", "host")):
        item = next(item for item in links if item.get("ifname") == endpoint[side])
        require("link" in item or "link_index" in item, "veth reciprocal peer reference unavailable")
        if "link" in item:
            require(item["link"] == endpoint[other], "veth reciprocal peer name differs")
        if "link_index" in item:
            require(type(item["link_index"]) is int and item["link_index"] == pair[other]["ifindex"],
                    "veth reciprocal peer index differs")
    return pair


class Recorder:
    def __init__(self, output, root):
        ordinary_path(output.parent, directory=True)
        require(output.parent == root and root.parent == APPROVED_PARENT,
                "output must be beneath the selected approved disposable run root")
        output.mkdir(mode=0o700)  # exclusive; retained even if all later preflight fails
        parent_fd = os.open(output.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            os.fsync(parent_fd)
        finally:
            os.close(parent_fd)
        self.output = output
        self.journal = (output / "commands.jsonl").open("x", encoding="utf-8")
        self.count = 0
        self.bytes = 0
        self.deadline = time.monotonic() + 180

    def event(self, value):
        self.journal.write(json.dumps(value, sort_keys=True) + "\n")
        self.journal.flush()
        os.fsync(self.journal.fileno())

    def save(self, name, value):
        with (self.output / name).open("x", encoding="utf-8") as f:
            json.dump(value, f, indent=2, sort_keys=True)
            f.write("\n")
            f.flush()
            os.fsync(f.fileno())
        fd = os.open(self.output, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)

    def command(self, argv, timeout=10, okay=(0,)):
        require(self.count < 240 and self.bytes < 8 * 1024 * 1024, "command journal bound reached")
        require(time.monotonic() < self.deadline, "fixture command deadline exceeded")
        self.count += 1
        self.event({"sequence": self.count, "phase": "intent", "argv": argv, "timeoutSeconds": timeout})
        start = time.monotonic()
        proc = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, env={"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C",
                                                            "PYTHONDONTWRITEBYTECODE": "1"},
                                start_new_session=True)
        captured = {"stdout": bytearray(), "stderr": bytearray()}
        aborted = None
        try:
            with selectors.DefaultSelector() as select:
                for name in captured:
                    pipe = getattr(proc, name)
                    os.set_blocking(pipe.fileno(), False)
                    select.register(pipe, selectors.EVENT_READ, name)
                while select.get_map():
                    if time.monotonic() - start > timeout:
                        raise Refusal("command deadline exceeded")
                    for key, _ in select.select(0.1):
                        data = os.read(key.fd, 16384)
                        if not data:
                            select.unregister(key.fileobj)
                            continue
                        captured[key.data].extend(data)
                        require(len(captured[key.data]) <= MAX_COMMAND_OUTPUT, "command output bound exceeded")
            proc.wait(timeout=max(0.1, timeout - (time.monotonic() - start)))
        except BaseException as exc:
            aborted = str(exc) or type(exc).__name__
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            proc.wait()
        finally:
            proc.stdout.close()
            proc.stderr.close()
        result = {name: bytes(value[:MAX_COMMAND_OUTPUT]).decode("utf-8", "replace")
                  for name, value in captured.items()}
        result.update({"sequence": self.count, "phase": "result", "returncode": proc.returncode,
                       "elapsedSeconds": round(time.monotonic() - start, 3), "aborted": aborted})
        self.bytes += sum(len(value) for value in captured.values())
        self.event(result)
        require(not aborted and proc.returncode in okay, "command failed; see commands.jsonl sequence " + str(self.count))
        return result


def discover(recorder):
    required = {"ip": ("/usr/bin/ip", "/usr/sbin/ip"), "virsh": ("/usr/bin/virsh",),
                "udevadm": ("/usr/bin/udevadm", "/usr/sbin/udevadm"),
                "ping": ("/usr/bin/ping",), "python": ("/usr/bin/python3",)}
    found = {}
    versions = {"pythonRuntime": sys.version, "kernel": os.uname().release,
                "optionalDHCPClients": {name: shutil.which(name) for name in ("dhclient", "udhcpc", "dhcpcd")},
                "tcpdump": shutil.which("tcpdump"), "rawClient": "Python standard library AF_PACKET"}
    for name, candidates in required.items():
        selected = next((Path(path) for path in candidates if Path(path).is_file()), None)
        require(selected is not None, "missing required binary " + name)
        # Distribution /usr/bin/python3 symlinks are resolved and pinned as tools,
        # unlike selected source/output files, where symlinks are refused.
        resolved = selected.resolve(strict=True)
        state = ordinary_path(resolved)
        require(state.st_uid == 0 and not state.st_mode & 0o022, "untrusted tool ownership/mode")
        found[name] = str(resolved)
        flag = "-Version" if name == "ip" else "-V" if name == "ping" else "--version"
        versions[name] = {"path": str(resolved), "sha256": digest(resolved.read_bytes()),
                          "version": recorder.command([str(resolved), flag])}
    recorder.save("prerequisites.json", versions)
    return found


def worker_guard(args):
    require(os.geteuid() == 0, "worker requires disposable-host root")
    require(os.stat("/proc/self/ns/net").st_ino != args.host_netns_inode, "worker refuses host namespace")
    require(re.fullmatch(r"ve[ab][0-9a-f]{10}", args.interface), "worker interface grammar refused")
    require(Path("/sys/class/net", args.interface, "ifalias").read_text().strip() ==
            "virmill-packet:" + args.run_id + ":peer", "worker interface ownership mismatch")
    require(digest(Path(__file__).read_bytes()) == args.recipe_sha256, "worker source hash changed")
    return bytes.fromhex(Path("/sys/class/net", args.interface, "address").read_text().strip().replace(":", ""))


def dhcp_frame_sample(frame, packet_address, flags):
    captured = frame[:MAX_FRAME]
    result = {"receivedBytes": len(frame), "capturedBytes": len(captured), "truncated": len(frame) > MAX_FRAME,
              "frameSHA256": digest(captured), "frameHex": captured.hex(), "messageFlags": flags,
              "packetType": packet_address[2] if len(packet_address) >= 3 else None,
              "classification": "not_parsed"}
    if len(frame) >= 14:
        result["etherType"] = frame[12:14].hex()
    if len(frame) >= 34 and frame[12:14] == b"\x08\x00" and frame[14] >> 4 == 4:
        result["ipProtocol"] = frame[23]
        ihl = (frame[14] & 15) * 4
        udp_start = 14 + ihl
        if 20 <= ihl <= 60 and frame[23] == 17 and len(frame) >= udp_start + 8:
            result["udpPorts"] = list(struct.unpack("!HH", frame[udp_start:udp_start + 4]))
            bootp = frame[udp_start + 8:]
            if len(bootp) >= 34:
                result["bootpXID"] = int.from_bytes(bootp[4:8], "big")
                result["bootpClientMAC"] = bootp[28:34].hex()
    return result


def dhcp_worker(args, mac):
    received_samples = []
    try:
        return dhcp_exchange(args, mac, received_samples)
    except (Refusal, OSError) as exc:
        raise Refusal(str(exc) + "; " + json.dumps({"receivedSamples": received_samples}, sort_keys=True)) from exc


def dhcp_exchange(args, mac, received_samples):
    xid = int.from_bytes(os.urandom(4), "big")
    malformed = []
    offers = []
    transmitted = []
    hostname = getattr(args, "dhcp_hostname", None)
    with socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0800)) as sock:
        sock.setsockopt(263, 8, 1)  # SOL_PACKET / PACKET_AUXDATA, Linux uapi.
        sock.bind((args.interface, 0))
        sock.settimeout(0.25)
        request = dhcp_request(mac, xid, hostname=hostname)
        for phase in (2, 5):
            deadline, resend, frames = time.monotonic() + 18, 0, 0
            while time.monotonic() < deadline and frames < 512:
                if time.monotonic() >= resend:
                    sock.send(request)
                    transmitted.append({"phase": phase, "frameSHA256": digest(request), "frameHex": request.hex()})
                    resend = time.monotonic() + 3
                try:
                    frame, ancillary, msg_flags, packet_address = sock.recvmsg(MAX_FRAME + 1, socket.CMSG_SPACE(20))
                except socket.timeout:
                    continue
                frames += 1
                sample = dhcp_frame_sample(frame, packet_address, msg_flags) if len(received_samples) < 8 else None
                if sample is not None:
                    received_samples.append(sample)
                try:
                    require(not msg_flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC), "truncated packet/auxdata")
                    checksum_status = "wire"
                    for level, kind, data in ancillary:
                        if (level, kind) == (263, 8):
                            require(len(data) == 20, "invalid PACKET_AUXDATA shape")
                            status = struct.unpack("=I", data[:4])[0]
                            if status & 8:  # TP_STATUS_CSUMNOTREADY, veth checksum offload.
                                checksum_status = "kernel_partial"
                            elif status & 128:  # TP_STATUS_CSUM_VALID.
                                checksum_status = "kernel_valid"
                    reply = parse_dhcp(frame, xid, mac, checksum_status)
                    if sample is not None:
                        sample["checksumStatus"] = checksum_status
                except Refusal as exc:
                    if sample is not None:
                        sample.update({"classification": "malformed", "reason": str(exc)[:256]})
                    if len(malformed) < 16:
                        malformed.append(str(exc))
                    continue
                if reply is None:
                    if sample is not None:
                        sample["classification"] = "unrelated"
                    continue
                if sample is not None:
                    sample["classification"] = "matched_phase_" + str(reply["messageType"])
                require(reply["messageType"] != 6, "DHCP NAK received")
                if reply["messageType"] != phase:
                    continue
                if phase == 2:
                    validate_lease(reply, ipaddress.IPv4Network(args.network), args.gateway,
                                   [(ipaddress.IPv4Address(low), ipaddress.IPv4Address(high))
                                    for low, high in (value.split(",") for value in args.range)])
                    offers.append(reply)
                    request = dhcp_request(mac, xid, reply["address"], reply["server"], hostname=hostname)
                    break
                require(reply["server"] == offers[0]["server"] and reply["address"] == offers[0]["address"],
                        "DHCP ACK differs from selected offer")
                return {"status": "acknowledged", "xid": xid, "offer": offers[0], "ack": reply,
                        "malformedFrames": malformed, "transmitted": transmitted, "requestedHostname": hostname}
            else:
                raise Refusal("DHCP phase " + str(phase) + " timed out or reached packet bound; " +
                              json.dumps({"frames": frames, "malformed": malformed, "transmitted": transmitted}, sort_keys=True))
    raise Refusal("DHCP did not complete")


def ra_worker(args, mac):
    observed, malformed = [], []
    with socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x86dd)) as sock:
        sock.bind((args.interface, 0))
        sock.settimeout(0.25)
        solicitation = router_solicitation(mac, args.source) if args.source else None
        if solicitation:
            sock.send(solicitation)
        start, frames = time.monotonic(), 0
        while time.monotonic() - start < 5 and frames < 512:
            try:
                frame = sock.recv(MAX_FRAME + 1)
            except socket.timeout:
                continue
            frames += 1
            try:
                value = parse_ra(frame)
                if value and len(observed) < 16:
                    observed.append(value)
            except Refusal as exc:
                if len(malformed) < 16:
                    malformed.append(str(exc))
    return {"status": "observed" if observed else "none_seen_in_bounded_window", "seconds": 5,
            "solicited": bool(args.source), "advertisements": observed, "malformedFrames": malformed,
            "packetBoundReached": frames >= 512, "absenceProven": False,
            "solicitationFrameHex": solicitation.hex() if solicitation else None,
            "extensionHeaderRAsInterpreted": False}


def dhcp_absence_worker(args, mac):
    xid = int.from_bytes(os.urandom(4), "big")
    request = dhcp_request(mac, xid)
    observed, malformed, samples = [], [], []
    frames = 0
    with socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0800)) as sock:
        sock.setsockopt(263, 8, 1)
        sock.bind((args.interface, 0))
        sock.settimeout(0.25)
        start, resend = time.monotonic(), 0
        while time.monotonic() - start < 5 and frames < 512:
            if time.monotonic() >= resend:
                sock.send(request)
                resend = time.monotonic() + 2
            try:
                frame, ancillary, flags, address = sock.recvmsg(MAX_FRAME + 1, socket.CMSG_SPACE(20))
            except socket.timeout:
                continue
            frames += 1
            sample = dhcp_frame_sample(frame, address, flags) if len(samples) < 8 else None
            if sample is not None:
                samples.append(sample)
            try:
                require(not flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC), "truncated packet/auxdata")
                state = "wire"
                for level, kind, data in ancillary:
                    if (level, kind) == (263, 8):
                        require(len(data) == 20, "invalid PACKET_AUXDATA shape")
                        status = struct.unpack("=I", data[:4])[0]
                        state = "kernel_partial" if status & 8 else "kernel_valid" if status & 128 else "wire"
                value = parse_dhcp(frame, xid, mac, state)
                if sample is not None:
                    sample.update({"classification": "matched" if value else "unrelated", "checksumStatus": state})
                if value and len(observed) < 16:
                    observed.append(value)
            except Refusal as exc:
                if sample is not None:
                    sample.update({"classification": "malformed", "reason": str(exc)[:256]})
                if len(malformed) < 16:
                    malformed.append(str(exc))
    # No offer is selected, requested or configured. Packet saturation or a
    # malformed response cannot manufacture a successful absence observation.
    clean = not observed and not malformed and frames < 512 and time.monotonic() - start >= 5
    return {"status": "none_seen_in_bounded_window" if clean else "response_or_inconclusive",
            "seconds": 5, "responses": observed, "malformedFrames": malformed, "receivedSamples": samples,
            "packetBoundReached": frames >= 512, "absenceProven": False, "xid": xid,
            "transmittedDiscoverHex": request.hex(), "transmittedDiscoverSHA256": digest(request)}


def tcp_worker(args):
    address = ipaddress.ip_address(args.target)
    family = socket.AF_INET if address.version == 4 else socket.AF_INET6
    connected = False
    try:
        with socket.socket(family, socket.SOCK_STREAM) as sock:
            sock.settimeout(3)
            sock.connect((str(address), args.port))
            connected = True
            if args.token:
                received = bytearray()
                while len(received) < len(args.token):
                    part = sock.recv(256 - len(received))
                    if not part:
                        break
                    received.extend(part)
                require(received.decode("ascii") == args.token, "host listener token mismatch")
        return {"status": "connected", "target": args.target, "port": args.port, "tokenVerified": True if args.token else None}
    except OSError as exc:
        if connected:
            # A completed handshake is exposure even if the application token
            # subsequently stalls. Never count it as blocked host access.
            return {"status": "connected", "target": args.target, "port": args.port,
                    "tokenVerified": False, "applicationError": str(exc), "errno": exc.errno}
        return {"status": "not_connected", "errno": exc.errno, "reason": str(exc),
                "target": args.target, "port": args.port, "filterEnforcementProven": False,
                "boundedNetworkRefusal": isinstance(exc, TimeoutError) or exc.errno in
                    (errno.ECONNREFUSED, errno.EHOSTUNREACH, errno.ENETUNREACH, errno.EACCES)}


def host_tcp_control(target, port, token=None):
    # No native command or device change. Only an explicitly selected IPv4 and
    # port may be contacted. The parent must authorize the external endpoint.
    return tcp_worker(argparse.Namespace(target=target, port=port, token=token))


def tcp_expectation(result, connected):
    if connected:
        return result.get("status") == "connected" and result.get("tokenVerified") is not False
    return result.get("status") == "not_connected" and result.get("boundedNetworkRefusal") is True


def assigned_host_target(rows, target, excluded_bridge):
    matches = []
    require(isinstance(rows, list) and len(rows) <= 4096, "host address inventory is invalid or excessive")
    for row in rows:
        require(isinstance(row, dict) and isinstance(row.get("addr_info"), list), "malformed host address inventory")
        for item in row["addr_info"]:
            require(isinstance(item, dict), "malformed host address entry")
            if item.get("family") == "inet" and item.get("local") == target:
                require(row.get("ifname") != excluded_bridge and "UP" in row.get("flags", []) and
                        item.get("scope") == "global" and not item.get("tentative") and not item.get("dadfailed"),
                        "host target is not a ready assigned address outside the selected bridge")
                require(type(row.get("ifindex")) is int and row["ifindex"] > 0 and
                        isinstance(row.get("ifname"), str) and
                        re.fullmatch(r"[A-Za-z0-9_.:-]{1,15}", row["ifname"]) and
                        isinstance(row.get("address"), str) and
                        re.fullmatch(r"[0-9a-f]{2}(?::[0-9a-f]{2}){5}", row["address"]) and
                        row["address"] != "00:00:00:00:00:00" and not int(row["address"][:2], 16) & 1 and
                        type(item.get("prefixlen")) is int and 0 <= item["prefixlen"] <= 32,
                        "host target requires complete canonical link and IPv4 identity")
                matches.append({"ifindex": row["ifindex"], "ifname": row["ifname"],
                                "address": row["address"], "target": target, "prefixlen": item["prefixlen"]})
    require(len(matches) == 1, "host target must be exactly one already assigned IPv4 address")
    return matches[0]


def dns_worker(args):
    xid = int.from_bytes(os.urandom(2), "big")
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.settimeout(3)
            sock.connect((str(canonical_ip(args.target, 4)), 53))
            sock.send(dns_query(args.dns_name, xid))
            packet = sock.recv(4097)
        return {**parse_dns(packet, xid, args.dns_name), "server": args.target, "name": args.dns_name}
    except OSError as exc:
        return {"status": "not_resolved", "errno": exc.errno, "reason": str(exc), "server": args.target}


def own_lease_dns_expectation(result, endpoint, gateway):
    hostname = endpoint["dhcpHostname"]
    expected = endpoint["lease"]["ack"]["address"]
    require(endpoint["role"] == "b" and endpoint["lease"].get("requestedHostname") == hostname and
            endpoint["address"] == expected, "local DNS target differs from role B's requested hostname/ACK")
    exact = (result.get("status") == "resolved" and result.get("rcode") == 0 and
             result.get("server") == gateway and result.get("name") == hostname and
             result.get("addresses") == [expected] and result.get("answers") == [{"name": hostname, "address": expected}])
    return {"expectedOwnLeaseHostname": hostname, "expectedOwnLeaseAddress": expected,
            "ownLeaseExpectationMet": exact, "upstreamResolutionQualified": False}


class Fixture:
    def __init__(self, args, recorder, tools):
        self.a, self.r, self.tools = args, recorder, tools
        self.endpoints = []
        self.source = str(Path(__file__).absolute())
        self.host_inode = os.stat("/proc/self/ns/net").st_ino
        self.before = None

    def ip(self, *args, **kwargs):
        return self.r.command([self.tools["ip"], *args], **kwargs)

    def links(self):
        return json.loads(self.ip("-j", "-d", "link", "show")["stdout"])

    def netxml(self):
        raw = self.r.command([self.tools["virsh"], "--readonly", "-c", "qemu:///system",
                              "net-dumpxml", self.a.network_id])["stdout"].encode()
        require(digest(raw) == self.a.network_xml_sha256, "selected live network XML hash changed")
        return raw

    def bridge_state(self):
        link = json.loads(self.ip("-j", "-d", "link", "show", "dev", self.a.bridge)["stdout"])
        require(len(link) == 1 and link[0].get("linkinfo", {}).get("info_kind") == "bridge",
                "selected interface is not exactly one native bridge")
        addresses = json.loads(self.ip("-j", "address", "show", "dev", self.a.bridge)["stdout"])
        return {"ifindex": link[0]["ifindex"], "address": link[0]["address"], "mtu": link[0]["mtu"],
                "addresses": sorted((item["family"], item["local"], item["prefixlen"], item["scope"])
                                    for item in addresses[0]["addr_info"])}

    def preflight(self):
        active = self.r.command([self.tools["virsh"], "--readonly", "-c", "qemu:///system",
                                 "net-list", "--uuid"])["stdout"].split()
        require(self.a.network_id in active, "selected network is not active")
        persistent = self.r.command([self.tools["virsh"], "--readonly", "-c", "qemu:///system",
                                     "net-list", "--all", "--persistent", "--uuid"])["stdout"].split()
        autostart = self.r.command([self.tools["virsh"], "--readonly", "-c", "qemu:///system",
                                    "net-list", "--all", "--autostart", "--uuid"])["stdout"].split()
        require(self.a.network_id in persistent and self.a.network_id not in autostart,
                "selected network must be persistent with autostart off")
        network, gateway, ranges = native_network(self.netxml(), self.a.network_id, self.a.bridge,
                                                  self.a.kind, self.a.dhcp, self.a.host_access, self.a.static_cidr4)
        self.before = self.bridge_state()
        if self.a.kind == "guest-only":
            require(not self.before["addresses"], "guest-only bridge must have no native L3 addresses")
            self.host_target_before = self.host_target_state()
            self.r.save("host-target-before.json", self.host_target_before)
        else:
            require(("inet", gateway, network.prefixlen, "global") in self.before["addresses"],
                    "bridge gateway differs from pinned XML")
        if self.a.host_access != "allow":
            self.host_addresses_before = json.loads(self.ip("-j", "-4", "address", "show")["stdout"])
            assigned = {item.get("local") for row in self.host_addresses_before for item in row.get("addr_info", [])}
            require(self.a.forward4_target not in assigned and canonical_ip(self.a.forward4_target, 4) not in network,
                    "forward target must not be any assigned host address or part of the logical subnet")
            if self.a.kind == "guest-only":
                require(not any(ipaddress.IPv4Address(value) in network for value in assigned),
                        "guest-only logical subnet overlaps an assigned host address")
        members = json.loads(self.ip("-j", "link", "show", "master", self.a.bridge)["stdout"])
        require(not members, "selected NEW bridge already has ports; no guest/shared bridge testing")
        existing = {item["ifname"] for item in self.links()}
        run = canonical_uuid(self.a.run_id).hex
        for role in ("a", "b"):
            endpoint = {"role": role, "namespace": "virmill-packet-" + run + "-" + role,
                        "host": "vp" + role + run[:10], "peer": "ve" + role + run[:10],
                        "alias": "virmill-packet:" + self.a.run_id + ":host-" + role,
                        "mac": "02:" + ":".join(run[i:i + 2] for i in (0, 2, 4, 6)) + (":0a" if role == "a" else ":0b")}
            if self.a.dns_own_lease:
                endpoint["dhcpHostname"] = lease_hostname(self.a.run_id, role)
            require(endpoint["host"] not in existing and endpoint["peer"] not in existing,
                    "generated veth name already exists")
            require(not os.path.lexists("/run/netns/" + endpoint["namespace"]), "generated namespace already exists")
            self.endpoints.append(endpoint)
        if self.a.dhcp == "off":
            require(self.a.static_a4 and self.a.static_b4, "DHCP-off requires both explicit static addresses")
            for endpoint, address in zip(self.endpoints, (self.a.static_a4, self.a.static_b4)):
                parsed = canonical_ip(address, 4)
                require(parsed in network and parsed not in (network.network_address, network.broadcast_address) and
                        str(parsed) != gateway, "unsafe static endpoint")
                endpoint["address"] = str(parsed)
            require(self.a.static_a4 != self.a.static_b4, "static endpoints must be distinct")
        else:
            require(not self.a.static_a4 and not self.a.static_b4, "DHCP profiles require own leases, not static overrides")
        sysctls = {}
        for key in ("net/ipv4/ip_forward", "net/ipv6/conf/all/forwarding",
                    "net/ipv6/conf/" + self.a.bridge + "/disable_ipv6",
                    "net/ipv6/conf/" + self.a.bridge + "/forwarding"):
            path = Path("/proc/sys", key)
            sysctls[key] = path.read_text().strip() if path.exists() else "unavailable"
        self.r.save("preflight.json", {"network": str(network), "gateway": gateway, "bridge": self.before,
                                       "sysctlsReadOnly": sysctls, "endpoints": self.endpoints,
                                       "existingBridgePorts": members})
        self.network, self.gateway, self.ranges = network, gateway, ranges

    def host_target_state(self):
        # iproute2's -4 view omits link-layer fields, including the MAC that
        # binds the selected target. Observe the combined view, then select IPv4.
        rows = json.loads(self.ip("-j", "address", "show")["stdout"])
        return assigned_host_target(rows, self.a.host4_target, self.a.bridge)

    def observation_boundary(self, label):
        self.netxml()
        observed = self.bridge_state()
        require(observed == self.before, "selected bridge identity/L3 changed during " + label)
        if self.a.kind == "guest-only":
            require(not observed["addresses"] and self.host_target_state() == self.host_target_before,
                    "guest-only host target binding or bridge L3 changed")
        for endpoint in self.endpoints:
            self.check_attached_endpoints(endpoint)
        self.r.event({"phase": "observation-boundary", "label": label, "bridge": observed})

    def direct_neighbor(self, endpoint, target):
        self.observation_boundary("before explicit target neighbor")
        # The host owns no address on a guest-only bridge. A deliberate /32
        # on-link route and exact bridge MAC avoid treating failed ARP as policy.
        self.ns(endpoint, [self.tools["ip"], "route", "add", target + "/32", "dev", endpoint["peer"], "scope", "link"])
        self.ns(endpoint, [self.tools["ip"], "neigh", "replace", target, "lladdr", self.before["address"],
                           "nud", "permanent", "dev", endpoint["peer"]])
        routes = json.loads(self.ns(endpoint, [self.tools["ip"], "-j", "-4", "route", "show", "exact", target + "/32"])["stdout"])
        neighbors = json.loads(self.ns(endpoint, [self.tools["ip"], "-j", "neigh", "show", "to", target, "dev", endpoint["peer"]])["stdout"])
        require(len(routes) == 1 and routes[0].get("dst") in (target, target + "/32") and
                routes[0].get("dev") == endpoint["peer"] and not routes[0].get("gateway") and
                len(neighbors) == 1 and neighbors[0].get("dst") == target and
                neighbors[0].get("lladdr") == self.before["address"] and
                "PERMANENT" in neighbors[0].get("state", []), "explicit target route/neighbor readback mismatch")
        self.r.event({"phase": "own-namespace-target-neighbor", "namespace": endpoint["namespace"],
                      "route": routes, "neighbor": neighbors, "hostAddressAssigned": False})

    def ns(self, endpoint, argv, **kwargs):
        state = os.lstat("/run/netns/" + endpoint["namespace"])
        require("nsfd" in endpoint and not stat.S_ISLNK(state.st_mode) and
                (state.st_dev, state.st_ino) == (endpoint["nsdev"], endpoint["nsino"]),
                "held namespace binding changed")
        return self.ip("netns", "exec", endpoint["namespace"], *argv, **kwargs)

    def worker(self, endpoint, mode, *extra):
        result = self.ns(endpoint, [self.tools["python"], "-I", "-B", self.source, "worker", "--worker", mode,
                                    "--run-id", self.a.run_id, "--interface", endpoint["peer"],
                                    "--recipe-sha256", self.a.recipe_sha256,
                                    "--host-netns-inode", str(self.host_inode), *extra], timeout=40 if mode == "dhcp" else 24)
        value = json.loads(result["stdout"])
        require(value.get("status") != "failed", "namespace worker failed")
        return value

    def check_created_pair(self, endpoint, links, allowed_aliases, peer_mac=None):
        observed = veth_pair_snapshot(endpoint, links)
        require("createdPair" in endpoint, "new veth pair has no acknowledged identity receipt")
        for side in ("host", "peer"):
            expected = endpoint["createdPair"][side]
            address = peer_mac if side == "peer" and peer_mac is not None else expected["address"]
            require(observed[side]["name"] == expected["name"] and observed[side]["ifindex"] == expected["ifindex"] and
                    observed[side]["address"] == address and observed[side]["alias"] in allowed_aliases[side],
                    "new veth pair identity or journaled alias differs")
        return observed

    def create_veth_pair(self, endpoint):
        # The iproute2 veth constructor may discard an alias supplied with add.
        # Never infer an alias from a successful acknowledgement of that command.
        existing = {item["ifname"] for item in self.links()}
        require(endpoint["host"] not in existing and endpoint["peer"] not in existing, "new veth name collision")
        self.ip("link", "add", "name", endpoint["host"], "type", "veth", "peer", "name", endpoint["peer"])
        initial = veth_pair_snapshot(endpoint, self.links())
        require(all(not initial[side]["alias"] for side in ("host", "peer")), "new veth unexpectedly has an alias")
        endpoint["createdPair"] = initial
        endpoint["ifindex"] = initial["host"]["ifindex"]
        endpoint["confirmedAliases"] = []
        endpoint["aliasIntent"] = None
        endpoint["aliasesVerified"] = False
        self.r.event({"phase": "created-veth-pair-receipt", "pair": initial,
                      "runId": self.a.run_id, "initializationSettled": False})
        # Normal device initialization can change generated MACs after add.
        # Await the existing udev queue, without changing any udev configuration.
        self.r.command([self.tools["udevadm"], "settle", "--timeout=5"], timeout=7)
        settled = veth_pair_snapshot(endpoint, self.links())
        for side in ("host", "peer"):
            require(settled[side]["name"] == initial[side]["name"] and
                    settled[side]["ifindex"] == initial[side]["ifindex"] and not settled[side]["alias"],
                    "new veth pair changed during initialization")
        self.r.event({"phase": "settled-veth-pair-receipt", "initialPair": initial, "pair": settled,
                      "runId": self.a.run_id, "MACChangeCause": "not_attributed"})
        endpoint["createdPair"] = settled
        expected = {"host": {""}, "peer": {""}}
        aliases = {"host": endpoint["alias"], "peer": "virmill-packet:" + self.a.run_id + ":peer"}
        for side in ("host", "peer"):
            self.check_created_pair(endpoint, self.links(), expected)
            endpoint["aliasIntent"] = side
            self.r.event({"phase": "veth-alias-intent", "side": side, "name": endpoint[side],
                          "ifindex": settled[side]["ifindex"], "address": settled[side]["address"],
                          "alias": aliases[side]})
            self.ip("link", "set", "dev", endpoint[side], "alias", aliases[side])
            expected[side] = {aliases[side]}
            self.check_created_pair(endpoint, self.links(), expected)
            endpoint["confirmedAliases"].append(side)
            endpoint["aliasIntent"] = None
            self.r.event({"phase": "veth-alias-verified", "side": side, "name": endpoint[side], "alias": aliases[side]})
        endpoint["aliasesVerified"] = True

    def forwarding_state(self, endpoint):
        host = Path("/sys/class/net", endpoint["host"])
        bridge = Path("/sys/class/net", self.a.bridge)
        read = lambda path: path.read_text(encoding="ascii").removesuffix("\n")
        original = endpoint["createdPair"]["host"]
        require(read(host / "ifindex") == str(original["ifindex"]) and
                read(host / "address") == original["address"] and read(host / "ifalias") == endpoint["alias"],
                "owned bridge port identity changed while awaiting forwarding")
        require(read(host / "master/ifindex") == str(self.before["ifindex"]) and
                read(bridge / "ifindex") == str(self.before["ifindex"]) and
                read(bridge / "address") == self.before["address"],
                "owned bridge port master changed while awaiting forwarding")
        state = read(host / "brport/state")
        require(state in ("0", "1", "2", "3", "4"), "unknown bridge port state")
        return int(state)

    def check_attached_endpoints(self, endpoint):
        host = json.loads(self.ip("-j", "-d", "link", "show", "dev", endpoint["host"])["stdout"])
        peer = json.loads(self.ns(endpoint, [self.tools["ip"], "-j", "-d", "link", "show", "dev", endpoint["peer"]])["stdout"])
        for side, rows in (("host", host), ("peer", peer)):
            require(len(rows) == 1, "attached veth endpoint missing or ambiguous")
            current, original = rows[0], endpoint["createdPair"][side]
            address = original["address"] if side == "host" else endpoint["mac"]
            alias = endpoint["alias"] if side == "host" else "virmill-packet:" + self.a.run_id + ":peer"
            require(current.get("ifname") == endpoint[side] and current.get("ifindex") == original["ifindex"] and
                    current.get("address") == address and current.get("ifalias") == alias and
                    current.get("linkinfo", {}).get("info_kind") == "veth" and "UP" in current.get("flags", []),
                    "attached veth endpoint identity, alias, MAC or administrative state differs")

    def wait_forwarding(self, endpoint):
        start = time.monotonic()
        deadline = min(start + 35, self.r.deadline)
        observations = []
        result = {"status": "failed", "host": endpoint["host"], "requiredState": BR_STATE_FORWARDING,
                  "timeoutSeconds": 35, "states": observations, "STPChanged": False}
        try:
            require(endpoint.get("aliasesVerified"), "forwarding wait requires verified endpoint aliases")
            self.check_attached_endpoints(endpoint)
            while time.monotonic() < deadline and len(observations) < 142:
                state = self.forwarding_state(endpoint)
                observed = {"state": state, "elapsedSeconds": round(time.monotonic() - start, 3)}
                observations.append(observed)
                self.r.event({"phase": "bridge-port-forwarding-observation", "host": endpoint["host"], **observed})
                if state == BR_STATE_FORWARDING:
                    self.check_attached_endpoints(endpoint)
                    require(self.bridge_state() == self.before, "bridge changed while awaiting forwarding")
                    self.netxml()
                    result["status"] = "forwarding"
                    return
                time.sleep(min(0.25, max(0, deadline - time.monotonic())))
            raise Refusal("owned bridge port did not reach forwarding within the bounded wait")
        except Exception as exc:
            result["error"] = str(exc)
            raise
        finally:
            self.r.save("bridge-forwarding-" + endpoint["role"] + ".json", result)

    def create(self):
        require(self.bridge_state() == self.before, "bridge changed before namespace creation")
        self.netxml()
        self.r.save("mutation-intent.json", {"runId": self.a.run_id, "networkId": self.a.network_id,
                                              "networkXMLSHA256": self.a.network_xml_sha256,
                                              "objects": self.endpoints, "cleanup": "only exact owned veth/netns"})
        for endpoint in self.endpoints:
            self.ip("netns", "add", endpoint["namespace"])
            endpoint["nsfd"] = os.open("/run/netns/" + endpoint["namespace"], os.O_RDONLY | os.O_CLOEXEC)
            state = os.fstat(endpoint["nsfd"])
            endpoint["nsino"], endpoint["nsdev"] = state.st_ino, state.st_dev
            self.create_veth_pair(endpoint)
            self.ip("link", "set", "dev", endpoint["peer"], "address", endpoint["mac"])
            self.check_created_pair(endpoint, self.links(),
                                    {"host": {endpoint["alias"]}, "peer": {"virmill-packet:" + self.a.run_id + ":peer"}},
                                    peer_mac=endpoint["mac"])
            self.ip("link", "set", "dev", endpoint["peer"], "netns", endpoint["namespace"])
            require(self.bridge_state() == self.before, "bridge changed before attaching owned veth")
            self.netxml()
            self.ip("link", "set", "dev", endpoint["host"], "master", self.a.bridge)
            self.ip("link", "set", "dev", endpoint["host"], "up")
            self.ns(endpoint, [self.tools["ip"], "link", "set", "dev", "lo", "up"])
            self.ns(endpoint, [self.tools["ip"], "link", "set", "dev", endpoint["peer"], "up"])
            self.wait_forwarding(endpoint)
            if self.a.dhcp == "on":
                lease = self.worker(endpoint, "dhcp", "--network", str(self.network), "--gateway", self.gateway,
                                    *[part for low, high in self.ranges for part in ("--range", str(low) + "," + str(high))],
                                    *(["--dhcp-hostname", endpoint["dhcpHostname"]] if self.a.dns_own_lease else []))
                self.r.save("dhcp-" + endpoint["role"] + ".json", lease)
                if self.a.dns_own_lease:
                    require(lease.get("requestedHostname") == endpoint["dhcpHostname"], "DHCP worker hostname receipt mismatch")
                endpoint["address"] = validate_lease(lease["ack"], self.network, self.gateway, self.ranges)
                endpoint["lease"] = lease
                require(not any(item is not endpoint and item.get("address") == endpoint["address"]
                                for item in self.endpoints), "server assigned duplicate fixture leases")
            self.ns(endpoint, [self.tools["ip"], "address", "add", endpoint["address"] + "/" + str(self.network.prefixlen),
                               "dev", endpoint["peer"]])
        require(self.endpoints[0]["address"] != self.endpoints[1]["address"], "server assigned duplicate fixture leases")

    def host_probe(self, endpoint):
        token = "virmill-packet:" + self.a.run_id
        protected = self.a.host_access != "allow"
        target = self.a.host4_target if self.a.kind == "guest-only" else self.gateway
        if protected:
            self.direct_neighbor(endpoint, target)
        stop = threading.Event()
        accepted = []
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
            # Protected probes need a real host-local positive control, which
            # cannot enter a listener constrained to ingress on the bridge.
            # Bind only the selected, already assigned address and ephemeral port.
            if not protected:
                listener.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, self.a.bridge.encode() + b"\0")
            listener.bind((target, 0))
            listener.listen(4)
            listener.settimeout(0.2)
            port = listener.getsockname()[1]
            self.r.event({"phase": "host-listener", "bind": target, "device": None if protected else self.a.bridge,
                          "port": port, "listening": bool(listener.getsockopt(socket.SOL_SOCKET, socket.SO_ACCEPTCONN))})

            def serve():
                while not stop.is_set() and len(accepted) < 4:
                    try:
                        conn, address = listener.accept()
                    except socket.timeout:
                        continue
                    with conn:
                        conn.settimeout(1)
                        accepted.append(address[0])
                        try:
                            conn.sendall(token.encode("ascii"))
                        except OSError:
                            pass
            thread = threading.Thread(target=serve, daemon=True)
            thread.start()
            try:
                controls = []
                if protected:
                    controls.append(host_tcp_control(target, port, token))
                    self.r.event({"phase": "host-listener-control-before", "result": controls[-1]})
                    require(tcp_expectation(controls[-1], True), "host listener positive control failed before namespace probe")
                result = self.worker(endpoint, "tcp", "--target", target, "--port", str(port), "--token", token)
                if protected:
                    controls.append(host_tcp_control(target, port, token))
                    self.r.event({"phase": "host-listener-control-after", "result": controls[-1]})
                    require(tcp_expectation(controls[-1], True), "host listener positive control failed after namespace probe")
            finally:
                stop.set()
                thread.join(timeout=2)
            require(not thread.is_alive(), "host listener failed to terminate")
        result.update({"listenerBoundOnlyToSelectedBridge": not protected, "listenerAccepts": accepted,
                       "listenerClosed": True, "hostAccessExpected": self.a.host_access,
                       "positiveControls": controls, "explicitL2Neighbor": protected,
                       "expected": "not_connected" if protected else "connected",
                       "negativeResultProvesFiltering": False})
        return result

    def forward_probe(self, endpoint):
        target = str(canonical_ip(self.a.forward4_target, 4))
        require(ipaddress.IPv4Address(target) not in self.network and
                not ipaddress.IPv4Address(target).is_link_local, "forward target must be outside fixture subnet")
        protected = self.a.host_access != "allow"
        controls = []
        if protected:
            controls.append(host_tcp_control(target, self.a.forward4_port))
            self.r.event({"phase": "forward-control-before", "result": controls[-1]})
            require(tcp_expectation(controls[-1], True), "external forward target positive control failed before probe")
        if self.a.kind == "guest-only":
            self.direct_neighbor(endpoint, target)
        else:
            self.ns(endpoint, [self.tools["ip"], "route", "add", target + "/32", "via", self.gateway, "dev", endpoint["peer"]])
        result = self.worker(endpoint, "tcp", "--target", target, "--port", str(self.a.forward4_port))
        if protected:
            controls.append(host_tcp_control(target, self.a.forward4_port))
            self.r.event({"phase": "forward-control-after", "result": controls[-1]})
            require(tcp_expectation(controls[-1], True), "external forward target positive control failed after probe")
        result.update({"routeOrigin": "explicit_fixture_target_route_not_DHCP_default",
                       "expected": "connected" if self.a.kind == "nat" else "not_connected",
                       "positiveControls": controls, "explicitL2Neighbor": protected,
                       "negativeResultProvesFiltering": False})
        return result

    def observe(self):
        a, b = self.endpoints
        result = {"scope": "namespace-packet-observations", "realGuestQualified": False,
                  "multiNICGuestRoutesVerified": False, "servicesOnlyEnforcementQualified": False,
                  "fullNetworkAcceptance": False, "kind": self.a.kind, "dhcp": self.a.dhcp,
                  "hostAccess": self.a.host_access,
                  "forwardIPv4": {"status": "not_requested"},
                  "forwardIPv6": {"status": "not_requested", "reason": "current native profile declares IPv6 disabled"},
                  "configuredDNS": {"status": "not_tested"}}
        self.observation_boundary("before packet observations")
        if self.a.host_access != "allow" and self.a.dhcp == "off":
            result["disabledDHCP"] = self.worker(a, "dhcp-absence")
        if self.a.dhcp == "on":
            result["dhcpRouterOptions"] = [{"role": item["role"],
                                            "present": item["lease"]["ack"]["routerOptionPresent"],
                                            "routers": item["lease"]["ack"]["routers"],
                                            "alternativeRouteOptions": [code for code in (121, 249)
                                                                         if code in item["lease"]["ack"]["optionCodes"]]}
                                           for item in self.endpoints]
            result["dhcpRouterExpectationMet"] = all(
                (not item["present"] and not item["alternativeRouteOptions"] if self.a.kind == "lab"
                 else item["routers"] == [self.gateway] and not item["alternativeRouteOptions"])
                for item in result["dhcpRouterOptions"])
        else:
            result["dhcpRouterExpectationMet"] = None
        ping = self.ns(a, [self.tools["ping"], "-4", "-n", "-c", "2", "-W", "2", b["address"]], okay=(0, 1))
        result["peerIPv4"] = {"status": "connected" if ping["returncode"] == 0 else "not_connected",
                              "source": a["address"], "target": b["address"],
                              "addressOrigin": "own_DHCP_ACKs" if self.a.dhcp == "on" else "explicit_static"}
        result["hostTCP"] = self.host_probe(a)
        self.observation_boundary("after host probe")
        if self.a.dns_name or self.a.dns_own_lease:
            name = b["dhcpHostname"] if self.a.dns_own_lease else self.a.dns_name
            result["configuredDNS"] = self.worker(a, "dns", "--target", self.gateway, "--dns-name", name)
            result["configuredDNS"]["mode"] = "own-lease" if self.a.dns_own_lease else "explicit-name"
            if self.a.dns_own_lease:
                result["configuredDNS"].update(own_lease_dns_expectation(result["configuredDNS"], b, self.gateway))
            result["configuredDNS"]["advertisedByOwnACK"] = (
                self.a.dhcp == "on" and self.gateway in a["lease"]["ack"]["dns"])
        if self.a.forward4_target:
            result["forwardIPv4"] = self.forward_probe(a)
        self.observation_boundary("after IPv4 probes")
        time.sleep(2)  # bounded DAD wait in the generated namespaces only
        for endpoint in self.endpoints:
            addrs = json.loads(self.ns(endpoint, [self.tools["ip"], "-j", "address", "show", "dev", endpoint["peer"]])["stdout"])
            endpoint["linkLocal"] = next((item["local"] for item in addrs[0]["addr_info"]
                                           if item["family"] == "inet6" and item["scope"] == "link" and
                                           not item.get("tentative", False) and not item.get("dadfailed", False)), None)
        if a["linkLocal"] and b["linkLocal"]:
            ping6 = self.ns(a, [self.tools["ping"], "-6", "-n", "-I", a["peer"], "-c", "2", "-W", "2", b["linkLocal"]], okay=(0, 1))
            result["peerIPv6LinkLocal"] = {"status": "connected" if ping6["returncode"] == 0 else "not_connected",
                                             "source": a["linkLocal"], "target": b["linkLocal"]}
        else:
            result["peerIPv6LinkLocal"] = {"status": "unavailable", "reason": "one or both fixture interfaces lack a ready link-local address"}
        result["routerAdvertisements"] = self.worker(a, "ra", *(["--source", a["linkLocal"]] if a["linkLocal"] else []))
        result["ipv6DisabledContradictedByObservedTraffic"] = (
            result["peerIPv6LinkLocal"]["status"] == "connected" or bool(result["routerAdvertisements"]["advertisements"]))
        result["ipv6IsolationProven"] = False
        result["namespaceRoutes"] = {}
        for endpoint in self.endpoints:
            result["namespaceRoutes"][endpoint["role"]] = {
                family: json.loads(self.ns(endpoint, [self.tools["ip"], "-j", family, "route", "show", "table", "all"])["stdout"])
                for family in ("-4", "-6")}
        result["ipv4ChecksMet"] = (result["peerIPv4"]["status"] == "connected" and
                                    tcp_expectation(result["hostTCP"], self.a.host_access == "allow")
                                    and result["dhcpRouterExpectationMet"] is not False)
        if self.a.forward4_target:
            result["ipv4ChecksMet"] &= tcp_expectation(result["forwardIPv4"], self.a.kind == "nat")
        if self.a.dns_name or self.a.dns_own_lease:
            result["ipv4ChecksMet"] &= result["configuredDNS"]["status"] == "resolved"
            if self.a.dns_own_lease:
                result["ipv4ChecksMet"] &= result["configuredDNS"]["ownLeaseExpectationMet"]
            if self.a.host_access == "services-only":
                result["ipv4ChecksMet"] &= result["configuredDNS"]["advertisedByOwnACK"]
        if "disabledDHCP" in result:
            result["ipv4ChecksMet"] &= result["disabledDHCP"]["status"] == "none_seen_in_bounded_window"
        self.observation_boundary("after packet observations")
        result["bridgeL3UnchangedAtCheckpoints"] = True
        result["guestOnlyNoHostL3AtCheckpoints"] = not self.before["addresses"] if self.a.kind == "guest-only" else None
        return result

    def cleanup(self):
        errors = []
        self.r.deadline = time.monotonic() + 60  # cleanup gets its own bounded opportunity
        for endpoint in reversed(self.endpoints):
            veth_safe = True
            try:
                links = {item["ifname"]: item for item in self.links()}
                current = links.get(endpoint["host"])
                if current:
                    require("createdPair" in endpoint, "unknown veth creation acknowledgement; manual review required")
                    if endpoint.get("aliasesVerified"):
                        original = endpoint["createdPair"]["host"]
                        require(current["ifindex"] == original["ifindex"] and current.get("address") == original["address"] and
                                current.get("ifalias") == endpoint["alias"] and current.get("linkinfo", {}).get("info_kind") == "veth",
                                "unknown or replaced veth; manual review required")
                    else:
                        expected = {"host": {""}, "peer": {""}}
                        for side in ("host", "peer"):
                            alias = endpoint["alias"] if side == "host" else "virmill-packet:" + self.a.run_id + ":peer"
                            if side in endpoint.get("confirmedAliases", []):
                                expected[side] = {alias}
                            elif endpoint.get("aliasIntent") == side:
                                expected[side].add(alias)  # only the exact, already journaled transition
                        self.check_created_pair(endpoint, list(links.values()), expected)
                    self.ip("link", "delete", "dev", endpoint["host"])
                    remaining = {item["ifname"] for item in self.links()}
                    require(endpoint["host"] not in remaining and endpoint["peer"] not in remaining,
                            "veth names remain after deletion; manual review required")
                elif "ifindex" in endpoint:
                    require(endpoint["peer"] not in links and not any(item["ifindex"] == endpoint["ifindex"] for item in links.values()),
                            "veth disappeared under its expected name but pair/index remains; manual review required")
                    self.r.event({"phase": "cleanup", "host": endpoint["host"], "status": "already_absent"})
            except Exception as exc:
                veth_safe = False
                errors.append(endpoint["host"] + ": " + str(exc))
            try:
                path = Path("/run/netns", endpoint["namespace"])
                if os.path.lexists(path):
                    require(veth_safe, "namespace retained because veth ownership is ambiguous")
                    state = path.lstat()
                    require("nsfd" in endpoint and not stat.S_ISLNK(state.st_mode) and
                            (state.st_dev, state.st_ino) == (endpoint["nsdev"], endpoint["nsino"]),
                            "unknown or replaced namespace; manual review required")
                    pids = self.ip("netns", "pids", endpoint["namespace"])["stdout"].strip()
                    require(not pids, "namespace contains processes; no unrelated process killing")
                    self.ip("netns", "delete", endpoint["namespace"])
            except Exception as exc:
                errors.append(endpoint["namespace"] + ": " + str(exc))
            finally:
                if "nsfd" in endpoint:
                    os.close(endpoint.pop("nsfd"))
        if self.before is not None:
            try:
                self.netxml()
                require(self.bridge_state() == self.before, "selected bridge identity/L3 changed")
                require(not json.loads(self.ip("-j", "link", "show", "master", self.a.bridge)["stdout"]),
                        "bridge has remaining ports; manual review required")
            except Exception as exc:
                errors.append(str(exc))
        return {"status": "verified" if not errors else "manual_review_required", "errors": errors,
                "DHCPLeasesRetained": self.a.dhcp == "on", "networkRemoved": False}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    run = sub.add_parser("run", help="parent-only execution on approved disposable host")
    run.add_argument("--execute-reviewed", action="store_true", required=True)
    run.add_argument("--confirm-new-unused-network", action="store_true", required=True)
    run.add_argument("--network-id", required=True)
    run.add_argument("--root", type=selected_root, required=True)
    run.add_argument("--network-xml-sha256", required=True)
    run.add_argument("--bridge", required=True)
    run.add_argument("--kind", choices=("nat", "lab", "guest-only"), required=True)
    run.add_argument("--host-access", choices=("allow", "services-only", "deny"), required=True)
    run.add_argument("--dhcp", choices=("on", "off"), required=True)
    run.add_argument("--static-a4")
    run.add_argument("--static-b4")
    run.add_argument("--static-cidr4", help="guest-only logical RFC1918 subnet; assigns no host address")
    run.add_argument("--host4-target", help="guest-only explicit already assigned host IPv4 outside the logical subnet")
    run.add_argument("--forward4-target")
    run.add_argument("--forward4-port", type=int)
    dns_modes = run.add_mutually_exclusive_group()
    dns_modes.add_argument("--dns-name", help="explicit A query through only this bridge gateway; may cause upstream DNS traffic")
    dns_modes.add_argument("--dns-own-lease", action="store_true", help="request generated DHCP hostnames and resolve role B's exact own lease locally")
    run.add_argument("--output", type=Path, required=True)
    worker = sub.add_parser("worker", help=argparse.SUPPRESS)
    worker.add_argument("--worker", choices=("dhcp", "dhcp-absence", "ra", "tcp", "dns"), required=True)
    worker.add_argument("--interface", required=True)
    worker.add_argument("--host-netns-inode", type=int, required=True)
    worker.add_argument("--source")
    worker.add_argument("--target")
    worker.add_argument("--port", type=int)
    worker.add_argument("--token")
    worker.add_argument("--network")
    worker.add_argument("--gateway")
    worker.add_argument("--range", action="append", default=[])
    worker.add_argument("--dns-name")
    worker.add_argument("--dhcp-hostname")
    for command in (run, worker):
        command.add_argument("--run-id", required=True)
        command.add_argument("--recipe-sha256", required=True)
    args = parser.parse_args(argv)
    canonical_uuid(args.run_id)
    require(re.fullmatch(r"[0-9a-f]{64}", args.recipe_sha256), "source SHA256 syntax invalid")
    source = Path(__file__).absolute()
    ordinary_path(source)
    require(digest(source.read_bytes()) == args.recipe_sha256, "source SHA256 mismatch")
    if args.command == "worker":
        mac = worker_guard(args)
        if args.dhcp_hostname is not None:
            require(args.worker == "dhcp" and args.dhcp_hostname == lease_hostname(args.run_id, args.interface[2]),
                    "worker DHCP hostname must match this run and endpoint role")
        result = {"dhcp": dhcp_worker, "dhcp-absence": dhcp_absence_worker, "ra": ra_worker}.get(args.worker)
        result = result(args, mac) if result else dns_worker(args) if args.worker == "dns" else tcp_worker(args)
        print(json.dumps(result, sort_keys=True))
        return 0
    require(os.geteuid() == 0, "only parent-operated root on the explicitly approved disposable host may execute")
    canonical_uuid(args.network_id)
    require(args.output == args.root / ("network-packet-" + args.run_id),
            "single-use output must be the approved run root/network-packet-RUN_UUID")
    require(re.fullmatch(r"[0-9a-f]{64}", args.network_xml_sha256), "XML SHA256 syntax invalid")
    require(re.fullmatch(r"vm[0-9a-f]{12}", args.bridge), "bridge grammar refused")
    validate_profile(args)
    recorder = Recorder(args.output, args.root)
    fixture, observation, error = None, None, None
    recorder.save("invocation.json", {**vars(args), "root": str(args.root), "output": str(args.output), "source": str(source),
                                       "python": sys.version, "scope": "new-network namespace fixture only"})
    original_handlers = {}
    def interrupted(signum, _frame):
        raise Refusal("interrupted by signal " + str(signum))
    for signum in (signal.SIGTERM, signal.SIGINT):
        original_handlers[signum] = signal.signal(signum, interrupted)
    try:
        fixture = Fixture(args, recorder, discover(recorder))
        fixture.preflight()
        fixture.create()
        observation = fixture.observe()
        recorder.save("observations.json", observation)
    except Exception as exc:
        error = type(exc).__name__ + ": " + str(exc)
    finally:
        # A second termination requests immediate operator intervention; no forced
        # cleanup of objects whose acknowledgement/identity was lost.
        for signum in original_handlers:
            signal.signal(signum, signal.SIG_IGN)
        cleanup = fixture.cleanup() if fixture else {"status": "not_created", "errors": []}
        passed = (error is None and observation is not None and observation["ipv4ChecksMet"] and
                  not observation["ipv6DisabledContradictedByObservedTraffic"] and cleanup["status"] == "verified")
        report = {"status": "scoped_checks_passed" if passed else "failed_or_policy_discrepancy",
                  "error": error, "runId": args.run_id, "networkId": args.network_id,
                  "recipeSHA256": args.recipe_sha256, "networkXMLSHA256": args.network_xml_sha256,
                  "cleanup": cleanup, "observations": observation, "fullNetworkAcceptance": False,
                  "guestOrHardwareQualification": False}
        recorder.save("report.json", report)
        recorder.journal.close()
        for signum, previous in original_handlers.items():
            signal.signal(signum, previous)
    print(json.dumps({"status": report["status"], "report": str(args.output / "report.json")}, sort_keys=True))
    return 0 if passed else 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (Refusal, ValueError, OSError) as exc:
        print(json.dumps({"status": "failed", "error": str(exc)}, sort_keys=True), file=sys.stderr)
        raise SystemExit(1)
