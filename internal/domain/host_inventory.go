package domain

import "context"

// PCIDevice is read-only discovery. Group membership is not a passthrough safety
// claim and never authorizes driver detachment or host boot configuration edits.
type PCIDevice struct {
	Name         string   `json:"name"`
	Address      string   `json:"address"`
	VendorID     string   `json:"vendorID"`
	ProductID    string   `json:"productID"`
	Vendor       string   `json:"vendor"`
	Product      string   `json:"product"`
	Driver       string   `json:"driver"`
	IOMMUGroup   *uint32  `json:"iommuGroup"`
	GroupMembers []string `json:"groupMembers"`
	NUMANode     *int32   `json:"numaNode"`
}
type PCIInventory struct {
	Devices  []PCIDevice `json:"devices"`
	Warnings []string    `json:"warnings"`
}
type PCIInventoryProvider interface {
	InspectPCI(context.Context, string) (PCIInventory, error)
}

// HostNetworkPrefix distinguishes interface address networks from routing-table
// destinations. Only route /0 entries may be excluded as universal defaults;
// an assigned address or planned allocation must never use that exclusion.
type HostNetworkPrefix struct {
	CIDR           string `json:"cidr"`
	Source         string `json:"source"` // address or route
	InterfaceIndex uint32 `json:"interfaceIndex"`
	Table          uint32 `json:"table"` // zero for interface addresses
}
type HostNetworkPrefixes struct {
	Prefixes []HostNetworkPrefix `json:"prefixes"`
	Warnings []string            `json:"warnings"`
}
