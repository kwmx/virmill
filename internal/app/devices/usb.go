package devices

import (
	"errors"
	"fmt"
)

type USB struct {
	ID              string `json:"id"`
	Vendor          string `json:"vendor"`
	Product         string `json:"product"`
	Serial          string `json:"serial"`
	Port            string `json:"port"`
	Bus             int    `json:"bus"`
	Device          int    `json:"device"`
	Mounted         bool   `json:"mounted"`
	CriticalHostUse bool   `json:"criticalHostUse"`
	Owner           string `json:"owner"`
	HostUseKnown    bool   `json:"hostUseKnown"`
}
type Selector struct {
	Vendor  string `json:"vendor"`
	Product string `json:"product"`
	Serial  string `json:"serial,omitempty"`
	Port    string `json:"port,omitempty"`
	Missing string `json:"missing"`
}

func Resolve(s Selector, inventory []USB, vmID string) (*USB, []string, error) {
	if s.Vendor == "" || s.Product == "" || (s.Serial == "" && s.Port == "") {
		return nil, nil, errors.New("persistent USB binding requires vendor/product and serial or approved physical port")
	}
	if s.Missing != "required" && s.Missing != "optional" {
		return nil, nil, errors.New("missing-device policy must be explicit")
	}
	matches := []USB{}
	for _, d := range inventory {
		if d.Vendor == s.Vendor && d.Product == s.Product && (s.Serial == "" || d.Serial == s.Serial) && (s.Port == "" || d.Port == s.Port) {
			matches = append(matches, d)
		}
	}
	if len(matches) == 0 {
		if s.Missing == "optional" {
			return nil, []string{"OPTIONAL_USB_MISSING"}, nil
		}
		return nil, nil, errors.New("required USB device missing")
	}
	if len(matches) != 1 {
		return nil, nil, errors.New("ambiguous USB identity; approve a physical port")
	}
	d := matches[0]
	if !d.HostUseKnown {
		return nil, nil, errors.New("host use could not be established; attachment refused")
	}
	if d.Mounted || d.CriticalHostUse {
		return nil, nil, errors.New("USB supplies mounted storage or critical host function")
	}
	if d.Owner != "" && d.Owner != vmID {
		return nil, nil, fmt.Errorf("USB is already owned by %s", d.Owner)
	}
	return &d, []string{}, nil
}
