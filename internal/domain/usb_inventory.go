package domain

import "context"

// USBDevice contains observed discovery facts, not authority to attach a device.
// Bus and Device are ephemeral. StableID is only a descriptive match key;
// serial uniqueness and physical-port continuity need separate qualification.
type USBDevice struct {
	Name         string `json:"name"`
	VendorID     string `json:"vendorID"`
	ProductID    string `json:"productID"`
	Vendor       string `json:"vendor"`
	Product      string `json:"product"`
	Serial       string `json:"serial"`
	PhysicalPort string `json:"physicalPort"`
	Bus          *uint  `json:"bus"`
	Device       *uint  `json:"device"`
	StableID     string `json:"stableID"`
	Ambiguous    bool   `json:"ambiguous"`
	Reason       string `json:"reason"`
}

type USBInventoryProvider interface {
	InspectUSB(context.Context, string) ([]USBDevice, error)
}
