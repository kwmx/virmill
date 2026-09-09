package domain

import "context"

// CreationOptions contains observed choices, not approval to allocate resources.
// The creation planner and executor recheck capabilities and available storage.
type CreationOptions struct {
	Architecture  string                   `json:"architecture"`
	Machines      []string                 `json:"machines"`
	Machine       string                   `json:"machine"`
	MaxVCPUs      uint                     `json:"maxVCPUs"`
	HostMemoryMiB uint64                   `json:"hostMemoryMiB"`
	CPUModes      []string                 `json:"cpuModes"`
	CPUModels     []string                 `json:"cpuModels"`
	Firmware      []CreationFirmwareOption `json:"firmware"`
	DiskBuses     []string                 `json:"diskBuses"`
	Graphics      []string                 `json:"graphics"`
}

type CreationFirmwareOption struct {
	Label    string           `json:"label"`
	Firmware CreationFirmware `json:"firmware"`
}

type CreationOptionsBackend interface {
	CreationOptions(context.Context, string, string) (CreationOptions, error)
}
