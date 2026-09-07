//go:build linux

package linux

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/domain"
)

const (
	prefixObserveTimeout = 5 * time.Second
	prefixDatagramLimit  = 1 << 20
	prefixDumpByteLimit  = 16 << 20
	prefixMessageLimit   = 65536
	prefixEntryLimit     = 131072
	prefixAttributeLimit = 4096
	prefixNestingLimit   = 8
	// RTA_NH_ID is defined in Linux UAPI rtnetlink.h but not the pinned x/sys.
	prefixRTANexthopID = 30
)

// ObserveHostNetworkPrefixes reads the calling network namespace through
// unprivileged rtnetlink dumps. It never changes links, routes or firewall state.
// Every family and table must finish successfully; errors return no inventory.
// This is an observation, not an atomic snapshot or a packet-isolation test.
func ObserveHostNetworkPrefixes(ctx context.Context) (domain.HostNetworkPrefixes, error) {
	ctx, cancel := context.WithTimeout(ctx, prefixObserveTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return domain.HostNetworkPrefixes{}, err
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_ROUTE)
	if err != nil {
		return domain.HostNetworkPrefixes{}, fmt.Errorf("open host-prefix netlink socket: %w", err)
	}
	defer unix.Close(fd)
	if err = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return domain.HostNetworkPrefixes{}, fmt.Errorf("bind host-prefix netlink socket: %w", err)
	}
	sa, err := unix.Getsockname(fd)
	if err != nil {
		return domain.HostNetworkPrefixes{}, fmt.Errorf("identify host-prefix netlink socket: %w", err)
	}
	local, ok := sa.(*unix.SockaddrNetlink)
	if !ok || local.Pid == 0 {
		return domain.HostNetworkPrefixes{}, errors.New("host-prefix socket has no netlink port identity")
	}
	var result domain.HostNetworkPrefixes
	var seq uint32
	for _, kind := range []uint16{unix.RTM_GETADDR, unix.RTM_GETROUTE} {
		for _, family := range []byte{unix.AF_INET, unix.AF_INET6} {
			if err := ctx.Err(); err != nil {
				return domain.HostNetworkPrefixes{}, err
			}
			seq++
			request := hostPrefixDumpRequest(kind, family, seq, local.Pid)
			if err := unix.Sendto(fd, request, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
				return domain.HostNetworkPrefixes{}, fmt.Errorf("request host-prefix dump: %w", err)
			}
			part, err := readHostPrefixDump(ctx, func(ctx context.Context, buf []byte) (int, int, uint32, error) {
				return receiveHostPrefixDatagram(ctx, fd, buf)
			}, kind, family, seq, local.Pid)
			if err != nil {
				return domain.HostNetworkPrefixes{}, fmt.Errorf("host-prefix dump type %d family %d: %w", kind, family, err)
			}
			result.Prefixes = append(result.Prefixes, part.Prefixes...)
			result.Warnings = append(result.Warnings, part.Warnings...)
			if len(result.Prefixes) > prefixEntryLimit {
				return domain.HostNetworkPrefixes{}, errors.New("host-prefix inventory exceeds entry limit")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return domain.HostNetworkPrefixes{}, err
	}
	return canonicalHostPrefixes(result), nil
}

func hostPrefixDumpRequest(kind uint16, family byte, seq, port uint32) []byte {
	size := unix.SizeofIfAddrmsg
	if kind == unix.RTM_GETROUTE {
		size = unix.SizeofRtMsg
	}
	b := make([]byte, unix.NLMSG_HDRLEN+size)
	binary.NativeEndian.PutUint32(b, uint32(len(b)))
	binary.NativeEndian.PutUint16(b[4:], kind)
	binary.NativeEndian.PutUint16(b[6:], unix.NLM_F_REQUEST|unix.NLM_F_DUMP)
	binary.NativeEndian.PutUint32(b[8:], seq)
	binary.NativeEndian.PutUint32(b[12:], port)
	b[unix.NLMSG_HDRLEN] = family
	// All other fields remain zero, including rtm_table=RT_TABLE_UNSPEC.
	return b
}

func receiveHostPrefixDatagram(ctx context.Context, fd int, buf []byte) (int, int, uint32, error) {
	for {
		if err := ctx.Err(); err != nil {
			return 0, 0, 0, err
		}
		n, _, flags, sa, err := unix.Recvmsg(fd, buf, nil, unix.MSG_DONTWAIT)
		if err == nil {
			from, ok := sa.(*unix.SockaddrNetlink)
			if !ok || from.Family != unix.AF_NETLINK || from.Groups != 0 {
				return 0, 0, 0, errors.New("unexpected host-prefix netlink sender")
			}
			return n, flags, from.Pid, nil
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if !errors.Is(err, unix.EAGAIN) {
			return 0, 0, 0, err // Includes ENOBUFS: never use a lossy dump.
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		_, err = unix.Poll(fds, 50)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return 0, 0, 0, err
		}
		if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return 0, 0, 0, errors.New("host-prefix socket lost dump data or closed")
		}
	}
}

type hostPrefixReceiver func(context.Context, []byte) (n, flags int, sender uint32, err error)

func readHostPrefixDump(ctx context.Context, receive hostPrefixReceiver, kind uint16, family byte, seq, port uint32) (domain.HostNetworkPrefixes, error) {
	d := hostPrefixDump{kind: kind, family: family, seq: seq, port: port}
	buf := make([]byte, prefixDatagramLimit)
	for !d.done {
		if err := ctx.Err(); err != nil {
			return domain.HostNetworkPrefixes{}, err
		}
		n, flags, sender, err := receive(ctx, buf)
		if err != nil {
			return domain.HostNetworkPrefixes{}, fmt.Errorf("receive incomplete host-prefix dump: %w", err)
		}
		if n <= 0 || n > len(buf) || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
			return domain.HostNetworkPrefixes{}, errors.New("empty or truncated host-prefix datagram")
		}
		if sender != 0 {
			return domain.HostNetworkPrefixes{}, errors.New("host-prefix response did not originate from kernel")
		}
		if err := d.consume(ctx, buf[:n]); err != nil {
			return domain.HostNetworkPrefixes{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return domain.HostNetworkPrefixes{}, err
	}
	return canonicalHostPrefixes(d.result), nil
}

type hostPrefixDump struct {
	kind            uint16
	family          byte
	seq, port       uint32
	bytes, messages int
	done            bool
	result          domain.HostNetworkPrefixes
}

func (d *hostPrefixDump) consume(ctx context.Context, b []byte) error {
	d.bytes += len(b)
	if len(b) > prefixDatagramLimit || d.bytes > prefixDumpByteLimit {
		return errors.New("host-prefix dump exceeds byte limit")
	}
	for len(b) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.done {
			return errors.New("host-prefix data follows dump completion")
		}
		d.messages++
		if d.messages > prefixMessageLimit {
			return errors.New("host-prefix dump exceeds message limit")
		}
		if len(b) < unix.NLMSG_HDRLEN {
			return errors.New("short host-prefix netlink header")
		}
		n64 := uint64(binary.NativeEndian.Uint32(b))
		if n64 < unix.NLMSG_HDRLEN || n64 > uint64(len(b)) {
			return errors.New("invalid host-prefix netlink message length")
		}
		n := int(n64)
		if prefixAlign(n) > len(b) {
			return errors.New("missing host-prefix netlink alignment bytes")
		}
		kind, flags := binary.NativeEndian.Uint16(b[4:]), binary.NativeEndian.Uint16(b[6:])
		if binary.NativeEndian.Uint32(b[8:]) != d.seq || binary.NativeEndian.Uint32(b[12:]) != d.port {
			return errors.New("host-prefix response sequence or port mismatch")
		}
		if flags&(unix.NLM_F_DUMP_INTR|unix.NLM_F_DUMP_FILTERED) != 0 {
			return errors.New("interrupted or filtered host-prefix dump")
		}
		payload := b[unix.NLMSG_HDRLEN:n]
		switch kind {
		case unix.NLMSG_DONE:
			if flags&unix.NLM_F_MULTI == 0 || len(payload) < 4 {
				return errors.New("malformed host-prefix dump completion")
			}
			status := int32(binary.NativeEndian.Uint32(payload))
			if status != 0 {
				return fmt.Errorf("host-prefix dump completed with kernel error %d", status)
			}
			if len(payload) > 4 {
				if flags&unix.NLM_F_ACK_TLVS == 0 {
					return errors.New("unexpected host-prefix completion payload")
				}
				if _, err := prefixAttributes(payload[4:], 0); err != nil {
					return err
				}
			}
			d.done = true
		case unix.NLMSG_ERROR:
			// ACKs are not requested; even a zero ACK cannot complete a dump.
			if len(payload) < 4+unix.NLMSG_HDRLEN {
				return errors.New("malformed host-prefix netlink error")
			}
			return fmt.Errorf("host-prefix netlink error response %d", int32(binary.NativeEndian.Uint32(payload)))
		case unix.NLMSG_OVERRUN:
			return errors.New("host-prefix netlink overrun")
		default:
			expected := uint16(unix.RTM_NEWADDR)
			if d.kind == unix.RTM_GETROUTE {
				expected = unix.RTM_NEWROUTE
			}
			if kind != expected || flags&unix.NLM_F_MULTI == 0 {
				return errors.New("unexpected host-prefix dump message type or flags")
			}
			var entries []domain.HostNetworkPrefix
			var warnings []string
			var err error
			if d.kind == unix.RTM_GETADDR {
				entries, err = decodeHostAddresses(payload, d.family)
			} else {
				entries, warnings, err = decodeHostRoute(payload, d.family)
			}
			if err != nil {
				return err
			}
			d.result.Prefixes = append(d.result.Prefixes, entries...)
			d.result.Warnings = append(d.result.Warnings, warnings...)
			if len(d.result.Prefixes) > prefixEntryLimit {
				return errors.New("host-prefix dump exceeds entry limit")
			}
		}
		b = b[prefixAlign(n):]
	}
	return nil
}

type prefixAttribute struct {
	flags uint16
	data  []byte
}

func prefixAlign(n int) int { return (n + 3) &^ 3 }

func prefixAttributes(b []byte, depth int) (map[uint16]prefixAttribute, error) {
	if depth > prefixNestingLimit {
		return nil, errors.New("host-prefix attributes exceed nesting limit")
	}
	attrs := make(map[uint16]prefixAttribute)
	for count := 0; len(b) > 0; count++ {
		if count >= prefixAttributeLimit || len(b) < 4 {
			return nil, errors.New("short or excessive host-prefix attributes")
		}
		n := int(binary.NativeEndian.Uint16(b))
		if n < 4 || n > len(b) || prefixAlign(n) > len(b) {
			return nil, errors.New("invalid host-prefix attribute length or alignment")
		}
		raw := binary.NativeEndian.Uint16(b[2:])
		kind, flags := raw&0x3fff, raw&0xc000
		a := prefixAttribute{flags: flags, data: b[4:n]}
		if old, ok := attrs[kind]; ok && (old.flags != flags || !bytes.Equal(old.data, a.data)) {
			return nil, fmt.Errorf("conflicting duplicate host-prefix attribute %d", kind)
		}
		if flags&unix.NLA_F_NESTED != 0 {
			if _, err := prefixAttributes(a.data, depth+1); err != nil {
				return nil, err
			}
		}
		attrs[kind] = a
		b = b[prefixAlign(n):]
	}
	return attrs, nil
}

func prefixAttrSize(a prefixAttribute, size int) error {
	if len(a.data) != size || a.flags != 0 {
		return errors.New("invalid host-prefix attribute size or encoding")
	}
	return nil
}

func prefixFamilySize(family byte) (int, error) {
	switch family {
	case unix.AF_INET:
		return 4, nil
	case unix.AF_INET6:
		return 16, nil
	default:
		return 0, errors.New("unsupported host-prefix address family")
	}
}

func decodeHostAddresses(b []byte, family byte) ([]domain.HostNetworkPrefix, error) {
	size, err := prefixFamilySize(family)
	if err != nil {
		return nil, err
	}
	if len(b) < unix.SizeofIfAddrmsg || b[0] != family || int(b[1]) > size*8 {
		return nil, errors.New("invalid host-prefix address message")
	}
	index := binary.NativeEndian.Uint32(b[4:])
	if index == 0 || index > 0x7fffffff {
		return nil, errors.New("invalid host-prefix address interface index")
	}
	attrs, err := prefixAttributes(b[unix.SizeofIfAddrmsg:], 0)
	if err != nil {
		return nil, err
	}
	for kind, a := range attrs {
		switch kind {
		case unix.IFA_ADDRESS, unix.IFA_LOCAL, unix.IFA_BROADCAST, unix.IFA_ANYCAST, unix.IFA_MULTICAST:
			err = prefixAttrSize(a, size)
		case unix.IFA_FLAGS, unix.IFA_RT_PRIORITY, unix.IFA_TARGET_NETNSID:
			err = prefixAttrSize(a, 4)
		case unix.IFA_CACHEINFO:
			err = prefixAttrSize(a, 16)
		}
		if err != nil {
			return nil, err
		}
	}
	var out []domain.HostNetworkPrefix
	// IFA_ADDRESS can be a point-to-point peer rather than IFA_LOCAL. Keep both
	// networks, and keep tentative/deprecated addresses as occupied prefixes.
	for _, kind := range []uint16{unix.IFA_LOCAL, unix.IFA_ADDRESS} {
		if a, ok := attrs[kind]; ok {
			addr, _ := netip.AddrFromSlice(a.data) // Exact family length checked above.
			out = append(out, domain.HostNetworkPrefix{CIDR: netip.PrefixFrom(addr, int(b[1])).Masked().String(), Source: "address", InterfaceIndex: index})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("host-prefix address message has no local or peer address")
	}
	return out, nil
}

func decodeHostRoute(b []byte, family byte) ([]domain.HostNetworkPrefix, []string, error) {
	size, err := prefixFamilySize(family)
	if err != nil {
		return nil, nil, err
	}
	if len(b) < unix.SizeofRtMsg || b[0] != family || int(b[1]) > size*8 || int(b[2]) > size*8 {
		return nil, nil, errors.New("invalid host-prefix route message")
	}
	attrs, err := prefixAttributes(b[unix.SizeofRtMsg:], 0)
	if err != nil {
		return nil, nil, err
	}
	if err := validatePrefixRouteAttributes(attrs, size); err != nil {
		return nil, nil, err
	}
	dst, ok := attrs[unix.RTA_DST]
	if !ok {
		if b[1] != 0 {
			return nil, nil, errors.New("non-default host-prefix route has no destination")
		}
		dst.data = make([]byte, size)
	}
	if _, ok := attrs[unix.RTA_SRC]; !ok && b[2] != 0 {
		return nil, nil, errors.New("source-specific host-prefix route has no source")
	}
	addr, _ := netip.AddrFromSlice(dst.data)
	cidr := netip.PrefixFrom(addr, int(b[1])).Masked().String()
	table := uint32(b[4])
	if a, ok := attrs[unix.RTA_TABLE]; ok {
		table = binary.NativeEndian.Uint32(a.data) // UAPI: extended table overrides rtm_table.
	}
	var interfaces []uint32
	for _, kind := range []uint16{unix.RTA_OIF, unix.RTA_IIF} {
		if a, ok := attrs[kind]; ok {
			index := binary.NativeEndian.Uint32(a.data)
			if index > 0x7fffffff {
				return nil, nil, errors.New("invalid host-prefix route interface index")
			}
			interfaces = append(interfaces, index)
		}
	}
	if a, ok := attrs[unix.RTA_MULTIPATH]; ok {
		indices, err := decodePrefixMultipath(a, size)
		if err != nil {
			return nil, nil, err
		}
		interfaces = append(interfaces, indices...)
	}
	var warnings []string
	if _, ok := attrs[prefixRTANexthopID]; ok {
		// Nexthop objects may point to groups outside this dump. Do not drop the
		// destination or pretend that its interface identity has been resolved.
		interfaces = append(interfaces, 0)
		warnings = append(warnings, "HOST_PREFIX_NEXTHOP_INTERFACE_UNRESOLVED: route destinations are retained; nexthop-object interfaces were not resolved")
	}
	if len(interfaces) == 0 {
		interfaces = append(interfaces, 0) // Includes blackhole/unreachable routes.
	}
	out := make([]domain.HostNetworkPrefix, 0, len(interfaces))
	for _, index := range interfaces {
		out = append(out, domain.HostNetworkPrefix{CIDR: cidr, Source: "route", InterfaceIndex: index, Table: table})
	}
	return out, warnings, nil
}

func validatePrefixRouteAttributes(attrs map[uint16]prefixAttribute, size int) error {
	for kind, a := range attrs {
		var err error
		switch kind {
		case unix.RTA_DST, unix.RTA_SRC, unix.RTA_GATEWAY, unix.RTA_PREFSRC:
			err = prefixAttrSize(a, size)
		case unix.RTA_IIF, unix.RTA_OIF, unix.RTA_PRIORITY, unix.RTA_FLOW, unix.RTA_TABLE, unix.RTA_MARK, unix.RTA_EXPIRES, unix.RTA_UID, prefixRTANexthopID:
			err = prefixAttrSize(a, 4)
		case unix.RTA_PREF, unix.RTA_TTL_PROPAGATE, unix.RTA_IP_PROTO:
			err = prefixAttrSize(a, 1)
		case unix.RTA_ENCAP_TYPE, unix.RTA_SPORT, unix.RTA_DPORT:
			err = prefixAttrSize(a, 2)
		case unix.RTA_CACHEINFO:
			err = prefixAttrSize(a, 32)
		case unix.RTA_METRICS:
			if a.flags&^uint16(unix.NLA_F_NESTED) != 0 {
				return errors.New("invalid host-prefix route metrics encoding")
			}
			_, err = prefixAttributes(a.data, 1)
		case unix.RTA_VIA:
			if len(a.data) < 2 || a.flags != 0 {
				return errors.New("invalid host-prefix route via attribute")
			}
			viaFamily := binary.NativeEndian.Uint16(a.data)
			if viaFamily != unix.AF_INET && viaFamily != unix.AF_INET6 {
				return errors.New("unsupported host-prefix route via family")
			}
			viaSize, _ := prefixFamilySize(byte(viaFamily))
			err = prefixAttrSize(a, 2+viaSize)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func decodePrefixMultipath(a prefixAttribute, size int) ([]uint32, error) {
	if a.flags != 0 || len(a.data) == 0 {
		return nil, errors.New("empty or invalid host-prefix multipath encoding")
	}
	var indices []uint32
	for b := a.data; len(b) > 0; {
		if len(indices) >= prefixAttributeLimit || len(b) < 8 {
			return nil, errors.New("short or excessive host-prefix multipath entries")
		}
		n := int(binary.NativeEndian.Uint16(b))
		if n < 8 || n > len(b) || prefixAlign(n) > len(b) {
			return nil, errors.New("invalid host-prefix multipath entry length")
		}
		index := binary.NativeEndian.Uint32(b[4:])
		if index > 0x7fffffff {
			return nil, errors.New("invalid host-prefix multipath interface index")
		}
		attrs, err := prefixAttributes(b[8:n], 1)
		if err != nil {
			return nil, err
		}
		if err := validatePrefixRouteAttributes(attrs, size); err != nil {
			return nil, err
		}
		// Nested multipath/destination/interface/table attributes are not the
		// rtnexthop layout. Refuse ambiguous layouts instead of overlooking them.
		for _, kind := range []uint16{unix.RTA_DST, unix.RTA_SRC, unix.RTA_MULTIPATH, unix.RTA_TABLE, unix.RTA_IIF, unix.RTA_OIF, prefixRTANexthopID} {
			if _, ok := attrs[kind]; ok {
				return nil, errors.New("unsupported host-prefix multipath attribute layout")
			}
		}
		indices = append(indices, index) // Retain dead/link-down paths too.
		b = b[prefixAlign(n):]
	}
	return indices, nil
}

func canonicalHostPrefixes(in domain.HostNetworkPrefixes) domain.HostNetworkPrefixes {
	seen := make(map[domain.HostNetworkPrefix]struct{}, len(in.Prefixes))
	out := domain.HostNetworkPrefixes{Prefixes: []domain.HostNetworkPrefix{}, Warnings: []string{}}
	for _, prefix := range in.Prefixes {
		if _, ok := seen[prefix]; !ok {
			seen[prefix] = struct{}{}
			out.Prefixes = append(out.Prefixes, prefix)
		}
	}
	sort.Slice(out.Prefixes, func(i, j int) bool {
		a, b := out.Prefixes[i], out.Prefixes[j]
		if a.CIDR != b.CIDR {
			return a.CIDR < b.CIDR
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		return a.InterfaceIndex < b.InterfaceIndex
	})
	warnings := make(map[string]bool)
	for _, warning := range in.Warnings {
		if !warnings[warning] {
			warnings[warning] = true
			out.Warnings = append(out.Warnings, warning)
		}
	}
	sort.Strings(out.Warnings)
	return out
}
