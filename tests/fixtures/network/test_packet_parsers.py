#!/usr/bin/env python3
"""Pure generated-byte and mocked-command tests: no sockets, namespaces or host changes."""

import importlib.util
import io
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

    def test_known_libvirt_normalizations_preserve_the_selected_profile(self):
        for kind, dhcp in (("nat", True), ("lab", True), ("lab", False)):
            with self.subTest(kind=kind, dhcp=dhcp):
                original = network_xml(kind, dhcp)
                normalized = (original.replace(b" ipv6='no'", b" connections='0'")
                              .replace(b" mode='nat'", b"").replace(b" family='ipv4'", b"")
                              .replace(b"prefix='24'", b"netmask='255.255.255.0'")
                              .replace(b"<bridge ", b"<mac address='52:54:00:12:34:56'/><bridge ")
                              .replace(b"<v:networkCreation xmlns:v=", b"<other:networkCreation xmlns:other="))
                # The full declaration/intent check remains the parent's Go
                # matcher; these are only equivalent forms read by the observer.
                self.assertEqual(fixture.native_network(original, UUID, "vm123456781234", kind, "on" if dhcp else "off"),
                                 fixture.native_network(normalized, UUID, "vm123456781234", kind, "on" if dhcp else "off"))

    def test_ipv6_enabled_empty_unknown_and_ipv6_addressing_remain_refused(self):
        for value in ("yes", "on", "true", "1", "", "NO", " no ", "unknown"):
            with self.subTest(value=value), self.assertRaisesRegex(fixture.Refusal, "IPv6 disabled"):
                fixture.native_network(network_xml().replace(b"ipv6='no'", ("ipv6='" + value + "'").encode()),
                                       UUID, "vm123456781234", "lab", "on")
        for raw in (network_xml().replace(b" ipv6='no'", b"").replace(b"family='ipv4'", b"family='ipv6'"),
                    network_xml().replace(b" ipv6='no'", b"").replace(b"</network>", b"<ip family='ipv6' address='fd12::1' prefix='64'/></network>")):
            with self.assertRaisesRegex(fixture.Refusal, "ambiguous native IP"):
                fixture.native_network(raw, UUID, "vm123456781234", "lab", "on")

    def test_forward_default_is_only_nat_and_never_changes_lab_mode(self):
        for value in ("route", "bridge", "open", "", "NAT"):
            with self.subTest(value=value), self.assertRaisesRegex(fixture.Refusal, "forwarding mode"):
                fixture.native_network(network_xml("nat").replace(b"mode='nat'", ("mode='" + value + "'").encode()),
                                       UUID, "vm123456781234", "nat", "on")
        with self.assertRaisesRegex(fixture.Refusal, "forwarding mode"):
            fixture.native_network(network_xml("nat").replace(b" mode='nat'", b""), UUID,
                                   "vm123456781234", "lab", "on")

    def test_equivalent_netmask_does_not_allow_ambiguous_or_inverted_masks(self):
        for replacement in (b"prefix='24' netmask='255.255.255.0'", b"netmask='0.0.0.255'",
                            b"netmask='255.0.255.0'", b"prefix='024'", b""):
            with self.subTest(replacement=replacement), self.assertRaises((fixture.Refusal, ValueError)):
                fixture.native_network(network_xml().replace(b"prefix='24'", replacement),
                                       UUID, "vm123456781234", "lab", "on")

    def test_native_comments_are_nonsemantic_but_xml_directives_refused(self):
        original = network_xml()
        self.assertEqual(fixture.native_network(original, UUID, "vm123456781234", "lab", "on"),
                         fixture.native_network(original.replace(b"<bridge", b"<!-- generated native network --><bridge"),
                                                UUID, "vm123456781234", "lab", "on"))
        for prefix in (b"<!DOCTYPE network []>", b"<?xml version='1.0'?>", b"<?native test?>"):
            with self.subTest(prefix=prefix), self.assertRaises(fixture.Refusal):
                fixture.native_network(prefix + original, UUID, "vm123456781234", "lab", "on")

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


