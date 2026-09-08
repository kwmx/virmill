package network

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func allocationConfig(cidr string, bits int) AllocationConfig {
	return AllocationConfig{Version: 1, Ranges: []AllocationRange{{CIDR: cidr, PrefixLength: bits}}}
}

func allocationPrefixes(raw ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(raw))
	for i, s := range raw {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

func allocationErrorCode(t *testing.T, err error, want string) {
	t.Helper()
	var d *domain.Error
	if !errors.As(err, &d) || d.Code != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

func TestDefaultAllocationConfig(t *testing.T) {
	want := AllocationConfig{Version: 1, Ranges: []AllocationRange{
		{CIDR: "10.0.0.0/8", PrefixLength: 24},
		{CIDR: "172.16.0.0/12", PrefixLength: 24},
		{CIDR: "192.168.0.0/16", PrefixLength: 24},
	}}
	got := DefaultAllocationConfig()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default = %#v", got)
	}
	if err := ValidateAllocationConfig(got); err != nil {
		t.Fatal(err)
	}
	got.Ranges[0].CIDR = "invalid"
	if !reflect.DeepEqual(DefaultAllocationConfig(), want) {
		t.Fatal("default configuration shares mutable range storage")
	}
}

func TestValidateAllocationConfigRejectsCorruption(t *testing.T) {
	tests := []struct {
		name string
		edit func(*AllocationConfig)
	}{
		{"zero version", func(c *AllocationConfig) { c.Version = 0 }},
		{"future version", func(c *AllocationConfig) { c.Version = 2 }},
		{"nil ranges", func(c *AllocationConfig) { c.Ranges = nil }},
		{"empty ranges", func(c *AllocationConfig) { c.Ranges = []AllocationRange{} }},
		{"too many ranges", func(c *AllocationConfig) { c.Ranges = make([]AllocationRange, 17) }},
		{"duplicate ranges", func(c *AllocationConfig) { c.Ranges = append(c.Ranges, c.Ranges[0]) }},
		{"contained range", func(c *AllocationConfig) { c.Ranges = append(c.Ranges, AllocationRange{"10.0.0.0/17", 24}) }},
		{"wider range", func(c *AllocationConfig) { c.Ranges = append(c.Ranges, AllocationRange{"10.0.0.0/8", 24}) }},
		{"missing CIDR", func(c *AllocationConfig) { c.Ranges[0].CIDR = "" }},
		{"malformed CIDR", func(c *AllocationConfig) { c.Ranges[0].CIDR = "10.0.0.0/not-bits" }},
		{"host bits", func(c *AllocationConfig) { c.Ranges[0].CIDR = "10.0.0.1/24" }},
		{"prefix leading zero", func(c *AllocationConfig) { c.Ranges[0].CIDR = "10.0.0.0/08" }},
		{"address leading zero", func(c *AllocationConfig) { c.Ranges[0].CIDR = "010.0.0.0/8" }},
		{"trailing whitespace", func(c *AllocationConfig) { c.Ranges[0].CIDR = "10.0.0.0/8 " }},
		{"public", func(c *AllocationConfig) { c.Ranges[0].CIDR = "8.0.0.0/8" }},
		{"shared address space", func(c *AllocationConfig) { c.Ranges[0].CIDR = "100.64.0.0/10" }},
		{"loopback", func(c *AllocationConfig) { c.Ranges[0].CIDR = "127.0.0.0/8" }},
		{"link local", func(c *AllocationConfig) { c.Ranges[0].CIDR = "169.254.0.0/16" }},
		{"multicast", func(c *AllocationConfig) { c.Ranges[0].CIDR = "224.0.0.0/8" }},
		{"all IPv4", func(c *AllocationConfig) { c.Ranges[0].CIDR = "0.0.0.0/0" }},
		{"partly private 10", func(c *AllocationConfig) { c.Ranges[0].CIDR = "10.0.0.0/7" }},
		{"partly private 172", func(c *AllocationConfig) { c.Ranges[0].CIDR = "172.0.0.0/11" }},
		{"partly private 192", func(c *AllocationConfig) { c.Ranges[0].CIDR = "192.168.0.0/15" }},
		{"IPv6 ULA", func(c *AllocationConfig) { c.Ranges[0].CIDR = "fd00::/8" }},
		{"IPv4 mapped", func(c *AllocationConfig) { c.Ranges[0].CIDR = "::ffff:10.0.0.0/104" }},
		{"overlong CIDR", func(c *AllocationConfig) { c.Ranges[0].CIDR = strings.Repeat("x", 4096) }},
		{"output prefix zero", func(c *AllocationConfig) { c.Ranges[0].PrefixLength = 0 }},
		{"output prefix negative", func(c *AllocationConfig) { c.Ranges[0].PrefixLength = -1 }},
		{"output prefix seven", func(c *AllocationConfig) { c.Ranges[0].PrefixLength = 7 }},
		{"output larger than pool", func(c *AllocationConfig) { c.Ranges[0].PrefixLength = 15 }},
		{"output prefix 31", func(c *AllocationConfig) { c.Ranges[0].PrefixLength = 31 }},
		{"output prefix 33", func(c *AllocationConfig) { c.Ranges[0].PrefixLength = 33 }},
		{"pool too narrow", func(c *AllocationConfig) { c.Ranges[0].CIDR = "10.0.0.0/31"; c.Ranges[0].PrefixLength = 30 }},
		{"too many planned", func(c *AllocationConfig) { c.Planned = make([]PlannedAllocation, 257) }},
		{"planned duplicate ID", func(c *AllocationConfig) {
			c.Planned = []PlannedAllocation{{"lab", "10.0.0.0/24"}, {"lab", "10.0.1.0/24"}}
		}},
		{"planned missing ID", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"", "10.0.0.0/24"}} }},
		{"planned nonalphanumeric first", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"_lab", "10.0.0.0/24"}} }},
		{"planned slash ID", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"lab/id", "10.0.0.0/24"}} }},
		{"planned Unicode ID", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"labé", "10.0.0.0/24"}} }},
		{"planned whitespace ID", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"lab ", "10.0.0.0/24"}} }},
		{"planned newline ID", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"lab\n", "10.0.0.0/24"}} }},
		{"planned long ID", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{strings.Repeat("a", 129), "10.0.0.0/24"}} }},
		{"planned host bits", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"lab", "10.0.0.1/24"}} }},
		{"planned public", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"lab", "8.8.8.0/24"}} }},
		{"planned partly private", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"lab", "172.0.0.0/11"}} }},
		{"planned default", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"lab", "0.0.0.0/0"}} }},
		{"planned IPv6", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"lab", "fd00::/64"}} }},
		{"planned mapped", func(c *AllocationConfig) { c.Planned = []PlannedAllocation{{"lab", "::ffff:10.0.0.0/120"}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := allocationConfig("10.0.0.0/16", 24)
			tt.edit(&c)
			allocationErrorCode(t, ValidateAllocationConfig(c), "INVALID_INPUT")
			got, err := AllocateConfigured(context.Background(), c, nil)
			allocationErrorCode(t, err, "INVALID_INPUT")
			if got.IsValid() {
				t.Fatalf("invalid config returned subnet %s", got)
			}
		})
	}
}

