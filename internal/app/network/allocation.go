package network

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/netip"
	"sort"

	"virmill.local/core/internal/domain"
)

type AllocationRange struct {
	CIDR         string `json:"cidr"`
	PrefixLength int    `json:"prefixLength"`
}

type PlannedAllocation struct {
	ID   string `json:"id"`
	CIDR string `json:"cidr"`
}

type AllocationConfig struct {
	Version int                 `json:"version"`
	Ranges  []AllocationRange   `json:"ranges"`
	Planned []PlannedAllocation `json:"planned,omitempty"`
}

// DefaultAllocationConfig returns ordered search ranges, not a claim that any
// range is unused. Each call owns its slices.
func DefaultAllocationConfig() AllocationConfig {
	return AllocationConfig{Version: 1, Ranges: []AllocationRange{
		{CIDR: "10.0.0.0/8", PrefixLength: 24},
		{CIDR: "172.16.0.0/12", PrefixLength: 24},
		{CIDR: "192.168.0.0/16", PrefixLength: 24},
	}}
}

// ValidateAllocationConfig checks only the declaration. Planned reservations
// may overlap one another and may lie outside the search ranges; both remain
// occupied. A /31 or /32 reservation still blocks a containing output subnet.
func ValidateAllocationConfig(c AllocationConfig) error {
	if c.Version != 1 || len(c.Ranges) < 1 || len(c.Ranges) > 16 || len(c.Planned) > 256 {
		return domain.Fail("INVALID_INPUT", "allocation configuration requires version 1, 1..16 ranges and at most 256 planned reservations")
	}
	ranges := make([]netip.Prefix, 0, len(c.Ranges))
	for i, r := range c.Ranges {
		p, ok := allocationPrivatePrefix(r.CIDR)
		if !ok || r.PrefixLength < 8 || r.PrefixLength < p.Bits() || r.PrefixLength > 30 {
			return domain.Fail("INVALID_INPUT", fmt.Sprintf("allocation range %d requires a canonical RFC1918 pool and subnet prefix between the pool prefix and /30", i))
		}
		for _, prior := range ranges {
			if p.Overlaps(prior) {
				return domain.Fail("INVALID_INPUT", "allocation search ranges must be disjoint")
			}
		}
		ranges = append(ranges, p)
	}
	ids := make(map[string]bool, len(c.Planned))
	for i, planned := range c.Planned {
		if !allocationID(planned.ID) || ids[planned.ID] {
			return domain.Fail("INVALID_INPUT", fmt.Sprintf("planned allocation %d requires a unique ID matching [A-Za-z0-9][A-Za-z0-9_.:-]{0,127}", i))
		}
		if _, ok := allocationPrivatePrefix(planned.CIDR); !ok {
			return domain.Fail("INVALID_INPUT", fmt.Sprintf("planned allocation %d requires a canonical RFC1918 IPv4 prefix", i))
		}
		ids[planned.ID] = true
	}
	return nil
}

func allocationID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		ch := id[i]
		if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' {
			continue
		}
		if i == 0 || ch != '_' && ch != '.' && ch != ':' && ch != '-' {
			return false
		}
	}
	return true
}

func allocationPrivatePrefix(raw string) (netip.Prefix, bool) {
	// Canonical IPv4 CIDRs cannot exceed 18 bytes; bound parsing independently
	// of any surrounding configuration reader.
	if len(raw) > 18 {
		return netip.Prefix{}, false
	}
	p, err := netip.ParsePrefix(raw)
	if err != nil || !p.Addr().Is4() || p != p.Masked() || p.String() != raw {
		return netip.Prefix{}, false
	}
	a := p.Addr().As4()
	private := a[0] == 10 && p.Bits() >= 8 ||
		a[0] == 172 && a[1] >= 16 && a[1] <= 31 && p.Bits() >= 12 ||
		a[0] == 192 && a[1] == 168 && p.Bits() >= 16
	return p, private
}

// allocationInterval is half-open. uint64 represents the end of IPv4 /0
// (2^32) without wrapping, even though every address fits in uint32.
type allocationInterval struct{ start, end uint64 }

