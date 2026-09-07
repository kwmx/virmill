//go:build linux

package linux

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/domain"
)

const fixturePrefixSeq, fixturePrefixPort = 7, 4242

func prefixTestAttr(kind uint16, data []byte) []byte {
	b := make([]byte, prefixAlign(4+len(data)))
	binary.NativeEndian.PutUint16(b, uint16(4+len(data)))
	binary.NativeEndian.PutUint16(b[2:], kind)
	copy(b[4:], data)
	return b
}

func prefixTestU32(kind uint16, value uint32) []byte {
	b := make([]byte, 4)
	binary.NativeEndian.PutUint32(b, value)
	return prefixTestAttr(kind, b)
}

func prefixTestIP(kind uint16, address string) []byte {
	return prefixTestAttr(kind, netip.MustParseAddr(address).AsSlice())
}

func prefixTestMessage(kind, flags uint16, payload []byte) []byte {
	b := make([]byte, prefixAlign(unix.NLMSG_HDRLEN+len(payload)))
	binary.NativeEndian.PutUint32(b, uint32(unix.NLMSG_HDRLEN+len(payload)))
	binary.NativeEndian.PutUint16(b[4:], kind)
	binary.NativeEndian.PutUint16(b[6:], flags)
	binary.NativeEndian.PutUint32(b[8:], fixturePrefixSeq)
	binary.NativeEndian.PutUint32(b[12:], fixturePrefixPort)
	copy(b[unix.NLMSG_HDRLEN:], payload)
	return b
}

func prefixTestDone() []byte {
	return prefixTestMessage(unix.NLMSG_DONE, unix.NLM_F_MULTI, make([]byte, 4))
}

func prefixTestRoute(attrs ...[]byte) []byte {
	b := make([]byte, unix.SizeofRtMsg)
	b[0], b[1], b[4], b[7] = unix.AF_INET, 24, unix.RT_TABLE_MAIN, unix.RTN_UNICAST
	for _, a := range attrs {
		b = append(b, a...)
	}
	return b
}

func prefixTestAddress(attrs ...[]byte) []byte {
	b := make([]byte, unix.SizeofIfAddrmsg)
	b[0], b[1], b[4] = unix.AF_INET, 24, 2
	for _, a := range attrs {
		b = append(b, a...)
	}
	return b
}

func prefixTestReceiver(packets ...[]byte) hostPrefixReceiver {
	return func(_ context.Context, buf []byte) (int, int, uint32, error) {
		if len(packets) == 0 {
			return 0, 0, 0, io.EOF
		}
		packet := packets[0]
		packets = packets[1:]
		return copy(buf, packet), 0, 0, nil
	}
}