func TestValidateAllocationConfigBoundaries(t *testing.T) {
	c := AllocationConfig{Version: 1}
	for i := 0; i < 16; i++ {
		c.Ranges = append(c.Ranges, AllocationRange{fmt.Sprintf("10.%d.0.0/16", i), 30})
	}
	for i := 0; i < 256; i++ {
		c.Planned = append(c.Planned, PlannedAllocation{fmt.Sprintf("id:%d", i), "192.168.255.255/32"})
	}
	c.Planned[0].ID = "A0_.:-" + strings.Repeat("z", 122)
	c.Planned[1].CIDR = "172.31.255.254/31"
	if err := ValidateAllocationConfig(c); err != nil {
		t.Fatalf("valid maximum counts, IDs, overlapping and outside-pool reservations: %v", err)
	}
	for _, raw := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "10.255.255.252/30", "172.31.255.252/30", "192.168.255.252/30"} {
		p := netip.MustParsePrefix(raw)
		if err := ValidateAllocationConfig(allocationConfig(raw, p.Bits())); err != nil {
			t.Fatalf("whole private boundary %s: %v", raw, err)
		}
	}
}

func TestAllocateConfiguredSelectionAndConflicts(t *testing.T) {
	tests := []struct {
		name     string
		config   AllocationConfig
		occupied []netip.Prefix
		want     string
	}{
		{"default first", DefaultAllocationConfig(), nil, "10.0.0.0/24"},
		{"default second range", DefaultAllocationConfig(), allocationPrefixes("10.0.0.0/8"), "172.16.0.0/24"},
		{"default third range", DefaultAllocationConfig(), allocationPrefixes("172.16.0.0/12", "10.0.0.0/8"), "192.168.0.0/24"},
		{"ordered not numeric", AllocationConfig{Version: 1, Ranges: []AllocationRange{{"192.168.100.0/24", 26}, {"10.0.0.0/24", 28}}}, nil, "192.168.100.0/26"},
		{"different output prefixes", AllocationConfig{Version: 1, Ranges: []AllocationRange{{"192.168.100.0/24", 26}, {"10.0.0.0/24", 28}}}, allocationPrefixes("192.168.100.0/24", "10.0.0.0/32"), "10.0.0.16/28"},
		{"LAN and VPN all tables", DefaultAllocationConfig(), allocationPrefixes("10.0.0.0/24", "10.0.1.0/25", "10.0.2.192/26"), "10.0.3.0/24"},
		{"unaligned observed address", DefaultAllocationConfig(), allocationPrefixes("10.0.1.9/23"), "10.0.2.0/24"},
		{"single address blocks subnet", DefaultAllocationConfig(), allocationPrefixes("10.0.0.255/32"), "10.0.1.0/24"},
		{"last address of previous subnet disjoint", allocationConfig("10.0.1.0/24", 24), allocationPrefixes("10.0.0.255/32", "10.0.2.0/24"), "10.0.1.0/24"},
		{"prefix crossing range boundary", allocationConfig("10.0.1.0/24", 26), allocationPrefixes("10.0.0.0/23"), ""},
		{"multiple addresses within blocked subnets", allocationConfig("10.0.0.0/24", 26), allocationPrefixes("10.0.0.0/32", "10.0.0.2/32", "10.0.0.32/32", "10.0.0.65/32", "10.0.0.66/32"), "10.0.0.128/26"},
		{"hole cannot fit aligned subnet", allocationConfig("10.0.0.0/24", 26), allocationPrefixes("10.0.0.0/32", "10.0.0.127/32"), "10.0.0.128/26"},
		{"IPv6 default is disjoint", DefaultAllocationConfig(), allocationPrefixes("::/0", "fd00::/8", "2001:db8::/32"), "10.0.0.0/24"},
		{"public IPv4 disjoint", DefaultAllocationConfig(), allocationPrefixes("0.0.0.0/8", "8.8.8.0/24", "255.255.255.255/32"), "10.0.0.0/24"},
		{"whole pool allocation", allocationConfig("10.0.0.0/8", 8), nil, "10.0.0.0/8"},
		{"whole pool blocked by one host", allocationConfig("10.0.0.0/8", 8), allocationPrefixes("10.255.255.255/32"), ""},
		{"IPv4 default is occupied", DefaultAllocationConfig(), allocationPrefixes("0.0.0.0/0"), ""},
		{"nonmasked IPv4 default is occupied", DefaultAllocationConfig(), allocationPrefixes("203.0.113.1/0"), ""},
		{"native superset not restricted to RFC1918", DefaultAllocationConfig(), allocationPrefixes("0.0.0.0/1", "128.0.0.0/1"), ""},
		{"all pools exhausted", DefaultAllocationConfig(), allocationPrefixes("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"), ""},
		{"last 10 subnet", allocationConfig("10.255.255.248/29", 30), allocationPrefixes("10.255.255.248/30"), "10.255.255.252/30"},
		{"last 172 subnet", allocationConfig("172.31.255.248/29", 30), allocationPrefixes("172.31.255.248/30"), "172.31.255.252/30"},
		{"last 192 subnet", allocationConfig("192.168.255.248/29", 30), allocationPrefixes("192.168.255.248/30"), "192.168.255.252/30"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AllocateConfigured(context.Background(), tt.config, tt.occupied)
			if tt.want == "" {
				allocationErrorCode(t, err, "CIDR_CONFLICT")
				if got.IsValid() {
					t.Fatalf("exhaustion returned %s", got)
				}
			} else if err != nil || got.String() != tt.want {
				t.Fatalf("got %s, %v; want %s", got, err, tt.want)
			}
		})
	}
}

