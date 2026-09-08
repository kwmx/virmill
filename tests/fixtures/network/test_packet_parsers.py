#!/usr/bin/env python3
"""Pure generated-byte and mocked-command tests: no sockets, namespaces or host changes."""

import importlib.util
import ipaddress
import json
import os
import random
from pathlib import Path
import stat
import struct
import types
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location("packet_fixture", Path(__file__).with_name("network_packet_fixture.py"))
fixture = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(fixture)
MAC = bytes.fromhex("02123456780a")
XID = 0x12345678
UUID = "12345678-1234-4234-8234-123456789abc"


def reply(options=None, message=5, xid=XID, mac=MAC, udp_checksum=False):
    # Independent server-shaped BOOTP fixture, never transmitted.
    payload = bytearray(240)
    payload[:3] = b"\x02\x01\x06"
    payload[4:8] = xid.to_bytes(4, "big")
    payload[16:20] = ipaddress.IPv4Address("192.168.80.21").packed
    payload[28:34] = mac
    payload[236:240] = bytes.fromhex("63825363")
    if options is None:
        options = (bytes([53, 1, message, 54, 4]) + bytes([192, 168, 80, 1]) +
                   bytes([1, 4, 255, 255, 255, 0, 51, 4, 0, 0, 14, 16, 6, 4, 192, 168, 80, 1, 255]))
    payload.extend(options)
    udp = bytearray(struct.pack("!HHHH", 67, 68, 8 + len(payload), 0) + payload)
    source, dest = bytes([192, 168, 80, 1]), bytes([255] * 4)
    if udp_checksum:
        pseudo = source + dest + b"\0\x11" + len(udp).to_bytes(2, "big")
        udp[6:8] = fixture.checksum(pseudo + udp).to_bytes(2, "big")
    header = bytearray(struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(udp), 7, 0, 64, 17, 0, source, dest))
    header[10:12] = fixture.checksum(header).to_bytes(2, "big")
    return b"\xff" * 6 + bytes.fromhex("021111111111") + b"\x08\x00" + header + udp


def changed(frame, offset, value, fix_ip=False):
    data = bytearray(frame)
    data[offset:offset + len(value)] = value
    if fix_ip:
        data[24:26] = bytes(2)
        data[24:26] = fixture.checksum(data[14:34]).to_bytes(2, "big")
    return bytes(data)


def ra(options=b"", lifetime=1800, source="fe80::1", hop=255):
    source = ipaddress.IPv6Address(source).packed
    dest = ipaddress.IPv6Address("ff02::1").packed
    packet = bytearray(struct.pack("!BBHBBHII", 134, 0, 0, 64, 0, lifetime, 0, 0) + options)
    pseudo = source + dest + len(packet).to_bytes(4, "big") + b"\0\0\0\x3a"
    packet[2:4] = fixture.checksum(pseudo + packet).to_bytes(2, "big")
    header = struct.pack("!IHBB", 6 << 28, len(packet), 58, hop) + source + dest
    return b"\x33\x33\0\0\0\x01" + bytes.fromhex("021111111111") + b"\x86\xdd" + header + packet


def network_xml(kind="lab", dhcp=True):
    return ("<network ipv6='no'><name>virmill-" + UUID + "</name><uuid>" + UUID + "</uuid>"
            "<metadata><v:networkCreation xmlns:v='urn:virmill:v1' apiVersion='virmill/v1' version='1' intent='" +
            "a" * 64 + "'/></metadata>" + ("<forward mode='nat'/>" if kind == "nat" else "") +
            "<bridge name='vm123456781234'/><ip family='ipv4' address='192.168.80.1' prefix='24'>" +
            ("<dhcp><range start='192.168.80.2' end='192.168.80.254'/></dhcp>" if dhcp else "") +
            "</ip></network>").encode()