func TestHostPrefixSyntheticWireFixtures(t *testing.T) {
	if binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		t.Skip("archived Linux x86-64 wire fixtures are little-endian; generated decoder tests are native-endian")
	}
	for _, tc := range []struct {
		file     string
		kind     uint16
		family   byte
		expected []domain.HostNetworkPrefix
		warnings int
	}{
		{"ipv4-addresses.hex", unix.RTM_GETADDR, unix.AF_INET, []domain.HostNetworkPrefix{
			{CIDR: "0.0.0.0/0", Source: "address", InterfaceIndex: 3},
			{CIDR: "192.0.2.0/24", Source: "address", InterfaceIndex: 2},
			{CIDR: "198.51.100.9/32", Source: "address", InterfaceIndex: 8},
			{CIDR: "203.0.113.7/32", Source: "address", InterfaceIndex: 8},
		}, 0},
		{"ipv6-addresses.hex", unix.RTM_GETADDR, unix.AF_INET6, []domain.HostNetworkPrefix{
			{CIDR: "2001:db8:1::/64", Source: "address", InterfaceIndex: 2},
			{CIDR: "2001:db8:2::2/128", Source: "address", InterfaceIndex: 8},
			{CIDR: "2001:db8:3::1/128", Source: "address", InterfaceIndex: 8},
			{CIDR: "fe80::/64", Source: "address", InterfaceIndex: 2},
		}, 0},
		{"ipv4-routes.hex", unix.RTM_GETROUTE, unix.AF_INET, []domain.HostNetworkPrefix{
			{CIDR: "0.0.0.0/0", Source: "route", InterfaceIndex: 2, Table: 254},
			{CIDR: "10.22.33.0/24", Source: "route", InterfaceIndex: 8, Table: 100},
			{CIDR: "10.22.33.0/24", Source: "route", InterfaceIndex: 9, Table: 100},
			{CIDR: "10.44.0.0/16", Source: "route", Table: 2000},
			{CIDR: "192.0.2.0/24", Source: "route", InterfaceIndex: 8, Table: 1000},
			{CIDR: "203.0.113.0/24", Source: "route", Table: 254},
		}, 1},
		{"ipv6-routes.hex", unix.RTM_GETROUTE, unix.AF_INET6, []domain.HostNetworkPrefix{
			{CIDR: "2001:db8:9::/64", Source: "route", InterfaceIndex: 9, Table: 100},
			{CIDR: "2001:db8:9::/64", Source: "route", InterfaceIndex: 11, Table: 100},
			{CIDR: "::/0", Source: "route", InterfaceIndex: 2, Table: 254},
			{CIDR: "fd12:3456:789a::/64", Source: "route", InterfaceIndex: 8, Table: 500},
			{CIDR: "fe80::/64", Source: "route", InterfaceIndex: 2, Table: 254},
		}, 0},
	} {
		t.Run(tc.file, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("../../../tests/fixtures/network-prefixes", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			wire, err := hex.DecodeString(strings.Join(strings.Fields(string(b)), ""))
			if err != nil {
				t.Fatal(err)
			}
			out, err := readHostPrefixDump(context.Background(), prefixTestReceiver(wire), tc.kind, tc.family, fixturePrefixSeq, fixturePrefixPort)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(out.Prefixes, tc.expected) || len(out.Warnings) != tc.warnings {
				t.Fatalf("unexpected synthetic fixture result: %#v", out)
			}
			if tc.warnings > 0 && !strings.HasPrefix(out.Warnings[0], "HOST_PREFIX_NEXTHOP_INTERFACE_UNRESOLVED:") {
				t.Fatal("missing unresolved nexthop warning")
			}
		})
	}
}

func TestHostPrefixRequestsDoNotFilterTablesOrMutate(t *testing.T) {
	for _, kind := range []uint16{unix.RTM_GETADDR, unix.RTM_GETROUTE} {
		for _, family := range []byte{unix.AF_INET, unix.AF_INET6} {
			b := hostPrefixDumpRequest(kind, family, 17, 55)
			if binary.NativeEndian.Uint16(b[4:]) != kind || binary.NativeEndian.Uint16(b[6:]) != unix.NLM_F_REQUEST|unix.NLM_F_DUMP || b[16] != family {
				t.Fatal("request is not an explicit family read-only dump")
			}
			if !bytes.Equal(b[17:], make([]byte, len(b)-17)) {
				t.Fatal("request contains an interface, table, protocol, or other filter")
			}
		}
	}
}