func TestAllocateConfiguredPlannedAndNative(t *testing.T) {
	c := allocationConfig("10.10.0.0/16", 24)
	c.Planned = []PlannedAllocation{
		{ID: "future.lab", CIDR: "10.10.0.128/25"},
		{ID: "future:point", CIDR: "10.10.1.255/32"},
		{ID: "future_link", CIDR: "10.10.2.2/31"},
		{ID: "overlapping-allowed", CIDR: "10.10.2.0/24"},
		{ID: "outside-pool", CIDR: "172.31.255.255/32"},
	}
	occupied := allocationPrefixes("10.10.3.0/24", "10.10.0.0/24", "10.10.3.0/24", "fd00::/64")
	before := append([]netip.Prefix(nil), occupied...)
	beforeRanges := append([]AllocationRange(nil), c.Ranges...)
	beforePlanned := append([]PlannedAllocation(nil), c.Planned...)
	for i := 0; i < 20; i++ {
		got, err := AllocateConfigured(context.Background(), c, occupied)
		if err != nil || got.String() != "10.10.4.0/24" {
			t.Fatalf("run %d: %s %v", i, got, err)
		}
	}
	if !reflect.DeepEqual(occupied, before) || !reflect.DeepEqual(c.Ranges, beforeRanges) || !reflect.DeepEqual(c.Planned, beforePlanned) {
		t.Fatal("allocator mutated caller-owned input")
	}
	c.Planned = append(c.Planned, PlannedAllocation{"all-future", "10.10.0.0/16"})
	got, err := AllocateConfigured(context.Background(), c, occupied)
	allocationErrorCode(t, err, "CIDR_CONFLICT")
	if got.IsValid() {
		t.Fatal("returned success despite planned exhaustion")
	}
}