func allocationSpan(p netip.Prefix) allocationInterval {
	a := p.Masked().Addr().As4()
	start := uint64(binary.BigEndian.Uint32(a[:]))
	return allocationInterval{start, start + uint64(1)<<(32-p.Bits())}
}

// AllocateConfigured selects the first free aligned subnet in range order.
// Native IPv4 host bits are masked; valid non-mapped IPv6 prefixes cannot
// overlap IPv4. Every other input must be valid. In particular, IPv4 /0 is
// occupied here: only the caller can distinguish host default routes from
// assigned, defined or planned prefixes and exclude appropriate host routes.
//
// Sorting and merging occupied intervals bounds work by input size, rather
// than enumerating millions of small subnets inside a large pool. Neither
// configuration nor occupied input is modified. This pure function does not
// observe or reserve host state; the caller must recheck before mutation.
func AllocateConfigured(ctx context.Context, c AllocationConfig, occupied []netip.Prefix) (netip.Prefix, error) {
	if err := ctx.Err(); err != nil {
		return netip.Prefix{}, err
	}
	if err := ValidateAllocationConfig(c); err != nil {
		return netip.Prefix{}, err
	}
	if len(occupied) > 65536 {
		return netip.Prefix{}, domain.Fail("INVALID_INPUT", "occupied prefix limit is 65536")
	}
	spans := make([]allocationInterval, 0, len(occupied)+len(c.Planned))
	for i, p := range occupied {
		if err := ctx.Err(); err != nil {
			return netip.Prefix{}, err
		}
		if !p.IsValid() || p.Addr().Is4In6() {
			return netip.Prefix{}, domain.Fail("INVALID_INPUT", fmt.Sprintf("occupied prefix %d is invalid or IPv4-mapped", i))
		}
		if p.Addr().Is4() {
			spans = append(spans, allocationSpan(p))
		}
	}
	for _, planned := range c.Planned {
		if err := ctx.Err(); err != nil {
			return netip.Prefix{}, err
		}
		p, _ := netip.ParsePrefix(planned.CIDR) // validated above
		spans = append(spans, allocationSpan(p))
	}
	if err := ctx.Err(); err != nil {
		return netip.Prefix{}, err
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end < spans[j].end
		}
		return spans[i].start < spans[j].start
	})
	merged := spans[:0]
	for _, s := range spans {
		if err := ctx.Err(); err != nil {
			return netip.Prefix{}, err
		}
		if n := len(merged); n > 0 && s.start <= merged[n-1].end {
			if s.end > merged[n-1].end {
				merged[n-1].end = s.end
			}
		} else {
			merged = append(merged, s)
		}
	}
	for _, r := range c.Ranges {
		if err := ctx.Err(); err != nil {
			return netip.Prefix{}, err
		}
		p, _ := netip.ParsePrefix(r.CIDR) // validated above
		pool := allocationSpan(p)
		size := uint64(1) << (32 - r.PrefixLength)
		cursor := pool.start
		first := sort.Search(len(merged), func(i int) bool { return merged[i].end > cursor })
		for _, busy := range merged[first:] {
			if err := ctx.Err(); err != nil {
				return netip.Prefix{}, err
			}
			if busy.end <= cursor {
				continue
			}
			if busy.start >= pool.end || busy.start >= cursor+size {
				break
			}
			// Jump directly beyond the occupied interval to an aligned start.
			cursor = (busy.end + size - 1) & ^(size - 1)
			if cursor+size > pool.end {
				break
			}
		}
		if cursor+size <= pool.end {
			if err := ctx.Err(); err != nil {
				return netip.Prefix{}, err
			}
			var a [4]byte
			binary.BigEndian.PutUint32(a[:], uint32(cursor))
			return netip.PrefixFrom(netip.AddrFrom4(a), r.PrefixLength), nil
		}
	}
	if err := ctx.Err(); err != nil {
		return netip.Prefix{}, err
	}
	return netip.Prefix{}, domain.Fail("CIDR_CONFLICT", "no non-overlapping private subnet available in the configured ranges")
}