func TestHostPrefixRejectsIncompleteAndInterruptedDumps(t *testing.T) {
	valid := prefixTestMessage(unix.RTM_NEWROUTE, unix.NLM_F_MULTI, prefixTestRoute(prefixTestIP(unix.RTA_DST, "192.0.2.1")))
	for _, tc := range []struct {
		name string
		recv hostPrefixReceiver
	}{
		{"missing-done", prefixTestReceiver(valid)},
		{"empty-receive", prefixTestReceiver([]byte{})},
		{"data-dump-interrupted", prefixTestReceiver(prefixTestMessage(unix.RTM_NEWROUTE, unix.NLM_F_MULTI|unix.NLM_F_DUMP_INTR, valid[16:]), prefixTestDone())},
		{"done-dump-interrupted", prefixTestReceiver(valid, prefixTestMessage(unix.NLMSG_DONE, unix.NLM_F_MULTI|unix.NLM_F_DUMP_INTR, make([]byte, 4)))},
		{"filtered-dump", prefixTestReceiver(valid, prefixTestMessage(unix.NLMSG_DONE, unix.NLM_F_MULTI|unix.NLM_F_DUMP_FILTERED, make([]byte, 4)))},
		{"receive-truncated", func(_ context.Context, b []byte) (int, int, uint32, error) {
			return copy(b, valid), unix.MSG_TRUNC, 0, nil
		}},
		{"control-truncated", func(_ context.Context, b []byte) (int, int, uint32, error) {
			return copy(b, valid), unix.MSG_CTRUNC, 0, nil
		}},
		{"sender-not-kernel", func(_ context.Context, b []byte) (int, int, uint32, error) { return copy(b, valid), 0, 42, nil }},
		{"lost-buffer", func(context.Context, []byte) (int, int, uint32, error) { return 0, 0, 0, unix.ENOBUFS }},
		{"completion-error", prefixTestReceiver(valid, prefixTestMessage(unix.NLMSG_DONE, unix.NLM_F_MULTI, []byte{255, 255, 255, 255}))},
		{"ack-is-not-completion", prefixTestReceiver(valid, prefixTestMessage(unix.NLMSG_ERROR, 0, make([]byte, 20)))},
		{"overrun", prefixTestReceiver(valid, prefixTestMessage(unix.NLMSG_OVERRUN, 0, nil))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := readHostPrefixDump(context.Background(), tc.recv, unix.RTM_GETROUTE, unix.AF_INET, fixturePrefixSeq, fixturePrefixPort)
			if err == nil || len(out.Prefixes) != 0 || len(out.Warnings) != 0 {
				t.Fatalf("incomplete dump returned inventory or success: %#v, %v", out, err)
			}
		})
	}
	// Multipart datagrams are accepted only when the terminal status arrives.
	out, err := readHostPrefixDump(context.Background(), prefixTestReceiver(valid, valid, prefixTestDone()), unix.RTM_GETROUTE, unix.AF_INET, fixturePrefixSeq, fixturePrefixPort)
	if err != nil || len(out.Prefixes) != 1 {
		t.Fatalf("complete multipart dump failed or retained duplicates: %#v, %v", out, err)
	}
}

func TestHostPrefixRejectsMalformedEnvelopes(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire []byte
	}{
		{"short-header", []byte{1, 2, 3}},
		{"zero-length", make([]byte, 16)},
		{"undersized-length", func() []byte { b := prefixTestDone(); binary.NativeEndian.PutUint32(b, 15); return b }()},
		{"oversized-length", func() []byte { b := prefixTestDone(); binary.NativeEndian.PutUint32(b, 200); return b }()},
		{"wrong-sequence", func() []byte { b := prefixTestDone(); b[8]++; return b }()},
		{"wrong-port", func() []byte { b := prefixTestDone(); b[12]++; return b }()},
		{"done-without-status", prefixTestMessage(unix.NLMSG_DONE, unix.NLM_F_MULTI, nil)},
		{"done-short-status", prefixTestMessage(unix.NLMSG_DONE, unix.NLM_F_MULTI, []byte{0})},
		{"done-without-multi", prefixTestMessage(unix.NLMSG_DONE, 0, make([]byte, 4))},
		{"done-unknown-payload", prefixTestMessage(unix.NLMSG_DONE, unix.NLM_F_MULTI, make([]byte, 8))},
		{"malformed-extended-ack", prefixTestMessage(unix.NLMSG_DONE, unix.NLM_F_MULTI|unix.NLM_F_ACK_TLVS, make([]byte, 8))},
		{"short-error", prefixTestMessage(unix.NLMSG_ERROR, 0, make([]byte, 4))},
		{"noop", prefixTestMessage(unix.NLMSG_NOOP, 0, nil)},
		{"wrong-message-family", prefixTestMessage(unix.RTM_NEWADDR, unix.NLM_F_MULTI, prefixTestAddress(prefixTestIP(unix.IFA_LOCAL, "192.0.2.1")))},
		{"data-after-done", append(prefixTestDone(), prefixTestDone()...)},
		{"extra-trailing-byte", append(prefixTestDone(), 0)},
		{"missing-alignment", prefixTestMessage(unix.RTM_NEWROUTE, unix.NLM_F_MULTI, []byte{1})[:17]},
		{"without-multipart", prefixTestMessage(unix.RTM_NEWROUTE, 0, prefixTestRoute(prefixTestIP(unix.RTA_DST, "192.0.2.1")))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := hostPrefixDump{kind: unix.RTM_GETROUTE, family: unix.AF_INET, seq: fixturePrefixSeq, port: fixturePrefixPort}
			if err := d.consume(context.Background(), tc.wire); err == nil {
				t.Fatal("malformed envelope accepted")
			}
		})
	}
}