class DHCPTests(unittest.TestCase):
    def test_lab_absent_router_is_distinct_from_nat_router(self):
        lab = fixture.parse_dhcp(reply(), XID, MAC)
        self.assertFalse(lab["routerOptionPresent"])
        self.assertEqual(lab["routers"], [])
        self.assertEqual(lab["dns"], ["192.168.80.1"])
        base = bytes.fromhex(reply().hex())[282:]
        nat = fixture.parse_dhcp(reply(base[:-1] + bytes([3, 4, 192, 168, 80, 1, 255])), XID, MAC)
        self.assertTrue(nat["routerOptionPresent"])
        self.assertEqual(nat["routers"], ["192.168.80.1"])

    def test_unrelated_xid_mac_protocol_and_client_frames_are_not_replies(self):
        for frame in (reply(xid=XID + 1), reply(mac=b"\x02\0\0\0\0\x09"),
                      fixture.dhcp_request(MAC, XID), b"\xff" * 14,
                      changed(reply(), 23, b"\x06", True)):
            with self.subTest(frame=frame[:40].hex()):
                self.assertIsNone(fixture.parse_dhcp(frame, XID, MAC))

    def test_malformed_envelope_is_never_router_absence_success(self):
        valid = reply()
        cases = {"oversized": valid + bytes(2048), "truncated_ip": valid[:30],
                 "bad_ihl": changed(valid, 14, b"\x44"), "bad_checksum": changed(valid, 24, b"\0\0"),
                 "fragment": changed(valid, 20, b"\x20\0", True),
                 "ip_length": changed(valid, 16, b"\xff\xff", True),
                 "udp_length": changed(valid, 38, b"\0\x08"),
                 "operation": changed(valid, 42, b"\x01"), "hardware_length": changed(valid, 44, b"\x05"),
                 "cookie": changed(valid, 278, b"BAD!")}
        for name, frame in cases.items():
            with self.subTest(name=name), self.assertRaises(fixture.Refusal):
                fixture.parse_dhcp(frame, XID, MAC)

    def test_malformed_options_including_hidden_router_are_refused(self):
        base = reply()[282:-1]
        cases = {"no_end": base, "length_missing": base + b"\x03",
                 "truncated_router": base + b"\x03\x04\x01\xff", "zero_router": base + b"\x03\0\xff",
                 "odd_router": base + b"\x03\x01\x01\xff", "duplicate_type": base + b"\x35\x01\x05\xff",
                 "same_value_duplicate_dns": base + b"\x06\x04\xc0\xa8\x50\x01\xff",
                 "overload": base + b"\x34\x01\x01\xff", "trailing": base + b"\xff\x03\x04\x01\x02\x03\x04",
                 "missing_type": b"\x36\x04\xc0\xa8\x50\x01\xff",
                 "invalid_type": bytes([53, 1, 1, 54, 4, 192, 168, 80, 1, 255]),
                 "missing_server": bytes([53, 1, 5, 255])}
        for name, options in cases.items():
            with self.subTest(name=name), self.assertRaises(fixture.Refusal):
                fixture.parse_dhcp(reply(options), XID, MAC)

    def test_checksum_offload_is_explicit_not_mislabeled_wire_validation(self):
        frame = reply(udp_checksum=True)
        self.assertEqual(fixture.parse_dhcp(frame, XID, MAC)["udpChecksumStatus"], "wire")
        bad = changed(frame, 40, b"\x12\x34")
        with self.assertRaisesRegex(fixture.Refusal, "UDP checksum"):
            fixture.parse_dhcp(bad, XID, MAC)
        for status in ("kernel_partial", "kernel_valid"):
            self.assertEqual(fixture.parse_dhcp(bad, XID, MAC, status)["udpChecksumStatus"], status)
        with self.assertRaises(fixture.Refusal):
            fixture.parse_dhcp(frame, XID, MAC, "trust_me")

    def test_lease_checks_gateway_range_mask_and_lifetime(self):
        network = ipaddress.IPv4Network("192.168.80.0/24")
        ranges = [(ipaddress.IPv4Address("192.168.80.2"), ipaddress.IPv4Address("192.168.80.254"))]
        valid = fixture.parse_dhcp(reply(), XID, MAC)
        self.assertEqual(fixture.validate_lease(valid, network, "192.168.80.1", ranges), "192.168.80.21")
        for field, value in (("address", "192.168.81.21"), ("address", "192.168.80.1"),
                             ("address", "192.168.80.255"), ("address", "192.168.80.0"),
                             ("server", "192.168.80.5"), ("subnetMask", "255.255.0.0"),
                             ("leaseSeconds", 299), ("leaseSeconds", None)):
            with self.subTest(field=field, value=value), self.assertRaises((fixture.Refusal, ValueError)):
                fixture.validate_lease({**valid, field: value}, network, "192.168.80.1", ranges)

    def test_request_contains_broadcast_flag_own_identity_and_requested_options(self):
        frame = fixture.dhcp_request(MAC, XID, "192.168.80.21", "192.168.80.1")
        self.assertEqual(frame[:6], bytes([255] * 6))
        self.assertEqual(frame[6:12], MAC)
        self.assertEqual(frame[34:38], b"\0\x44\0\x43")
        self.assertEqual(frame[46:50], XID.to_bytes(4, "big"))
        self.assertEqual(frame[52:54], b"\x80\0")
        self.assertEqual(frame[70:76], MAC)
        options = fixture.parse_options(frame[282:])
        self.assertEqual(options[53], b"\x03")
        self.assertEqual(options[50], ipaddress.IPv4Address("192.168.80.21").packed)
        self.assertIn(3, options[55])


