package domain

// FirewallZones is what firewalld reports for network bridges: its default
// zone and, per bridge, the zone that holds it ("" when none does).
type FirewallZones struct {
	Default string
	Bridges map[string]string
}