func TestHostPrefixRejectsMalformedRouteAndAddressAttributes(t *testing.T) {
	ip := prefixTestIP(unix.RTA_DST, "192.0.2.1")
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"short-route", make([]byte, 11)},
		{"wrong-family", func() []byte { b := prefixTestRoute(ip); b[0] = unix.AF_INET6; return b }()},
		{"long-prefix", func() []byte { b := prefixTestRoute(ip); b[1] = 33; return b }()},
		{"long-source-prefix", func() []byte { b := prefixTestRoute(ip); b[2] = 33; return b }()},
		{"missing-destination", prefixTestRoute()},
		{"missing-source", func() []byte { b := prefixTestRoute(ip); b[2] = 24; return b }()},
		{"destination-wrong-width", prefixTestRoute(prefixTestIP(unix.RTA_DST, "2001:db8::1"))},
		{"table-wrong-width", prefixTestRoute(ip, prefixTestAttr(unix.RTA_TABLE, []byte{1}))},
		{"table-conflict", prefixTestRoute(ip, prefixTestU32(unix.RTA_TABLE, 1000), prefixTestU32(unix.RTA_TABLE, 1001))},
		{"destination-conflict", prefixTestRoute(ip, prefixTestIP(unix.RTA_DST, "192.0.2.2"))},
		{"negative-interface", prefixTestRoute(ip, prefixTestU32(unix.RTA_OIF, 0xffffffff))},
		{"unknown-attribute-conflict", prefixTestRoute(ip, prefixTestU32(999, 1), prefixTestU32(999, 2))},
		{"short-attribute", prefixTestRoute(ip, []byte{1, 2, 3})},
		{"attribute-length-too-short", prefixTestRoute(ip, []byte{3, 0, 1, 0})},
		{"attribute-length-too-long", prefixTestRoute(ip, []byte{255, 255, 1, 0})},
		{"missing-attribute-alignment", prefixTestRoute(ip, prefixTestAttr(999, []byte{1})[:5])},
		{"bad-metrics", prefixTestRoute(ip, prefixTestAttr(unix.RTA_METRICS, []byte{0, 0, 0, 0}))},
		{"bad-nested-unknown", prefixTestRoute(ip, prefixTestAttr(999|unix.NLA_F_NESTED, []byte{0, 0, 0, 0}))},
		{"unexpected-ip-byte-order", prefixTestRoute(prefixTestAttr(unix.RTA_DST|unix.NLA_F_NET_BYTEORDER, []byte{192, 0, 2, 1}))},
		{"short-via", prefixTestRoute(ip, prefixTestAttr(unix.RTA_VIA, []byte{2}))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := decodeHostRoute(tc.body, unix.AF_INET); err == nil {
				t.Fatal("malformed route accepted")
			}
		})
	}
	for _, body := range [][]byte{
		make([]byte, 7), prefixTestAddress(),
		prefixTestAddress(prefixTestIP(unix.IFA_LOCAL, "2001:db8::1")),
		prefixTestAddress(prefixTestIP(unix.IFA_LOCAL, "192.0.2.1"), prefixTestIP(unix.IFA_LOCAL, "192.0.2.2")),
		prefixTestAddress(prefixTestIP(unix.IFA_LOCAL, "192.0.2.1"), prefixTestAttr(unix.IFA_FLAGS, []byte{1})),
		func() []byte { b := prefixTestAddress(prefixTestIP(unix.IFA_LOCAL, "192.0.2.1")); b[4] = 0; return b }(),
		func() []byte { b := prefixTestAddress(prefixTestIP(unix.IFA_LOCAL, "192.0.2.1")); b[1] = 33; return b }(),
	} {
		if _, err := decodeHostAddresses(body, unix.AF_INET); err == nil {
			t.Fatal("malformed address accepted")
		}
	}
	// Identical duplicate attributes do not lose information and are accepted.
	if _, _, err := decodeHostRoute(prefixTestRoute(ip, ip, prefixTestU32(unix.RTA_TABLE, 1000), prefixTestU32(unix.RTA_TABLE, 1000)), unix.AF_INET); err != nil {
		t.Fatal(err)
	}
}