class VethInitializationTests(unittest.TestCase):
    def harness(self, setter_failure=None, settle=None, lost_add=False):
        endpoint = {"host": "vpaa0cdad290f", "peer": "veaa0cdad290f", "namespace": "unused-generated-test",
                    "alias": "virmill-packet:" + UUID + ":host-a", "mac": "02:12:34:56:78:0a"}
        state = {"links": [], "events": []}

        def record(event):
            state["events"].append(json.loads(json.dumps(event)))

        def command(argv, **kwargs):
            record({"command": argv})
            self.assertEqual(argv, ["/usr/bin/udevadm", "settle", "--timeout=5"])
            self.assertEqual(kwargs["timeout"], 7)
            if settle:
                settle(state)
            return {"returncode": 0, "stdout": "", "stderr": ""}

        recorder = types.SimpleNamespace(event=record, command=command, deadline=0)
        instance = fixture.Fixture(types.SimpleNamespace(run_id=UUID, dhcp="off"), recorder,
                                   {"ip": "/usr/bin/ip", "udevadm": "/usr/bin/udevadm"})
        instance.endpoints = [endpoint]
        instance.links = lambda: json.loads(json.dumps(state["links"]))

        def ip(*argv, **kwargs):
            record({"ip": list(argv)})
            if argv[:2] == ("link", "add"):
                state["links"] = [
                    {"ifname": endpoint["host"], "ifindex": 10, "link": endpoint["peer"],
                     "address": "3e:a7:0f:0f:5c:1a", "flags": ["BROADCAST", "MULTICAST", "M-DOWN"],
                     "operstate": "DOWN", "linkinfo": {"info_kind": "veth"}},
                    {"ifname": endpoint["peer"], "ifindex": 9, "link": endpoint["host"],
                     "address": "a6:77:80:0a:fc:b7", "flags": ["BROADCAST", "MULTICAST", "M-DOWN"],
                     "operstate": "DOWN", "linkinfo": {"info_kind": "veth"}}]
                if lost_add:
                    raise OSError("creation acknowledgement lost")
            elif argv[:2] == ("link", "set"):
                row = next(item for item in state["links"] if item["ifname"] == argv[3])
                side = "host" if argv[3] == endpoint["host"] else "peer"
                self.assertEqual(argv[4], "alias")
                if setter_failure != side + "_noop":
                    row["ifalias"] = argv[5]
                if setter_failure == side + "_lost":
                    raise OSError("alias acknowledgement lost")
            elif argv[:2] == ("link", "delete"):
                self.assertEqual(argv, ("link", "delete", "dev", endpoint["host"]))
                state["links"] = []
            else:
                raise AssertionError("unexpected native command " + repr(argv))
            return {"returncode": 0, "stdout": "", "stderr": ""}
        instance.ip = ip
        return instance, endpoint, state

    def cleanup(self, instance):
        with mock.patch.object(fixture.os.path, "lexists", return_value=False):
            return instance.cleanup()

    def test_explicit_alias_commands_follow_durable_pair_receipt_and_settle(self):
        instance, endpoint, state = self.harness()
        instance.create_veth_pair(endpoint)
        mutations = [event["ip"] for event in state["events"] if "ip" in event]
        self.assertEqual(mutations, [
            ["link", "add", "name", endpoint["host"], "type", "veth", "peer", "name", endpoint["peer"]],
            ["link", "set", "dev", endpoint["host"], "alias", endpoint["alias"]],
            ["link", "set", "dev", endpoint["peer"], "alias", "virmill-packet:" + UUID + ":peer"]])
        phases = [event.get("phase", "command") for event in state["events"]]
        self.assertLess(phases.index("created-veth-pair-receipt"), phases.index("settled-veth-pair-receipt"))
        self.assertLess(phases.index("settled-veth-pair-receipt"), phases.index("veth-alias-intent"))
        self.assertTrue(endpoint["aliasesVerified"])
        self.assertEqual(endpoint["confirmedAliases"], ["host", "peer"])
        self.assertEqual(self.cleanup(instance)["status"], "verified")
        self.assertEqual(state["links"], [])

    def test_observed_mac_initialization_change_is_bound_after_settle(self):
        def initialize(state):
            state["links"][0]["address"] = "22:22:a5:0b:8a:90"
            state["links"][1]["address"] = "b2:d4:04:eb:5b:83"
        instance, endpoint, state = self.harness(settle=initialize)
        instance.create_veth_pair(endpoint)
        receipt = next(event for event in state["events"] if event.get("phase") == "settled-veth-pair-receipt")
        self.assertEqual(receipt["initialPair"]["host"]["address"], "3e:a7:0f:0f:5c:1a")
        self.assertEqual(receipt["pair"]["host"]["address"], "22:22:a5:0b:8a:90")
        self.assertEqual(receipt["MACChangeCause"], "not_attributed")
        self.assertEqual(endpoint["createdPair"], receipt["pair"])
        self.assertEqual(self.cleanup(instance)["status"], "verified")

    def test_alias_gap_noop_and_lost_ack_cleanup_only_the_unchanged_pair(self):
        for failure in ("host_noop", "host_lost", "peer_noop", "peer_lost"):
            with self.subTest(failure=failure):
                instance, endpoint, state = self.harness(setter_failure=failure)
                with self.assertRaises((fixture.Refusal, OSError)):
                    instance.create_veth_pair(endpoint)
                self.assertFalse(endpoint["aliasesVerified"])
                self.assertEqual(self.cleanup(instance)["status"], "verified")
                self.assertEqual(state["links"], [])
                self.assertEqual(sum(event.get("ip", [])[:2] == ["link", "delete"] for event in state["events"]), 1)

    def test_alias_gap_identity_or_attachment_changes_refuse_cleanup(self):
        changes = [(0, "address", "02:00:00:00:00:99"), (1, "address", "02:00:00:00:00:99"),
                   (0, "ifindex", 77), (1, "ifindex", 78), (1, "link", "unrelated0"),
                   (0, "ifalias", "foreign-owner"), (1, "ifalias", "foreign-owner"),
                   (0, "master", "unrelated0"), (1, "flags", ["UP"])]
        for index, key, value in changes:
            with self.subTest(index=index, key=key):
                instance, endpoint, state = self.harness(setter_failure="host_lost")
                with self.assertRaises(OSError):
                    instance.create_veth_pair(endpoint)
                state["links"][index][key] = value
                self.assertEqual(self.cleanup(instance)["status"], "manual_review_required")
                self.assertFalse(any(event.get("ip", [])[:2] == ["link", "delete"] for event in state["events"]))

    def test_lost_creation_ack_never_claims_or_deletes_a_named_pair(self):
        instance, endpoint, state = self.harness(lost_add=True)
        with self.assertRaises(OSError):
            instance.create_veth_pair(endpoint)
        self.assertNotIn("createdPair", endpoint)
        self.assertEqual(self.cleanup(instance)["status"], "manual_review_required")
        self.assertFalse(any(event.get("ip", [])[:2] == ["link", "delete"] for event in state["events"]))

    def test_settle_failure_does_not_permit_changed_mac_cleanup(self):
        for change_mac in (False, True):
            def failed_settle(state):
                if change_mac:
                    state["links"][0]["address"] = "22:22:a5:0b:8a:90"
                raise fixture.Refusal("settle deadline")
            with self.subTest(change_mac=change_mac):
                instance, endpoint, state = self.harness(settle=failed_settle)
                with self.assertRaises(fixture.Refusal):
                    instance.create_veth_pair(endpoint)
                result = self.cleanup(instance)
                self.assertEqual(result["status"], "manual_review_required" if change_mac else "verified")
                self.assertEqual(bool(state["links"]), change_mac)

    def test_post_settle_mac_drift_and_replacement_refused_even_with_same_alias(self):
        instance, endpoint, state = self.harness()
        instance.create_veth_pair(endpoint)
        state["links"][0]["address"] = "02:00:00:00:00:99"
        self.assertEqual(self.cleanup(instance)["status"], "manual_review_required")
        self.assertFalse(any(event.get("ip", [])[:2] == ["link", "delete"] for event in state["events"]))

    def test_pair_index_change_during_settle_refuses_alias_mutation(self):
        def replace_pair(state):
            state["links"][0]["ifindex"] = 99
        instance, endpoint, state = self.harness(settle=replace_pair)
        with self.assertRaisesRegex(fixture.Refusal, "changed during initialization"):
            instance.create_veth_pair(endpoint)
        self.assertEqual(self.cleanup(instance)["status"], "manual_review_required")
        self.assertFalse(any(event.get("ip", [])[:2] in (["link", "set"], ["link", "delete"])
                             for event in state["events"]))

    def test_post_receipt_change_is_refused_before_first_alias_command(self):
        instance, endpoint, state = self.harness()
        record = instance.r.event
        def replaced_after_receipt(event):
            record(event)
            if event.get("phase") == "settled-veth-pair-receipt":
                state["links"][1]["address"] = "02:00:00:00:00:99"
        instance.r.event = replaced_after_receipt
        with self.assertRaisesRegex(fixture.Refusal, "identity or journaled alias differs"):
            instance.create_veth_pair(endpoint)
        self.assertEqual(self.cleanup(instance)["status"], "manual_review_required")
        self.assertFalse(any(event.get("ip", [])[:2] in (["link", "set"], ["link", "delete"])
                             for event in state["events"]))

    def test_peer_json_name_and_numeric_forms_require_reciprocal_pair(self):
        instance, endpoint, state = self.harness(setter_failure="host_noop")
        with self.assertRaises(fixture.Refusal):
            instance.create_veth_pair(endpoint)
        named = fixture.veth_pair_snapshot(endpoint, state["links"])
        for row, index in zip(state["links"], (9, 10)):
            del row["link"]
            row["link_index"] = index
        self.assertEqual(fixture.veth_pair_snapshot(endpoint, state["links"]), named)
        state["links"][0]["link_index"] = 100
        with self.assertRaisesRegex(fixture.Refusal, "reciprocal"):
            fixture.veth_pair_snapshot(endpoint, state["links"])