func TestAllocateConfiguredInvalidOccupied(t *testing.T) {
	tests := []struct {
		name string
		p    netip.Prefix
	}{
		{"zero", netip.Prefix{}},
		{"invalid IPv4 length", netip.PrefixFrom(netip.MustParseAddr("10.0.0.0"), 33)},
		{"invalid IPv6 length", netip.PrefixFrom(netip.MustParseAddr("fd00::"), 129)},
		{"mapped IPv4", netip.MustParsePrefix("::ffff:10.0.0.0/120")},
		{"mapped default", netip.MustParsePrefix("::ffff:0.0.0.0/0")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Even a later invalid prefix must refuse an otherwise immediately
			// available first subnet, rather than return partial validation.
			occupied := append(allocationPrefixes("8.8.8.0/24"), tt.p)
			got, err := AllocateConfigured(context.Background(), DefaultAllocationConfig(), occupied)
			allocationErrorCode(t, err, "INVALID_INPUT")
			if got.IsValid() {
				t.Fatal("invalid occupied input returned a subnet")
			}
		})
	}
}

func TestAllocateConfiguredOccupiedBound(t *testing.T) {
	occupied := make([]netip.Prefix, 65536)
	for i := range occupied {
		occupied[i] = netip.MustParsePrefix("10.0.0.0/24")
	}
	c := DefaultAllocationConfig()
	for i := 0; i < 256; i++ {
		c.Planned = append(c.Planned, PlannedAllocation{fmt.Sprintf("planned-%d", i), "10.0.1.0/24"})
	}
	got, err := AllocateConfigured(context.Background(), c, occupied)
	if err != nil || got.String() != "10.0.2.0/24" {
		t.Fatalf("maximum native plus planned limits: %s %v", got, err)
	}
	got, err = AllocateConfigured(context.Background(), c, append(occupied, netip.MustParsePrefix("fd00::/64")))
	allocationErrorCode(t, err, "INVALID_INPUT")
	if got.IsValid() {
		t.Fatal("over-limit input returned a subnet")
	}
}

// allocationCancelContext deterministically cancels at a boundary without
// scheduler timing. Once canceled it satisfies the Context Err contract.
type allocationCancelContext struct {
	context.Context
	after, calls int
}

func (c *allocationCancelContext) Err() error {
	c.calls++
	if c.after > 0 && c.calls >= c.after {
		return context.Canceled
	}
	return nil
}