func TestHostPrefixRejectsMalformedMultipath(t *testing.T) {
	hop := func(index uint32, attrs ...[]byte) []byte {
		b := make([]byte, 8)
		binary.NativeEndian.PutUint32(b[4:], index)
		for _, a := range attrs {
			b = append(b, a...)
		}
		binary.NativeEndian.PutUint16(b, uint16(len(b)))
		return b
	}
	for _, data := range [][]byte{
		nil, make([]byte, 7), make([]byte, 8), hop(0xffffffff),
		append(hop(2), 0), hop(2, prefixTestIP(unix.RTA_GATEWAY, "2001:db8::1")),
		hop(2, prefixTestU32(unix.RTA_OIF, 3)), hop(2, prefixTestIP(unix.RTA_DST, "192.0.2.1")),
		hop(2, prefixTestAttr(unix.RTA_MULTIPATH, hop(3))), hop(2, []byte{255, 255, 1, 0}),
	} {
		if _, err := decodePrefixMultipath(prefixAttribute{data: data}, 4); err == nil {
			t.Fatal("malformed multipath accepted")
		}
	}
	if _, err := decodePrefixMultipath(prefixAttribute{flags: unix.NLA_F_NESTED, data: hop(2)}, 4); err == nil {
		t.Fatal("unknown multipath encoding accepted")
	}
}

func TestHostPrefixResourceBoundsAndCancellation(t *testing.T) {
	for _, d := range []hostPrefixDump{
		{bytes: prefixDumpByteLimit}, {messages: prefixMessageLimit},
	} {
		d.kind, d.family, d.seq, d.port = unix.RTM_GETROUTE, unix.AF_INET, fixturePrefixSeq, fixturePrefixPort
		if err := d.consume(context.Background(), prefixTestDone()); err == nil {
			t.Fatal("dump bound not enforced")
		}
	}
	d := hostPrefixDump{}
	if err := d.consume(context.Background(), make([]byte, prefixDatagramLimit+1)); err == nil {
		t.Fatal("datagram bound not enforced")
	}
	attrs := bytes.Repeat(prefixTestU32(999, 1), prefixAttributeLimit+1)
	if _, err := prefixAttributes(attrs, 0); err == nil {
		t.Fatal("attribute count bound not enforced")
	}
	nested := prefixTestU32(999, 1)
	for i := 0; i <= prefixNestingLimit; i++ {
		nested = prefixTestAttr(1000|unix.NLA_F_NESTED, nested)
	}
	if _, err := prefixAttributes(nested, 0); err == nil {
		t.Fatal("attribute nesting bound not enforced")
	}
	d = hostPrefixDump{kind: unix.RTM_GETROUTE, family: unix.AF_INET, seq: fixturePrefixSeq, port: fixturePrefixPort,
		result: domain.HostNetworkPrefixes{Prefixes: make([]domain.HostNetworkPrefix, prefixEntryLimit)}}
	if err := d.consume(context.Background(), prefixTestMessage(unix.RTM_NEWROUTE, unix.NLM_F_MULTI, prefixTestRoute(prefixTestIP(unix.RTA_DST, "192.0.2.1")))); err == nil {
		t.Fatal("prefix count bound not enforced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := ObserveHostNetworkPrefixes(ctx); !errors.Is(err, context.Canceled) || len(out.Prefixes) != 0 {
		t.Fatal("pre-canceled observation did not return cancellation without inventory")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	recv := func(_ context.Context, b []byte) (int, int, uint32, error) {
		cancel()
		return copy(b, prefixTestDone()), 0, 0, nil
	}
	if out, err := readHostPrefixDump(ctx, recv, unix.RTM_GETROUTE, unix.AF_INET, fixturePrefixSeq, fixturePrefixPort); !errors.Is(err, context.Canceled) || len(out.Prefixes) != 0 {
		t.Fatal("cancellation during receive did not discard inventory")
	}
}

func TestHostPrefixIdleReceiveHonorsDeadline(t *testing.T) {
	// An empty local socket pair exercises the real nonblocking receive/poll
	// loop without host networking, a privileged socket or a fake clock.
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fds[0])
	defer unix.Close(fds[1])
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, _, err = receiveHostPrefixDatagram(ctx, fds[0], make([]byte, 256))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("idle receive did not stop at a bounded deadline: %v", err)
	}
}