class BridgeForwardingTests(unittest.TestCase):
    def harness(self, states):
        observed, saved, clock = [], {}, {"now": 0.0}
        recorder = types.SimpleNamespace(deadline=180, event=lambda value: observed.append(value),
                                         save=lambda name, value: saved.update({name: json.loads(json.dumps(value))}))
        instance = fixture.Fixture(types.SimpleNamespace(bridge="vm123456781234", run_id=UUID), recorder,
                                   {"ip": "/usr/bin/ip"})
        instance.before = {"ifindex": 7, "address": "52:54:00:11:22:33"}
        endpoint = {"role": "a", "host": "vpa1234567812", "peer": "vea1234567812", "aliasesVerified": True,
                    "alias": "virmill-packet:" + UUID + ":host-a", "mac": "02:12:34:56:78:0a",
                    "createdPair": {"host": {"ifindex": 10, "address": "22:22:a5:0b:8a:90"},
                                    "peer": {"ifindex": 9, "address": "b2:d4:04:eb:5b:83"}}}
        calls = []
        instance.check_attached_endpoints = lambda item: calls.append("identity")
        instance.bridge_state = lambda: instance.before
        instance.netxml = lambda: calls.append("xml")
        stream = iter(states)
        last = {"state": 0}
        def state(item):
            last["state"] = next(stream, last["state"])
            return last["state"]
        instance.forwarding_state = state
        def sleep(seconds):
            clock["now"] += seconds
        return instance, endpoint, observed, saved, calls, clock, sleep

    def test_wait_records_transitions_and_rechecks_identity_before_forwarding_success(self):
        instance, endpoint, observed, saved, calls, clock, sleep = self.harness([1, 2, 3])
        with mock.patch.object(fixture.time, "monotonic", side_effect=lambda: clock["now"]), \
                mock.patch.object(fixture.time, "sleep", side_effect=sleep):
            instance.wait_forwarding(endpoint)
        result = saved["bridge-forwarding-a.json"]
        self.assertEqual(result["status"], "forwarding")
        self.assertEqual([item["state"] for item in result["states"]], [1, 2, 3])
        self.assertEqual(result["requiredState"], 3)
        self.assertFalse(result["STPChanged"])
        self.assertEqual(calls, ["identity", "identity", "xml"])
        self.assertEqual(len(observed), 3)

    def test_timeout_retains_states_and_never_reports_forwarding(self):
        instance, endpoint, observed, saved, calls, clock, sleep = self.harness([1, 2])
        with mock.patch.object(fixture.time, "monotonic", side_effect=lambda: clock["now"]), \
                mock.patch.object(fixture.time, "sleep", side_effect=sleep), \
                self.assertRaisesRegex(fixture.Refusal, "did not reach forwarding"):
            instance.wait_forwarding(endpoint)
        result = saved["bridge-forwarding-a.json"]
        self.assertEqual(result["status"], "failed")
        self.assertLessEqual(len(result["states"]), 142)
        self.assertEqual(clock["now"], 35)
        self.assertEqual(calls, ["identity"])

    def test_identity_failure_after_forwarding_is_still_failure(self):
        instance, endpoint, observed, saved, calls, clock, sleep = self.harness([3])
        instance.check_attached_endpoints = mock.Mock(side_effect=[None, fixture.Refusal("peer MAC changed")])
        with mock.patch.object(fixture.time, "monotonic", return_value=0), \
                self.assertRaisesRegex(fixture.Refusal, "peer MAC changed"):
            instance.wait_forwarding(endpoint)
        self.assertEqual(saved["bridge-forwarding-a.json"]["status"], "failed")

    def test_sysfs_wait_requires_exact_port_and_bridge_identities(self):
        instance, endpoint, *_ = self.harness([])
        host = "/sys/class/net/" + endpoint["host"]
        bridge = "/sys/class/net/vm123456781234"
        expected = {host + "/ifindex": "10\n", host + "/address": "22:22:a5:0b:8a:90\n",
                    host + "/ifalias": endpoint["alias"] + "\n", host + "/master/ifindex": "7\n",
                    bridge + "/ifindex": "7\n", bridge + "/address": "52:54:00:11:22:33\n",
                    host + "/brport/state": "3\n"}
        def check(values):
            with mock.patch.object(fixture.Path, "read_text", autospec=True,
                                   side_effect=lambda path, **kwargs: values[str(path)]):
                return fixture.Fixture.forwarding_state(instance, endpoint)
        self.assertEqual(check(expected), 3)
        for key in expected:
            with self.subTest(key=key), self.assertRaises(fixture.Refusal):
                check({**expected, key: "999\n"})

    def test_dhcp_worker_alone_has_extended_timeout(self):
        instance, endpoint, *_ = self.harness([])
        instance.tools["python"] = "/usr/bin/python3"
        instance.a.recipe_sha256 = "a" * 64
        calls = []
        instance.ns = lambda item, argv, **kwargs: calls.append(kwargs) or {"stdout": '{"status":"fixture-result"}'}
        for mode in ("dhcp", "ra", "tcp", "dns"):
            instance.worker(endpoint, mode)
        self.assertEqual([call["timeout"] for call in calls], [40, 24, 24, 24])


