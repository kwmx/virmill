#!/usr/bin/env python3
"""Parent-operated, single-use packet observations on a NEW disposable network.

Importing this module performs no native operations. See docs/network-packet-fixtures.md.
"""

import argparse
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


APPROVED_PARENT = Path("/home/virmill-test/virmill-tests")
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


def dhcp_request(mac, xid, offered=None, server=None):
    bootp = struct.pack("!BBBBIHH4s4s4s4s16s64s128s", 1, 1, 6, 0, xid, 0, 0x8000,
                        bytes(4), bytes(4), bytes(4), bytes(4), mac + bytes(10), bytes(64), bytes(128))
    options = b"\x35\x01" + bytes([3 if offered else 1]) + b"\x3d\x07\x01" + mac
    options += b"\x37\x06\x01\x03\x06\x33\x36\x79"
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
    addresses = []
    for index in range(an + ns + ar):
        _, pos = read_name(pos)
        require(pos + 10 <= len(packet), "truncated DNS record")
        kind, family, _ttl, size = struct.unpack("!HHIH", packet[pos:pos + 10])
        pos += 10
        require(pos + size <= len(packet), "truncated DNS record data")
        if index < an and kind == 1 and family == 1:
            require(size == 4, "invalid DNS A record")
            addresses.append(str(ipaddress.IPv4Address(packet[pos:pos + size])))
        pos += size
    require(pos == len(packet), "trailing DNS response bytes")
    rcode = flags & 15
    return {"status": "resolved" if rcode == 0 and addresses else "no_A_answer", "rcode": rcode,
            "addresses": addresses, "responseSHA256": digest(packet), "responseHex": packet.hex()}


def native_network(raw, network_id, bridge, kind, dhcp):
    require(len(raw) <= 65536 and not re.search(br"<!(?!--)", raw) and b"<?" not in raw,
            "oversized XML or XML declarations/entities refused")
    root = ET.fromstring(raw)
    require(root.tag == "network" and len(root.findall("uuid")) == 1 and root.findtext("uuid") == network_id,
            "native network UUID mismatch")
    require(len(root.findall("name")) == 1 and root.findtext("name") == "virmill-" + network_id,
            "network is not the new Virmill name")
    markers = root.findall("metadata/{urn:virmill:v1}networkCreation")
    require(len(markers) == 1 and markers[0].get("apiVersion") == "virmill/v1" and
            markers[0].get("version") == "1" and re.fullmatch(r"[0-9a-f]{64}", markers[0].get("intent", "")),
            "missing current Virmill creation marker; marker is not an ownership grant")
    require(bridge == "vm" + canonical_uuid(network_id).hex[:12] and re.fullmatch(r"vm[0-9a-f]{12}", bridge),
            "bridge is not the UUID-bound Virmill bridge")
    bridges = root.findall("bridge")
    require(len(bridges) == 1 and bridges[0].get("name") == bridge, "native bridge mismatch")
    forwards = root.findall("forward")
    require((kind == "nat" and len(forwards) == 1 and forwards[0].get("mode", "nat") == "nat") or
            (kind == "lab" and not forwards), "native forwarding mode mismatch")
    # These two omitted defaults are the same equivalences used by the parent's
    # networkxml.Match. They say nothing about actual bridge-frame filtering.
    require(root.get("ipv6") in (None, "no"), "this fixture profile requires declared IPv6 disabled")
    ips = root.findall("ip")
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
    with socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0800)) as sock:
        sock.setsockopt(263, 8, 1)  # SOL_PACKET / PACKET_AUXDATA, Linux uapi.
        sock.bind((args.interface, 0))
        sock.settimeout(0.25)
        request = dhcp_request(mac, xid)
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
                    request = dhcp_request(mac, xid, reply["address"], reply["server"])
                    break
                require(reply["server"] == offers[0]["server"] and reply["address"] == offers[0]["address"],
                        "DHCP ACK differs from selected offer")
                return {"status": "acknowledged", "xid": xid, "offer": offers[0], "ack": reply,
                        "malformedFrames": malformed, "transmitted": transmitted}
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