func TestHostPrefixCanonicalOrderRetainsProvenance(t *testing.T) {
	entries := []domain.HostNetworkPrefix{
		{CIDR: "192.0.2.0/24", Source: "route", InterfaceIndex: 5, Table: 1000},
		{CIDR: "192.0.2.0/24", Source: "address", InterfaceIndex: 2},
		{CIDR: "192.0.2.0/24", Source: "route", InterfaceIndex: 2, Table: 254},
		{CIDR: "192.0.2.0/24", Source: "route", InterfaceIndex: 3, Table: 254},
	}
	want := canonicalHostPrefixes(domain.HostNetworkPrefixes{Prefixes: entries, Warnings: []string{"b", "a", "b"}})
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	got := canonicalHostPrefixes(domain.HostNetworkPrefixes{Prefixes: append(entries, entries...), Warnings: []string{"a", "b", "a"}})
	if !reflect.DeepEqual(want, got) || len(got.Prefixes) != 4 || !reflect.DeepEqual(got.Warnings, []string{"a", "b"}) {
		t.Fatal("deduplication changed provenance or ordering is not deterministic")
	}
}

func TestHostPrefixReadOnlySmoke(t *testing.T) {
	if os.Getenv("VIRMILL_HOST_PREFIX_SMOKE") != "1" {
		t.Skip("set VIRMILL_HOST_PREFIX_SMOKE=1 for an ordinary-user read-only rtnetlink observation")
	}
	if os.Geteuid() == 0 {
		t.Fatal("run the read-only smoke as the ordinary user")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := ObserveHostNetworkPrefixes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range out.Prefixes {
		p, err := netip.ParsePrefix(item.CIDR)
		if err != nil || p.Masked().String() != item.CIDR {
			t.Fatal("noncanonical observed prefix (address redacted)")
		}
		if item.Source != "address" && item.Source != "route" {
			t.Fatal("unknown observed prefix provenance")
		}
	}
	t.Logf("read-only current-namespace observation: %d prefixes, %d warnings; no addresses logged; no routing or isolation qualification", len(out.Prefixes), len(out.Warnings))
}

func FuzzHostPrefixDecoder(f *testing.F) {
	f.Add(prefixTestMessage(unix.RTM_NEWROUTE, unix.NLM_F_MULTI, prefixTestRoute(prefixTestIP(unix.RTA_DST, "192.0.2.1"))), byte(unix.RTM_GETROUTE), byte(unix.AF_INET))
	f.Add(prefixTestMessage(unix.RTM_NEWADDR, unix.NLM_F_MULTI, prefixTestAddress(prefixTestIP(unix.IFA_LOCAL, "192.0.2.1"))), byte(unix.RTM_GETADDR), byte(unix.AF_INET))
	f.Add(prefixTestDone(), byte(unix.RTM_GETROUTE), byte(unix.AF_INET6))
	f.Fuzz(func(t *testing.T, b []byte, kind, family byte) {
		d := hostPrefixDump{kind: uint16(kind), family: family, seq: fixturePrefixSeq, port: fixturePrefixPort}
		if err := d.consume(context.Background(), b); err != nil {
			return
		}
		for _, entry := range d.result.Prefixes {
			p, err := netip.ParsePrefix(entry.CIDR)
			if err != nil || p.Masked().String() != entry.CIDR {
				t.Fatal("accepted prefix is not canonical")
			}
		}
	})
}