class DHCPFailureDiagnosticsTests(unittest.TestCase):
    def test_failure_retains_only_first_eight_frames_with_unrelated_classification(self):
        clock = {"now": 0.0}
        packets = [reply(xid=XID + index + 1) for index in range(10)]
        sent = []
        class FakeSocket:
            def __enter__(self):
                return self
            def __exit__(self, *args):
                pass
            def setsockopt(self, *args):
                pass
            def bind(self, *args):
                pass
            def settimeout(self, *args):
                pass
            def send(self, packet):
                sent.append(packet)
            def recvmsg(self, *args):
                clock["now"] += 0.25
                if packets:
                    return packets.pop(0), [], 0, ("vea1234567812", 2048, 0, 1, bytes(6))
                raise fixture.socket.timeout()
        args = types.SimpleNamespace(interface="vea1234567812")
        with mock.patch.object(fixture.socket, "socket", return_value=FakeSocket()), \
                mock.patch.object(fixture.os, "urandom", return_value=XID.to_bytes(4, "big")), \
                mock.patch.object(fixture.time, "monotonic", side_effect=lambda: clock["now"]), \
                self.assertRaisesRegex(fixture.Refusal, "DHCP phase 2 timed out") as caught:
            fixture.dhcp_worker(args, MAC)
        diagnostics = json.loads(str(caught.exception).rsplit("; ", 1)[1])
        self.assertEqual(len(diagnostics["receivedSamples"]), 8)
        self.assertEqual([item["classification"] for item in diagnostics["receivedSamples"]], ["unrelated"] * 8)
        self.assertEqual([item["bootpXID"] for item in diagnostics["receivedSamples"]], list(range(XID + 1, XID + 9)))
        self.assertEqual(clock["now"], 18)
        self.assertEqual(len(sent), 6)

    def test_frame_diagnostic_is_bounded_and_preserves_packet_type(self):
        frame = reply() + bytes(3000)
        sample = fixture.dhcp_frame_sample(frame, ("vea1234567812", 2048, 4), 32)
        self.assertEqual(sample["capturedBytes"], 2048)
        self.assertEqual(len(sample["frameHex"]), 4096)
        self.assertTrue(sample["truncated"])
        self.assertEqual(sample["packetType"], 4)
        self.assertEqual(sample["udpPorts"], [67, 68])
        self.assertEqual(sample["bootpClientMAC"], MAC.hex())


def protected_args(**changes):
    values = dict(kind="lab", host_access="services-only", dhcp="on", static_cidr4=None,
                  static_a4=None, static_b4=None, host4_target=None, forward4_target="198.51.100.8",
                  forward4_port=443, dns_name="example.test", dns_own_lease=False)
    values.update(changes)
    return types.SimpleNamespace(**values)


def protected_xml(kind="lab", dhcp=True):
    raw = network_xml(kind, dhcp).replace(b"version='1'", b"version='2'")
    if kind == "guest-only":
        raw = raw[:raw.index(b"<ip ")] + b"</network>"
    return raw.replace(b"</network>", (b"<dns enable='yes'/>" if dhcp else b"<dns enable='no'/>") + b"</network>")