class IPv6Tests(unittest.TestCase):
    def test_ra_records_router_and_prefix_without_absence_claim(self):
        prefix = bytes([3, 4, 64, 192]) + struct.pack("!III", 3600, 1800, 0) + ipaddress.IPv6Address("fd12:3456::").packed
        value = fixture.parse_ra(ra(prefix))
        self.assertEqual(value["source"], "fe80::1")
        self.assertEqual(value["routerLifetime"], 1800)
        self.assertEqual(value["prefixes"][0]["prefix"], "fd12:3456::")
        self.assertEqual(fixture.parse_ra(ra(lifetime=0))["routerLifetime"], 0)

    def test_ra_malformed_and_nonlocal_sources_refused(self):
        for name, frame in {"hop": ra(hop=64), "source": ra(source="fd12::1"),
                            "checksum": changed(ra(), 56, b"\0\0"), "zero_option": ra(b"\x01\0"),
                            "truncated_option": ra(b"\x01\x02" + bytes(6)),
                            "bad_prefix": ra(b"\x03\x01" + bytes(6)), "oversized": ra() + bytes(2048)}.items():
            with self.subTest(name=name), self.assertRaises(fixture.Refusal):
                fixture.parse_ra(frame)

    def test_solicitation_is_linklocal_and_not_parsed_as_advertisement(self):
        frame = fixture.router_solicitation(MAC, "fe80::1234")
        self.assertEqual(frame[:6], bytes.fromhex("333300000002"))
        self.assertEqual(frame[21], 255)
        self.assertEqual(frame[54], 133)
        self.assertIsNone(fixture.parse_ra(frame))
        with self.assertRaises(fixture.Refusal):
            fixture.router_solicitation(MAC, "fd12::1")


class DNSTests(unittest.TestCase):
    def response(self):
        query = fixture.dns_query("example.test", 7)
        header = struct.pack("!HHHHHH", 7, 0x8180, 1, 1, 0, 0)
        return header + query[12:] + bytes.fromhex("c00c000100010000003c0004c0000201")

    def test_valid_dns_response_and_identity(self):
        result = fixture.parse_dns(self.response(), 7, "EXAMPLE.test")
        self.assertEqual(result["status"], "resolved")
        self.assertEqual(result["addresses"], ["192.0.2.1"])
        for name in ("bad..test", "a" * 64 + ".test", "-bad.test", "a\n.test", "*.test"):
            with self.subTest(name=name), self.assertRaises(fixture.Refusal):
                fixture.dns_query(name, 7)

    def test_dns_malformed_and_pointer_cycle_refused(self):
        valid = self.response()
        for name, packet in {"xid": changed(valid, 0, b"\0\x08"), "truncated_flag": changed(valid, 2, b"\x83\x80"),
                             "truncated_record": valid[:-1], "trailing": valid + b"x",
                             "pointer_cycle": changed(valid, 12, b"\xc0\x0c"),
                             "forward_pointer": changed(valid, 12, b"\xc0\x25"),
                             "counts": changed(valid, 6, b"\xff\xff")}.items():
            with self.subTest(name=name), self.assertRaises(fixture.Refusal):
                fixture.parse_dns(packet, 7, "example.test")