def tcp_worker(args):
    address = ipaddress.ip_address(args.target)
    family = socket.AF_INET if address.version == 4 else socket.AF_INET6
    try:
        with socket.socket(family, socket.SOCK_STREAM) as sock:
            sock.settimeout(3)
            sock.connect((str(address), args.port))
            if args.token:
                received = bytearray()
                while len(received) < len(args.token):
                    part = sock.recv(256 - len(received))
                    if not part:
                        break
                    received.extend(part)
                require(received.decode("ascii") == args.token, "host listener token mismatch")
        return {"status": "connected", "target": args.target, "port": args.port}
    except OSError as exc:
        return {"status": "not_connected", "errno": exc.errno, "reason": str(exc),
                "target": args.target, "port": args.port, "filterEnforcementProven": False}


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
                                                  self.a.kind, self.a.dhcp)
        self.before = self.bridge_state()
        require(("inet", gateway, network.prefixlen, "global") in self.before["addresses"],
                "bridge gateway differs from pinned XML")
        members = json.loads(self.ip("-j", "link", "show", "master", self.a.bridge)["stdout"])
        require(not members, "selected NEW bridge already has ports; no guest/shared bridge testing")
        existing = {item["ifname"] for item in self.links()}
        run = canonical_uuid(self.a.run_id).hex
        for role in ("a", "b"):
            endpoint = {"role": role, "namespace": "virmill-packet-" + run + "-" + role,
                        "host": "vp" + role + run[:10], "peer": "ve" + role + run[:10],
                        "alias": "virmill-packet:" + self.a.run_id + ":host-" + role,
                        "mac": "02:" + ":".join(run[i:i + 2] for i in (0, 2, 4, 6)) + (":0a" if role == "a" else ":0b")}
            require(endpoint["host"] not in existing and endpoint["peer"] not in existing,
                    "generated veth name already exists")
            require(not os.path.lexists("/run/netns/" + endpoint["namespace"]), "generated namespace already exists")
            self.endpoints.append(endpoint)
        if self.a.dhcp == "off":
            require(self.a.static_a4 and self.a.static_b4, "DHCP-off requires both explicit static addresses")
            for endpoint, address in zip(self.endpoints, (self.a.static_a4, self.a.static_b4)):
                parsed = canonical_ip(address, 4)
                require(parsed in network and parsed not in (network.network_address, network.broadcast_address,
                                                             ipaddress.IPv4Address(gateway)), "unsafe static endpoint")
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
                                    *[part for low, high in self.ranges for part in ("--range", str(low) + "," + str(high))])
                self.r.save("dhcp-" + endpoint["role"] + ".json", lease)
                endpoint["address"] = validate_lease(lease["ack"], self.network, self.gateway, self.ranges)
                endpoint["lease"] = lease
                require(not any(item is not endpoint and item.get("address") == endpoint["address"]
                                for item in self.endpoints), "server assigned duplicate fixture leases")
            self.ns(endpoint, [self.tools["ip"], "address", "add", endpoint["address"] + "/" + str(self.network.prefixlen),
                               "dev", endpoint["peer"]])
        require(self.endpoints[0]["address"] != self.endpoints[1]["address"], "server assigned duplicate fixture leases")

    def host_probe(self, endpoint):
        token = "virmill-packet:" + self.a.run_id
        stop = threading.Event()
        accepted = []
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, self.a.bridge.encode() + b"\0")
            listener.bind((self.gateway, 0))
            listener.listen(2)
            listener.settimeout(0.2)
            port = listener.getsockname()[1]
            self.r.event({"phase": "host-listener", "bind": self.gateway, "device": self.a.bridge,
                          "port": port, "listening": bool(listener.getsockopt(socket.SOL_SOCKET, socket.SO_ACCEPTCONN))})

            def serve():
                while not stop.is_set() and len(accepted) < 2:
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
                result = self.worker(endpoint, "tcp", "--target", self.gateway, "--port", str(port), "--token", token)
            finally:
                stop.set()
                thread.join(timeout=2)
            require(not thread.is_alive(), "host listener failed to terminate")
        result.update({"listenerBoundOnlyToSelectedBridge": True, "listenerAccepts": accepted,
                       "listenerClosed": True, "hostAccessExpected": "allow"})
        return result

    def observe(self):
        a, b = self.endpoints
        result = {"scope": "namespace-packet-observations", "realGuestQualified": False,
                  "multiNICGuestRoutesVerified": False, "servicesOnlyEnforcementQualified": False,
                  "fullNetworkAcceptance": False, "kind": self.a.kind, "dhcp": self.a.dhcp,
                  "forwardIPv4": {"status": "not_requested"},
                  "forwardIPv6": {"status": "not_requested", "reason": "current native profile declares IPv6 disabled"},
                  "configuredDNS": {"status": "not_tested"}}
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
        if self.a.dns_name:
            result["configuredDNS"] = self.worker(a, "dns", "--target", self.gateway, "--dns-name", self.a.dns_name)
            result["configuredDNS"]["advertisedByOwnACK"] = (
                self.a.dhcp == "on" and self.gateway in a["lease"]["ack"]["dns"])
        if self.a.forward4_target:
            target = canonical_ip(self.a.forward4_target, 4)
            require(target not in self.network and not target.is_link_local, "forward target must be outside fixture subnet")
            self.ns(a, [self.tools["ip"], "route", "add", str(target) + "/32", "via", self.gateway, "dev", a["peer"]])
            result["forwardIPv4"] = self.worker(a, "tcp", "--target", str(target), "--port", str(self.a.forward4_port))
            result["forwardIPv4"].update({"routeOrigin": "explicit_fixture_target_route_not_DHCP_default",
                                           "expected": "connected" if self.a.kind == "nat" else "not_connected",
                                           "negativeResultProvesFiltering": False})
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
        result["ipv4ChecksMet"] = (result["peerIPv4"]["status"] == "connected" and result["hostTCP"]["status"] == "connected"
                                    and result["dhcpRouterExpectationMet"] is not False)
        if self.a.forward4_target:
            result["ipv4ChecksMet"] &= result["forwardIPv4"]["status"] == result["forwardIPv4"]["expected"]
        if self.a.dns_name:
            result["ipv4ChecksMet"] &= result["configuredDNS"]["status"] == "resolved"
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
    run.add_argument("--kind", choices=("nat", "lab"), required=True)
    run.add_argument("--host-access", choices=("allow",), required=True)
    run.add_argument("--dhcp", choices=("on", "off"), required=True)
    run.add_argument("--static-a4")
    run.add_argument("--static-b4")
    run.add_argument("--forward4-target")
    run.add_argument("--forward4-port", type=int)
    run.add_argument("--dns-name", help="explicit A query through only this bridge gateway; may cause upstream DNS traffic")
    run.add_argument("--output", type=Path, required=True)
    worker = sub.add_parser("worker", help=argparse.SUPPRESS)
    worker.add_argument("--worker", choices=("dhcp", "ra", "tcp", "dns"), required=True)
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
        result = {"dhcp": dhcp_worker, "ra": ra_worker}.get(args.worker)
        result = result(args, mac) if result else dns_worker(args) if args.worker == "dns" else tcp_worker(args)
        print(json.dumps(result, sort_keys=True))
        return 0
    require(os.geteuid() == 0, "only parent-operated root on the explicitly approved disposable host may execute")
    canonical_uuid(args.network_id)
    require(args.output == args.root / ("network-packet-" + args.run_id),
            "single-use output must be the approved run root/network-packet-RUN_UUID")
    require(re.fullmatch(r"[0-9a-f]{64}", args.network_xml_sha256), "XML SHA256 syntax invalid")
    require(re.fullmatch(r"vm[0-9a-f]{12}", args.bridge), "bridge grammar refused")
    require(bool(args.forward4_target) == bool(args.forward4_port), "forward target and port must be supplied together")
    if args.forward4_target:
        canonical_ip(args.forward4_target, 4)
        require(1 <= args.forward4_port <= 65535, "invalid TCP target port")
    if args.dns_name:
        dns_name(args.dns_name)
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