class ProtectedProfileTests(unittest.TestCase):
    def test_supported_protected_profiles_require_explicit_controls(self):
        for kind in ("nat", "lab"):
            for enabled in (True, False):
                args = protected_args(kind=kind, dhcp="on" if enabled else "off",
                                      static_a4=None if enabled else "192.168.80.20",
                                      static_b4=None if enabled else "192.168.80.21",
                                      dns_name="example.test" if enabled else None)
                fixture.validate_profile(args)
                network, gateway, ranges = fixture.native_network(protected_xml(kind, enabled), UUID,
                    "vm123456781234", kind, args.dhcp, args.host_access)
                self.assertEqual((str(network), gateway, bool(ranges)), ("192.168.80.0/24", "192.168.80.1", enabled))
        args = protected_args(kind="guest-only", host_access="deny", dhcp="off", dns_name=None,
                              static_cidr4="192.168.80.0/24", static_a4="192.168.80.20",
                              static_b4="192.168.80.21", host4_target="192.168.90.1")
        fixture.validate_profile(args)
        network, gateway, ranges = fixture.native_network(protected_xml("guest-only", False), UUID,
            "vm123456781234", "guest-only", "off", "deny", args.static_cidr4)
        self.assertEqual((str(network), gateway, ranges), ("192.168.80.0/24", None, []))

    def test_policy_confusion_or_missing_positive_controls_refused(self):
        for change in ({"host_access": "deny"}, {"kind": "guest-only"}, {"forward4_target": None},
                       {"forward4_target": None, "forward4_port": None}, {"forward4_port": 0},
                       {"dns_name": None}, {"static_cidr4": "192.168.80.0/24"},
                       {"host4_target": "192.168.90.1"}, {"static_a4": "192.168.80.20"}):
            with self.subTest(change=change), self.assertRaises(fixture.Refusal):
                fixture.validate_profile(protected_args(**change))
        guest = vars(protected_args(kind="guest-only", host_access="deny", dhcp="off", dns_name=None,
                                   static_cidr4="192.168.80.0/24", static_a4="192.168.80.20",
                                   static_b4="192.168.80.21", host4_target="192.168.90.1"))
        for change in ({"host_access": "allow"}, {"dhcp": "on"}, {"host4_target": None},
                       {"host4_target": "192.168.80.1"}, {"host4_target": "198.51.100.8"},
                       {"static_cidr4": None}, {"static_b4": None}, {"static_a4": "192.168.80.0"},
                       {"static_b4": "192.168.80.255"}, {"static_b4": "192.168.90.8"}, {"dns_name": "example.test"}):
            with self.subTest(change=change), self.assertRaises(fixture.Refusal):
                fixture.validate_profile(types.SimpleNamespace(**{**guest, **change}))

    def test_guest_static_cidr_requires_canonical_ordinary_private_subnet(self):
        for value in ("192.168.80.1/24", "192.168.80.0/024", "192.168.80.0/30", "127.0.0.0/8",
                      "198.51.100.0/24", "169.254.0.0/16", "0.0.0.0/0", "fd00::/64"):
            with self.subTest(cidr=value), self.assertRaises((fixture.Refusal, ValueError)):
                fixture.private_subnet(value)

    def test_protected_dns_requires_exact_explicit_pair_and_marker(self):
        for enabled in (True, False):
            raw = protected_xml("lab", enabled)
            dns = b"<dns enable='yes'/>" if enabled else b"<dns enable='no'/>"
            for replacement in (b"", b"<dns/>", b"<dns enable='true'/>", dns + dns,
                                b"<dns enable='yes'><forwarder addr='192.0.2.1'/></dns>"):
                with self.subTest(enabled=enabled, replacement=replacement), self.assertRaises(fixture.Refusal):
                    fixture.native_network(raw.replace(dns, replacement), UUID, "vm123456781234",
                                           "lab", "on" if enabled else "off", "services-only")
            with self.assertRaisesRegex(fixture.Refusal, "marker"):
                fixture.native_network(raw.replace(b"version='2'", b"version='1'"), UUID, "vm123456781234",
                                       "lab", "on" if enabled else "off", "services-only")

    def test_guest_native_l3_dhcp_dns_route_or_forward_drift_refused(self):
        raw = protected_xml("guest-only", False)
        for extra in (b"<ip address='192.168.80.1' prefix='24'/>", b"<ip family='ipv6' address='fe80::1' prefix='64'/>",
                      b"<dhcp/>", b"<route address='0.0.0.0' prefix='0' gateway='192.168.80.1'/>",
                      b"<forward mode='nat'/>"):
            with self.subTest(extra=extra), self.assertRaises(fixture.Refusal):
                fixture.native_network(raw.replace(b"</network>", extra + b"</network>"), UUID,
                                       "vm123456781234", "guest-only", "off", "deny", "192.168.80.0/24")

    def test_host_target_requires_one_ready_existing_address(self):
        row = {"ifindex": 7, "ifname": "ens3", "address": "02:00:00:00:00:01", "flags": ["UP"],
               "addr_info": [{"family": "inet", "local": "192.168.90.1", "prefixlen": 24, "scope": "global"}]}
        found = fixture.assigned_host_target([row], "192.168.90.1", "vm123456781234")
        self.assertEqual(found["ifindex"], 7)
        for rows in ([], [row, row], [{**row, "flags": []}], [{**row, "ifname": "vm123456781234"}],
                     [{**row, "addr_info": [{**row["addr_info"][0], "scope": "host"}]}]):
            with self.subTest(rows=rows), self.assertRaises(fixture.Refusal):
                fixture.assigned_host_target(rows, "192.168.90.1", "vm123456781234")

    def test_tcp_negative_requires_a_network_outcome_not_arbitrary_worker_failure(self):
        for value in ({"status": "failed"}, {"status": "not_connected"},
                      {"status": "not_connected", "errno": 24, "boundedNetworkRefusal": False},
                      {"status": "connected"}):
            self.assertFalse(fixture.tcp_expectation(value, False))
        self.assertTrue(fixture.tcp_expectation({"status": "not_connected", "boundedNetworkRefusal": True}, False))

    def test_connected_handshake_with_failed_token_cannot_be_a_negative_pass(self):
        sock = mock.MagicMock()
        sock.__enter__.return_value = sock
        sock.recv.side_effect = TimeoutError("application token timed out")
        with mock.patch.object(fixture.socket, "socket", return_value=sock):
            result = fixture.tcp_worker(types.SimpleNamespace(target="192.168.90.1", port=32123, token="expected"))
        self.assertEqual(result["status"], "connected")
        self.assertFalse(fixture.tcp_expectation(result, False))
        self.assertFalse(fixture.tcp_expectation(result, True))

    def test_explicit_neighbor_changes_only_held_namespace_and_requires_readback(self):
        f = object.__new__(fixture.Fixture)
        f.tools = {"ip": "/usr/bin/ip"}
        f.before = {"address": "52:54:00:12:34:56"}
        f.r = types.SimpleNamespace(event=mock.Mock())
        f.observation_boundary = mock.Mock()
        endpoint, calls = {"peer": "vea1234567812", "namespace": "fixture-owned"}, []
        def ns(ep, argv):
            self.assertIs(ep, endpoint)
            calls.append(argv)
            if argv[1:3] == ["-j", "-4"]:
                return {"stdout": json.dumps([{"dst": "192.168.90.1", "dev": ep["peer"]}])}
            if argv[1:3] == ["-j", "neigh"]:
                return {"stdout": json.dumps([{"dst": "192.168.90.1", "lladdr": f.before["address"], "state": ["PERMANENT"]}])}
            return {"stdout": ""}
        f.ns = ns
        f.direct_neighbor(endpoint, "192.168.90.1")
        self.assertEqual(calls[0], ["/usr/bin/ip", "route", "add", "192.168.90.1/32", "dev", endpoint["peer"], "scope", "link"])
        self.assertEqual(calls[1], ["/usr/bin/ip", "neigh", "replace", "192.168.90.1", "lladdr", f.before["address"],
                                   "nud", "permanent", "dev", endpoint["peer"]])
        self.assertFalse(any("address" in argv or "default" in argv for argv in calls))
        f.observation_boundary = mock.Mock(side_effect=fixture.Refusal("replacement"))
        calls.clear()
        with self.assertRaisesRegex(fixture.Refusal, "replacement"):
            f.direct_neighbor(endpoint, "192.168.90.1")
        self.assertEqual(calls, [])

    def test_protected_listener_requires_controls_before_and_after_guest_probe(self):
        for outcomes, calls_expected in ((["connected", "connected"], 1), (["not_connected"], 0),
                                         (["connected", "not_connected"], 1)):
            with self.subTest(outcomes=outcomes):
                f = object.__new__(fixture.Fixture)
                f.a = types.SimpleNamespace(host_access="deny", kind="guest-only", host4_target="192.168.90.1",
                                            bridge="vm123456781234", run_id=UUID)
                f.r = types.SimpleNamespace(event=mock.Mock())
                f.direct_neighbor = mock.Mock()
                f.worker = mock.Mock(return_value={"status": "not_connected", "boundedNetworkRefusal": True})
                listener = mock.MagicMock()
                listener.__enter__.return_value = listener
                listener.getsockname.return_value = (f.a.host4_target, 32123)
                thread = mock.Mock()
                thread.is_alive.return_value = False
                with mock.patch.object(fixture.socket, "socket", return_value=listener), \
                        mock.patch.object(fixture.threading, "Thread", return_value=thread), \
                        mock.patch.object(fixture, "host_tcp_control", side_effect=[{"status": status} for status in outcomes]):
                    if all(status == "connected" for status in outcomes):
                        result = f.host_probe({})
                        self.assertEqual(result["expected"], "not_connected")
                        self.assertEqual(len(result["positiveControls"]), 2)
                        self.assertTrue(result["listenerClosed"])
                    else:
                        with self.assertRaisesRegex(fixture.Refusal, "positive control"):
                            f.host_probe({})
                self.assertEqual(f.worker.call_count, calls_expected)
                listener.bind.assert_called_once_with((f.a.host4_target, 0))
                listener.setsockopt.assert_not_called()
                listener.__exit__.assert_called_once()
                thread.join.assert_called_once()

    def test_bridge_l3_or_host_binding_drift_stops_observation(self):
        f = object.__new__(fixture.Fixture)
        f.a = types.SimpleNamespace(kind="guest-only")
        f.before = {"ifindex": 12, "addresses": []}
        f.host_target_before = {"ifindex": 3}
        f.netxml = mock.Mock()
        f.bridge_state = mock.Mock(return_value={"ifindex": 12, "addresses": [["inet6", "fe80::1", 64, "link"]]})
        f.host_target_state = mock.Mock()
        with self.assertRaisesRegex(fixture.Refusal, "bridge identity/L3"):
            f.observation_boundary("test")
        f.host_target_state.assert_not_called()
        f.bridge_state.return_value = f.before
        f.host_target_state.return_value = {"ifindex": 4}
        with self.assertRaisesRegex(fixture.Refusal, "host target"):
            f.observation_boundary("test")

    def test_observer_reports_each_protected_matrix_outcome_without_promoting_acceptance(self):
        for kind, dhcp in (("nat", "on"), ("lab", "on"), ("lab", "off"), ("guest-only", "off")):
            for fault in (None, "host-exposed", "wrong-forward", "peer-blocked", "IPv6-leak", "service-failed"):
                with self.subTest(kind=kind, dhcp=dhcp, fault=fault):
                    f = object.__new__(fixture.Fixture)
                    f.a = protected_args(kind=kind, dhcp=dhcp, host_access="deny" if kind == "guest-only" else "services-only",
                                        dns_name="example.test" if dhcp == "on" and kind != "lab" else None,
                                        dns_own_lease=dhcp == "on" and kind == "lab", run_id=UUID)
                    f.before = {"addresses": [] if kind == "guest-only" else [["inet", "192.168.80.1", 24, "global"]]}
                    f.gateway = None if kind == "guest-only" else "192.168.80.1"
                    f.tools = {"ip": "/usr/bin/ip", "ping": "/usr/bin/ping"}
                    ack = {"routerOptionPresent": kind == "nat", "routers": [f.gateway] if kind == "nat" else [],
                           "optionCodes": [3] if kind == "nat" else [], "dns": [f.gateway]}
                    f.endpoints = [{"role": role, "address": "192.168.80." + last, "peer": "ve" + role,
                                    "dhcpHostname": fixture.lease_hostname(UUID, role),
                                    "lease": {"ack": {**ack, "address": "192.168.80." + last},
                                              "requestedHostname": fixture.lease_hostname(UUID, role)}}
                                   for role, last in (("a", "20"), ("b", "21"))]
                    f.observation_boundary = mock.Mock()
                    f.host_probe = mock.Mock(return_value={"status": "connected" if fault == "host-exposed" else "not_connected",
                                                          "boundedNetworkRefusal": True})
                    forward_connected = (kind == "nat") != (fault == "wrong-forward")
                    f.forward_probe = mock.Mock(return_value={"status": "connected" if forward_connected else "not_connected",
                                                             "boundedNetworkRefusal": True})
                    def ns(endpoint, argv, **kwargs):
                        if argv[0] == f.tools["ping"]:
                            return {"returncode": int(fault == "peer-blocked"), "stdout": ""}
                        if "address" in argv:
                            return {"stdout": '[{"addr_info": []}]'}
                        return {"stdout": '[]'}
                    def worker(endpoint, mode, *args):
                        if mode == "dns":
                            if f.a.dns_own_lease:
                                target = f.endpoints[1]
                                self.assertIs(endpoint, f.endpoints[0])
                                self.assertEqual(args, ("--target", f.gateway, "--dns-name", target["dhcpHostname"]))
                                return {"status": "resolved", "rcode": 0, "server": f.gateway, "name": target["dhcpHostname"],
                                        "addresses": ["192.168.80.22" if fault == "service-failed" else target["address"]],
                                        "answers": [{"name": target["dhcpHostname"], "address": target["address"]}]}
                            return {"status": "not_resolved" if fault == "service-failed" else "resolved"}
                        if mode == "dhcp-absence":
                            return {"status": "response_or_inconclusive" if fault == "service-failed" else "none_seen_in_bounded_window"}
                        self.assertEqual(mode, "ra")
                        return {"advertisements": ["observed"] if fault == "IPv6-leak" else []}
                    f.ns, f.worker = ns, worker
                    with mock.patch.object(fixture.time, "sleep"):
                        result = f.observe()
                    self.assertEqual(result["ipv4ChecksMet"], fault in (None, "IPv6-leak"))
                    self.assertEqual(result["ipv6DisabledContradictedByObservedTraffic"], fault == "IPv6-leak")
                    self.assertFalse(result["fullNetworkAcceptance"])
                    self.assertFalse(result["realGuestQualified"])
                    self.assertFalse(result["servicesOnlyEnforcementQualified"])
                    self.assertTrue(result["bridgeL3UnchangedAtCheckpoints"])

    def test_guest_forward_probe_uses_direct_neighbor_and_failed_control_is_not_negative_success(self):
        for control in ("connected", "not_connected"):
            f = object.__new__(fixture.Fixture)
            f.a = protected_args(kind="guest-only", host_access="deny")
            f.network = ipaddress.IPv4Network("192.168.80.0/24")
            f.r = types.SimpleNamespace(event=mock.Mock())
            f.direct_neighbor = mock.Mock()
            f.worker = mock.Mock(return_value={"status": "not_connected", "boundedNetworkRefusal": True})
            with self.subTest(control=control), mock.patch.object(fixture, "host_tcp_control", return_value={"status": control}) as probe:
                if control == "connected":
                    result = f.forward_probe({})
                    self.assertEqual(result["expected"], "not_connected")
                    self.assertEqual(len(result["positiveControls"]), 2)
                    f.direct_neighbor.assert_called_once_with({}, "198.51.100.8")
                    self.assertEqual(probe.call_count, 2)
                else:
                    with self.assertRaisesRegex(fixture.Refusal, "positive control"):
                        f.forward_probe({})
                    f.direct_neighbor.assert_not_called()
                    f.worker.assert_not_called()

    def test_dhcp_absence_never_turns_response_malformed_or_packet_saturation_into_pass(self):
        for case in ("silent", "offer", "malformed", "saturated"):
            now, frames = [0.0], [0]
            class FakeSocket:
                def __enter__(self):
                    return self
                def __exit__(self, *args):
                    pass
                def setsockopt(self, *args):
                    pass
                def bind(self, *args):
                    pass
                def settimeout(self, *args):
                    pass
                def send(self, packet):
                    pass
                def recvmsg(self, *args):
                    now[0] += 0.001 if case == "saturated" else 0.25
                    frames[0] += 1
                    if case == "silent" or (frames[0] > 1 and case != "saturated"):
                        raise fixture.socket.timeout()
                    packet = reply(message=2) if case == "offer" else reply()[:-1] if case == "malformed" else reply(xid=XID + 1)
                    return packet, [], 0, ("vea1234567812", 2048, 0)
            with self.subTest(case=case), mock.patch.object(fixture.socket, "socket", return_value=FakeSocket()), \
                    mock.patch.object(fixture.os, "urandom", return_value=XID.to_bytes(4, "big")), \
                    mock.patch.object(fixture.time, "monotonic", side_effect=lambda: now[0]):
                result = fixture.dhcp_absence_worker(types.SimpleNamespace(interface="vea1234567812"), MAC)
                self.assertEqual(result["status"], "none_seen_in_bounded_window" if case == "silent" else "response_or_inconclusive")
                self.assertFalse(result["absenceProven"])
                self.assertLessEqual(len(result["receivedSamples"]), 8)