class NativeGuardTests(unittest.TestCase):
    def test_run_root_is_an_explicit_canonical_direct_child(self):
        root = "/home/virmill-test/virmill-tests/run-network-generated"
        self.assertEqual(str(fixture.selected_root(root)), root)
        for value in ("/tmp/run-test", "/home/virmill-test/virmill-tests", root + "/nested",
                      root + "/", root + "/../another", root.replace("/run-", "//run-"),
                      root + "\n", " " + root, "relative", root + "\0"):
            with self.subTest(root=value), self.assertRaises(fixture.Refusal):
                fixture.selected_root(value)

    def test_worker_refuses_host_namespace_before_any_socket(self):
        args = types.SimpleNamespace(host_netns_inode=os.stat("/proc/self/ns/net").st_ino)
        with mock.patch.object(fixture.os, "geteuid", return_value=0), \
                mock.patch.object(fixture.socket, "socket", side_effect=AssertionError("socket must not open")), \
                self.assertRaisesRegex(fixture.Refusal, "host namespace"):
            fixture.worker_guard(args)

    def test_exact_three_supported_profiles(self):
        for kind, dhcp in (("nat", True), ("lab", True), ("lab", False)):
            with self.subTest(kind=kind, dhcp=dhcp):
                network, gateway, ranges = fixture.native_network(network_xml(kind, dhcp), UUID,
                                                                  "vm123456781234", kind, "on" if dhcp else "off")
                self.assertEqual(str(network), "192.168.80.0/24")
                self.assertEqual(gateway, "192.168.80.1")
                self.assertEqual(bool(ranges), dhcp)

    def test_unbound_bridge_or_existing_config_shape_refused(self):
        base = network_xml()
        cases = {"bridge": base.replace(b"vm123456781234", b"virbr0"),
                 "name": base.replace(b"virmill-", b"other-"),
                 "uuid_duplicate": base.replace(b"</uuid>", b"</uuid><uuid>" + UUID.encode() + b"</uuid>"),
                 "foreign_marker": base.replace(b"urn:virmill:v1", b"urn:other:v1"),
                 "wrong_marker_version": base.replace(b"version='1'", b"version='2'"),
                 "dhcp_reservation": base.replace(b"</dhcp>", b"<host mac='02:00:00:00:00:01' ip='192.168.80.10'/></dhcp>"),
                 "extra_ip": base.replace(b"</network>", b"<ip family='ipv6' address='fd12::1' prefix='64'/></network>"),
                 "small_pool": base.replace(b"prefix='24'", b"prefix='30'"),
                 "entity": b"<!DOCTYPE x []>" + base}
        for name, raw in cases.items():
            with self.subTest(name=name), self.assertRaises((fixture.Refusal, ValueError)):
                fixture.native_network(raw, UUID, "vm123456781234", "lab", "on")
        for bridge in ("virbr0", "vm123456781234;id", "vm123456781235", "vm12345678123A"):
            with self.subTest(bridge=bridge), self.assertRaises(fixture.Refusal):
                fixture.native_network(base, UUID, bridge, "lab", "on")

    def test_cleanup_never_deletes_replacement_or_unknown_ack_objects(self):
        for owned_index in (None, 10):
            commands = []
            recorder = types.SimpleNamespace(deadline=0, event=lambda event: None)
            args = types.SimpleNamespace(run_id=UUID, dhcp="off")
            instance = fixture.Fixture(args, recorder, {"ip": "/usr/bin/ip"})
            endpoint = {"host": "vpa1234567812", "namespace": "unused-test", "alias": "expected"}
            if owned_index:
                endpoint["ifindex"] = owned_index
            instance.endpoints = [endpoint]
            instance.links = lambda: [{"ifname": endpoint["host"], "ifindex": 999, "ifalias": "expected",
                                       "linkinfo": {"info_kind": "veth"}}]
            instance.ip = lambda *args, **kwargs: commands.append(args)
            with mock.patch.object(fixture.os.path, "lexists", return_value=False):
                result = instance.cleanup()
            self.assertEqual(result["status"], "manual_review_required")
            self.assertEqual(commands, [])

    def test_namespace_rebinding_or_symlink_refuses_exec(self):
        instance = fixture.Fixture(types.SimpleNamespace(), types.SimpleNamespace(), {"ip": "/usr/bin/ip"})
        endpoint = {"namespace": "unused-test", "nsfd": 123, "nsdev": 9, "nsino": 10}
        commands = []
        instance.ip = lambda *args, **kwargs: commands.append(args)
        for state in (types.SimpleNamespace(st_mode=stat.S_IFREG, st_dev=9, st_ino=11),
                      types.SimpleNamespace(st_mode=stat.S_IFREG, st_dev=8, st_ino=10),
                      types.SimpleNamespace(st_mode=stat.S_IFLNK, st_dev=9, st_ino=10)):
            with mock.patch.object(fixture.os, "lstat", return_value=state), self.assertRaises(fixture.Refusal):
                instance.ns(endpoint, ["unreachable"])
        self.assertEqual(commands, [])

    def test_bounded_deterministic_malformed_corpus_has_only_refusal_or_unrelated(self):
        rng = random.Random(12345678)
        for _ in range(512):
            frame = rng.randbytes(rng.randrange(0, 2200))
            for parse in (lambda data: fixture.parse_dhcp(data, XID, MAC), fixture.parse_ra,
                          lambda data: fixture.parse_dns(data, 7, "example.test")):
                try:
                    result = parse(frame)
                    self.assertIsNone(result)
                except fixture.Refusal:
                    pass


if __name__ == "__main__":
    unittest.main(verbosity=2)
