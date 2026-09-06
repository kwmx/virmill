package network

import (
	"net/netip"
	"testing"
)

func TestRouteOverlapAndDefaultRoute(t *testing.T) {
	p := netip.MustParsePrefix
	got, e := Allocate([]netip.Prefix{p("10.1.0.0/24"), p("10.2.0.0/24")}, []netip.Prefix{p("0.0.0.0/0"), p("10.1.0.0/16")}, nil)
	if e != nil || got != p("10.2.0.0/24") {
		t.Fatal(got, e)
	}
	if _, e = Allocate([]netip.Prefix{p("10.2.0.0/24")}, nil, []netip.Prefix{p("0.0.0.0/0")}); e == nil {
		t.Fatal("planned /0 incorrectly ignored")
	}
	a := NIC{ID: "a", DefaultRoute: true}
	a.IPv4.Mode = "dhcp"
	b := a
	b.ID = "b"
	if _, e := ValidateNICs([]NIC{a, b}, nil); e == nil {
		t.Fatal("duplicate defaults")
	}
}
func TestLabDHCPAndGuestOnly(t *testing.T) {
	s := Spec{Type: "guest-only", HostAccess: "deny", Egress: "none", IPv4: &IPv4{CIDR: "10.0.0.0/24"}}
	s.IPv4.DHCP.Enabled = true
	if Validate(s) == nil {
		t.Fatal("host DHCP in guest-only")
	}
	s.Type = "lab"
	s.IPv4.DHCP.AdvertiseDefaultRoute = true
	if Validate(s) == nil {
		t.Fatal("lab router advertisement accepted")
	}
}