func TestAllocateConfiguredCancellation(t *testing.T) {
	c := allocationConfig("10.0.0.0/16", 24)
	c.Planned = []PlannedAllocation{{"planned", "10.0.3.0/24"}}
	occupied := allocationPrefixes("10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24", "10.0.5.0/24")
	for _, exhausted := range []bool{false, true} {
		t.Run(fmt.Sprintf("exhausted=%t", exhausted), func(t *testing.T) {
			snapshot := append([]netip.Prefix(nil), occupied...)
			if exhausted {
				snapshot = append(snapshot, netip.MustParsePrefix("0.0.0.0/0"))
			}
			probe := &allocationCancelContext{Context: context.Background()}
			_, _ = AllocateConfigured(probe, c, snapshot)
			for boundary := 1; boundary <= probe.calls; boundary++ {
				ctx := &allocationCancelContext{Context: context.Background(), after: boundary}
				got, err := AllocateConfigured(ctx, c, snapshot)
				if !errors.Is(err, context.Canceled) || got.IsValid() {
					t.Fatalf("boundary %d/%d returned %s %v", boundary, probe.calls, got, err)
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := AllocateConfigured(ctx, c, occupied)
	if !errors.Is(err, context.Canceled) || got.IsValid() {
		t.Fatalf("canceled real context: %s %v", got, err)
	}
}

func TestAllocateConfiguredSkipsLargeOccupiedIntervals(t *testing.T) {
	// 22 prefixes occupy every /30 in 10/8 except the final one. Searching
	// candidates individually would visit more than four million subnets.
	occupied := []netip.Prefix{}
	base := uint32(10) << 24
	for bits := 9; bits <= 30; bits++ {
		var a [4]byte
		binary.BigEndian.PutUint32(a[:], base)
		occupied = append(occupied, netip.PrefixFrom(netip.AddrFrom4(a), bits))
		base += uint32(1) << (32 - bits)
	}
	ctx := &allocationCancelContext{Context: context.Background(), after: 200}
	got, err := AllocateConfigured(ctx, allocationConfig("10.0.0.0/8", 30), occupied)
	if err != nil || got.String() != "10.255.255.252/30" {
		t.Fatalf("bounded interval jump: %s %v (%d boundaries)", got, err, ctx.calls)
	}
}

func TestAllocateConfiguredMatchesSmallExhaustiveOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(6006))
	for trial := 0; trial < 200; trial++ {
		c := AllocationConfig{Version: 1, Ranges: []AllocationRange{{"192.168.50.0/24", 30}, {"10.1.2.0/24", 28}}}
		occupied := allocationPrefixes("::/0", "203.0.113.0/24")
		count := rng.Intn(100) + 1
		for i := 0; i < count; i++ {
			pool := netip.MustParsePrefix(c.Ranges[rng.Intn(len(c.Ranges))].CIDR)
			a := pool.Addr().As4()
			a[3] = byte(rng.Intn(256))
			p := netip.PrefixFrom(netip.AddrFrom4(a), 26+rng.Intn(7))
			if i%3 == 0 {
				c.Planned = append(c.Planned, PlannedAllocation{fmt.Sprintf("plan-%d", i), p.Masked().String()})
			} else {
				occupied = append(occupied, p) // host bits deliberately retained
			}
		}
		all := append([]netip.Prefix(nil), occupied...)
		for _, planned := range c.Planned {
			all = append(all, netip.MustParsePrefix(planned.CIDR))
		}
		var want netip.Prefix
		for _, r := range c.Ranges {
			pool := netip.MustParsePrefix(r.CIDR)
			address := pool.Addr().As4()
			start := binary.BigEndian.Uint32(address[:])
			for offset := uint32(0); offset < 256; offset += 1 << (32 - r.PrefixLength) {
				var a [4]byte
				binary.BigEndian.PutUint32(a[:], start+offset)
				candidate := netip.PrefixFrom(netip.AddrFrom4(a), r.PrefixLength)
				free := true
				for _, busy := range all {
					if candidate.Overlaps(busy) {
						free = false
						break
					}
				}
				if free {
					want = candidate
					break
				}
			}
			if want.IsValid() {
				break
			}
		}
		for order := 0; order < 3; order++ {
			rng.Shuffle(len(occupied), func(i, j int) { occupied[i], occupied[j] = occupied[j], occupied[i] })
			got, err := AllocateConfigured(context.Background(), c, occupied)
			if got != want {
				t.Fatalf("trial %d permutation %d got %s %v, oracle %s", trial, order, got, err, want)
			}
			if want.IsValid() && err != nil {
				t.Fatalf("trial %d: unexpected error %v", trial, err)
			}
			if !want.IsValid() {
				allocationErrorCode(t, err, "CIDR_CONFLICT")
			}
		}
	}
}
