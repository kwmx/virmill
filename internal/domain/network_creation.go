package domain

import "context"

// NetworkDefinition describes a newly owned network. Existing XML is never
// reconstructed through this type. UUID and bridge identity are reserved by a
// durable plan before definition; activation is a separate journaled step.
type NetworkDefinition struct {
	UUID                  string `json:"uuid"`
	Name                  string `json:"name"`
	Bridge                string `json:"bridge"`
	Type                  string `json:"type"`
	IPv4CIDR              string `json:"ipv4CIDR"`
	DHCPEnabled           bool   `json:"dhcpEnabled"`
	AdvertiseDefaultRoute bool   `json:"advertiseDefaultRoute"`
	IPv6Mode              string `json:"ipv6Mode"`
	HostAccess            string `json:"hostAccess"`
	Egress                string `json:"egress"`
}

// PolicyVersion separates legacy allowed-host definitions and helper grants
// from protected profiles. Zero means no supported policy family; callers must
// still validate addresses, DHCP, forwarding and native state independently.
func (d NetworkDefinition) PolicyVersion() int {
	if d.Type == "nat" || d.Type == "lab" {
		if d.HostAccess == "allow" {
			return 1
		}
		if d.HostAccess == "services-only" {
			return 2
		}
	}
	if d.Type == "guest-only" && d.HostAccess == "deny" {
		return 2
	}
	return 0
}

// NetworkCreationProvider cannot replace or remove an existing definition.
// Methods must repeat collision/configuration checks immediately before a
// native mutation. InspectCreatedNetwork is read-only and verifies the exact
// requested definition; missing or different state is an error.
type NetworkCreationProvider interface {
	CheckNetworkCreation(context.Context, string, NetworkDefinition) error
	DefineNetwork(context.Context, string, NetworkDefinition) error
	ActivateNetwork(context.Context, string, NetworkDefinition) error
	InspectCreatedNetwork(context.Context, string, NetworkDefinition) (VirtualNetwork, error)
}