class OwnLeaseDNSTests(unittest.TestCase):
    def test_unique_role_hostnames_are_bounded_and_sent_in_both_dhcp_phases(self):
        names = [fixture.lease_hostname(run, role) for run in (UUID, "87654321-1234-4234-8234-123456789abc") for role in ("a", "b")]
        self.assertEqual(len(set(names)), 4)
        for name in names:
            self.assertLessEqual(len(name), 63)
            for offered, server in ((None, None), ("192.168.80.21", "192.168.80.1")):
                frame = fixture.dhcp_request(MAC, XID, offered, server, hostname=name)
                self.assertEqual(fixture.checksum(frame[14:34]), 0)
                self.assertEqual(int.from_bytes(frame[16:18], "big"), len(frame) - 14)
                self.assertEqual(int.from_bytes(frame[38:40], "big"), len(frame) - 34)
                self.assertEqual(frame[40:42], bytes(2))  # preserved IPv4 UDP no-checksum encoding
                self.assertEqual(frame[52:54], b"\x80\x00")
                self.assertEqual(frame[46:50], XID.to_bytes(4, "big"))
                self.assertEqual(frame[70:76], MAC)
                options = fixture.parse_options(frame[282:])
                self.assertEqual(options[12], name.encode("ascii"))
                self.assertEqual(options[53], bytes([3 if offered else 1]))
                self.assertEqual(options[61], b"\x01" + MAC)
                self.assertEqual(options[55], bytes([1, 3, 6, 51, 54, 121]))
                self.assertEqual(50 in options, offered is not None)
        self.assertNotIn(12, fixture.parse_options(fixture.dhcp_request(MAC, XID)[282:]))

    def test_hostname_cannot_inject_options_or_extra_dns_labels(self):
        for name in ("", "a" * 64, "-bad", "bad-", "name.local", "UPPER", "name\x00x", "name\n", "é", "*"):
            with self.subTest(name=name), self.assertRaises(fixture.Refusal):
                fixture.dhcp_request(MAC, XID, hostname=name)
        with self.assertRaises(fixture.Refusal):
            fixture.lease_hostname(UUID, "c")

    def test_dhcp_exchange_retains_actual_requested_hostname_and_frames(self):
        hostname = fixture.lease_hostname(UUID, "b")
        packets, sent = [reply(message=2), reply(message=5)], []
        class FakeSocket:
            def __enter__(self):
                return self
            def __exit__(self, *args):
                pass
            def setsockopt(self, *args):
                pass
            def bind(self, *args):
                pass
            def settimeout(self, *args):
                pass
            def send(self, data):
                sent.append(data)
            def recvmsg(self, *args):
                return packets.pop(0), [], 0, ("veb1234567812", 2048, 0)
        args = types.SimpleNamespace(interface="veb1234567812", network="192.168.80.0/24", gateway="192.168.80.1",
                                     range=["192.168.80.2,192.168.80.254"], dhcp_hostname=hostname)
        with mock.patch.object(fixture.socket, "socket", return_value=FakeSocket()), \
                mock.patch.object(fixture.os, "urandom", return_value=XID.to_bytes(4, "big")):
            result = fixture.dhcp_worker(args, MAC)
        self.assertEqual(result["requestedHostname"], hostname)
        self.assertEqual(result["status"], "acknowledged")
        self.assertEqual(result["ack"]["address"], "192.168.80.21")
        self.assertEqual([fixture.parse_options(frame[282:])[12] for frame in sent], [hostname.encode()] * 2)
        self.assertEqual([entry["frameHex"] for entry in result["transmitted"]], [frame.hex() for frame in sent])

    def test_dns_modes_are_explicit_mutually_exclusive_and_dhcp_only(self):
        for host in ("allow", "services-only"):
            for kind in ("nat", "lab"):
                fixture.validate_profile(protected_args(kind=kind, host_access=host, dns_name=None, dns_own_lease=True))
        for change in ({"dns_name": "example.test"}, {"dhcp": "off", "static_a4": "192.168.80.20", "static_b4": "192.168.80.21"},
                       {"kind": "guest-only", "host_access": "deny", "dhcp": "off", "static_cidr4": "192.168.80.0/24",
                        "static_a4": "192.168.80.20", "static_b4": "192.168.80.21", "host4_target": "192.168.90.1"}):
            values = {**vars(protected_args(dns_name=None, dns_own_lease=True)), **change}
            with self.subTest(change=change), self.assertRaises(fixture.Refusal):
                fixture.validate_profile(types.SimpleNamespace(**values))
        root = "/home/virmill-test/virmill-tests/generated-test"
        args = ["run", "--execute-reviewed", "--confirm-new-unused-network", "--root", root, "--output", root + "/network-packet-" + UUID,
                "--run-id", UUID, "--network-id", UUID, "--bridge", "vm123456781234", "--recipe-sha256", "a" * 64,
                "--network-xml-sha256", "b" * 64, "--kind", "lab", "--host-access", "services-only", "--dhcp", "on",
                "--dns-own-lease", "--dns-name", "example.test"]
        with mock.patch.object(fixture.sys, "stderr", io.StringIO()), self.assertRaises(SystemExit) as failure:
            fixture.main(args)
        self.assertEqual(failure.exception.code, 2)  # argparse refused before source/host/filesystem operations

    def test_dns_answer_must_bind_exact_role_b_ack_owner_and_address(self):
        name = fixture.lease_hostname(UUID, "b")
        target = {"role": "b", "dhcpHostname": name, "address": "192.168.80.21",
                  "lease": {"requestedHostname": name, "ack": {"address": "192.168.80.21"}}}
        query = fixture.dns_query(name, 7)
        header = struct.pack("!HHHHHH", 7, 0x8180, 1, 1, 0, 0)
        record = bytes.fromhex("c00c000100010000003c0004c0a85015")
        parsed = fixture.parse_dns(header + query[12:] + record, 7, name)
        result = {**parsed, "name": name, "server": "192.168.80.1"}
        self.assertTrue(fixture.own_lease_dns_expectation(result, target, "192.168.80.1")["ownLeaseExpectationMet"])
        for change in ({"rcode": 5}, {"status": "no_A_answer"}, {"server": "192.168.80.2"}, {"name": "other"},
                       {"addresses": ["192.168.80.20"]}, {"addresses": ["192.168.80.21", "192.168.80.22"]},
                       {"addresses": ["192.168.80.21"] * 2}, {"answers": [{"name": "unrelated", "address": "192.168.80.21"}]}):
            with self.subTest(change=change):
                self.assertFalse(fixture.own_lease_dns_expectation({**result, **change}, target, "192.168.80.1")["ownLeaseExpectationMet"])
        # Matching question/address with an unrelated A owner is not this lease.
        wrong_owner = b"\x05other\0" + record[2:]
        wrong = fixture.parse_dns(header + query[12:] + wrong_owner, 7, name)
        self.assertFalse(fixture.own_lease_dns_expectation({**wrong, "name": name, "server": "192.168.80.1"}, target,
                                                        "192.168.80.1")["ownLeaseExpectationMet"])
        for change in ({"role": "a"}, {"address": "192.168.80.20"}, {"dhcpHostname": "other"}):
            with self.subTest(change=change), self.assertRaises(fixture.Refusal):
                fixture.own_lease_dns_expectation(result, {**target, **change}, "192.168.80.1")

    def test_worker_hostname_flag_cannot_target_another_run_role_or_worker(self):
        hostname = fixture.lease_hostname(UUID, "b")
        for mode, value in (("dhcp", hostname), ("dhcp", fixture.lease_hostname(UUID, "a")),
                            ("dhcp", "other-name"), ("dhcp-absence", hostname)):
            args = ["worker", "--worker", mode, "--interface", "veb1234567812", "--host-netns-inode", "1",
                    "--run-id", UUID, "--recipe-sha256", fixture.digest(Path(fixture.__file__).read_bytes()), "--dhcp-hostname", value]
            with self.subTest(mode=mode, hostname=value), mock.patch.object(fixture, "worker_guard", return_value=MAC), \
                    mock.patch.object(fixture, "dhcp_worker", return_value={"status": "generated-only"}) as worker, \
                    mock.patch.object(fixture.sys, "stdout", io.StringIO()):
                if mode == "dhcp" and value == hostname:
                    self.assertEqual(fixture.main(args), 0)
                    self.assertEqual(worker.call_args.args[0].dhcp_hostname, hostname)
                else:
                    with self.assertRaisesRegex(fixture.Refusal, "run and endpoint role"):
                        fixture.main(args)
                    worker.assert_not_called()


if __name__ == "__main__":
    unittest.main(verbosity=2)
