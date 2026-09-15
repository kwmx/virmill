package app

import (
	"context"
	"encoding/xml"
	"regexp"
	"strings"

	"virmill.local/core/internal/domain"
)

// firewallName is what a bridge, zone or network name must look like before
// the host check prints it, or a command using it, for you to run.
var firewallName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// libvirtBridge is the part of a network's XML that says whether libvirt runs
// its bridge, with DHCP and DNS, and which firewalld zone it asks for.
type libvirtBridge struct {
	Forward *struct {
		Mode string `xml:"mode,attr"`
	} `xml:"forward"`
	Bridge struct {
		Name string `xml:"name,attr"`
		Zone string `xml:"zone,attr"`
	} `xml:"bridge"`
}

// networkFirewallCheck reports active libvirt networks whose bridge firewalld
// holds in no zone. The default zone then applies, which usually drops DHCP
// and DNS, so VMs there get no address. libvirt puts each bridge in a zone
// when the network starts, so this happens when a network started before
// firewalld. It only reads; ok is false when there is nothing to report.
func (s *Service) networkFirewallCheck(ctx context.Context, uri string) (domain.Capability, bool) {
	inv, ok := s.Provider.(domain.ResourceInventory)
	if !ok || s.BridgeZones == nil || uri != "qemu:///system" {
		return domain.Capability{}, false
	}
	networks, err := inv.ListNetworks(ctx, uri)
	if err != nil || len(networks) > 4096 {
		return domain.Capability{}, false
	}
	type bridge struct{ network, name, zone string }
	var bridges []bridge
	for _, n := range networks {
		var x libvirtBridge
		if !n.Active || len(n.LiveXML) > 1<<20 || xml.Unmarshal([]byte(n.LiveXML), &x) != nil {
			continue
		}
		// Only NAT, routed and isolated networks have a bridge libvirt runs itself.
		if x.Forward != nil && x.Forward.Mode != "" && x.Forward.Mode != "nat" && x.Forward.Mode != "route" {
			continue
		}
		zone := x.Bridge.Zone
		if zone == "" {
			zone = "libvirt"
		}
		if !firewallName.MatchString(x.Bridge.Name) || len(x.Bridge.Name) > 15 || !firewallName.MatchString(zone) {
			continue
		}
		name := n.Name
		if !firewallName.MatchString(name) {
			name = ""
		}
		bridges = append(bridges, bridge{name, x.Bridge.Name, zone})
	}
	if len(bridges) == 0 {
		return domain.Capability{}, false
	}
	names := make([]string, len(bridges))
	for i, b := range bridges {
		names[i] = b.name
	}
	zones, ok := s.BridgeZones(ctx, names)
	if !ok || !firewallName.MatchString(zones.Default) {
		return domain.Capability{}, false
	}
	c := domain.Capability{ID: "network-firewall", Purpose: "Lets VMs on libvirt networks get an address and DNS from this host",
		Status: "supported-with-prerequisites", Alternatives: []string{}, EvidenceClass: "read-only-probe"}
	var blocked, fine []string
	for _, b := range bridges {
		zone, known := zones.Bridges[b.name]
		label := b.name
		if b.network != "" {
			label += " (network " + b.network + ")"
		}
		switch {
		case !known || zone != "" && !firewallName.MatchString(zone):
		case zone == "" && zones.Default != b.zone:
			blocked = append(blocked, label)
			c.Alternatives = append(c.Alternatives, "sudo firewall-cmd --zone="+b.zone+" --change-interface="+b.name)
		case zone == "":
			fine = append(fine, label+" uses the default zone "+zones.Default)
		default:
			fine = append(fine, label+" is in zone "+zone)
		}
	}
	switch {
	case len(blocked) > 0:
		c.ReasonCode = "NETWORK_BRIDGE_ZONE_MISSING"
		c.Reason = "firewalld holds " + strings.Join(blocked, ", ") + " in no zone, so its default zone " + zones.Default +
			" can block DHCP and DNS and VMs there get no address; this happens when a network starts before firewalld"
	case len(fine) > 0:
		c.ReasonCode, c.Reason = "NETWORK_BRIDGE_ZONE_READY", "firewalld: "+strings.Join(fine, ", ")
	default:
		return domain.Capability{}, false
	}
	return c, true
}
