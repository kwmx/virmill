package domain

import (
	"context"
	"io"
)

type CreationCPU struct {
	Mode  string `json:"mode"`
	Model string `json:"model,omitempty"`
}
type CreationFirmware struct {
	Mode       string `json:"mode"`
	Code       string `json:"code,omitempty"`
	Template   string `json:"template,omitempty"`
	Format     string `json:"format,omitempty"`
	SecureBoot bool   `json:"secureBoot"`
	TPM        bool   `json:"tpm"`
}
type CreationDisk struct {
	SourceID  string `json:"sourceID"`
	Bus       string `json:"bus"`
	BootOrder int    `json:"bootOrder"`
}

// CreationMedia is an independent read-only ISO volume. BootOrder 0 means the
// media is attached without becoming a boot candidate (for example a NoCloud seed).
type CreationMedia struct {
	SourceID  string `json:"sourceID"`
	Bus       string `json:"bus"`
	BootOrder int    `json:"bootOrder"`
}
type CreationNIC struct {
	ID          string `json:"id"`
	SourceIndex int    `json:"sourceIndex"`
	NetworkID   string `json:"networkID"`
	Model       string `json:"model"`
	Link        string `json:"link"`
	MAC         string `json:"mac,omitempty"`
}
type CreationSpec struct {
	UUID         string                `json:"uuid"`
	Name         string                `json:"name"`
	PoolID       string                `json:"poolID"`
	Architecture string                `json:"architecture"`
	Machine      string                `json:"machine"`
	VCPUs        uint                  `json:"vcpus"`
	MemoryMiB    uint64                `json:"memoryMiB"`
	CPU          CreationCPU           `json:"cpu"`
	Firmware     CreationFirmware      `json:"firmware"`
	Clock        string                `json:"clock"`
	GuestAgent   bool                  `json:"guestAgent,omitempty"`
	Graphics     string                `json:"graphics"`
	Disks        []CreationDisk        `json:"disks"`
	Media        []CreationMedia       `json:"media,omitempty"`
	NICs         []CreationNIC         `json:"nics"`
	DevicePolicy *CreationDevicePolicy `json:"devicePolicy,omitempty"`
}

// CreationDevicePolicy is versioned reviewed intent for chipset devices that
// libvirt otherwise adds implicitly. A nil policy retains legacy plan semantics.
type CreationDevicePolicy struct {
	Version        int    `json:"version"`
	Chipset        string `json:"chipset"`
	PCIPlacement   string `json:"pciPlacement"`
	USBController  string `json:"usbController"`
	MemoryBalloon  string `json:"memoryBalloon"`
	WatchdogAction string `json:"watchdogAction"`
	Input          string `json:"input"`
	Audio          string `json:"audio"`
	Serial         string `json:"serial"`
}
type CreationTarget struct {
	Spec               CreationSpec     `json:"spec"`
	PoolName           string           `json:"poolName"`
	PoolGeneration     string           `json:"poolGeneration,omitempty"`
	PoolFingerprint    string           `json:"poolFingerprint"`
	Networks           []VirtualNetwork `json:"networks"`
	Emulator           string           `json:"emulator"`
	EmulatorDigest     string           `json:"emulatorDigest"`
	CapabilitiesDigest string           `json:"capabilitiesDigest"`
	FirmwareDigest     string           `json:"firmwareDigest"`
}
type VolumeIntent struct {
	ContentType  string `json:"contentType,omitempty"` // empty: legacy qcow2 disk; cdrom-iso: raw read-only media
	PoolID       string `json:"poolID"`
	Name         string `json:"name"`
	SourceID     string `json:"sourceID"`
	VirtualBytes uint64 `json:"virtualBytes"`
	FileBytes    uint64 `json:"fileBytes"`
	SHA256       string `json:"sha256"`
}
type CreatedVolume struct {
	Intent     VolumeIntent `json:"intent"`
	BackendKey string       `json:"backendKey"`
	Path       string       `json:"path"`
	Generation string       `json:"generation,omitempty"`
}

// CreationBackend accepts typed reviewed intent. No method executes guest media
// or accepts a caller-supplied command line. Volume writes create new names only.
type CreationBackend interface {
	PreflightCreation(context.Context, string, CreationSpec) (CreationTarget, error)
	CheckCreationIdentity(context.Context, string, string, string) error
	VolumeAbsent(context.Context, string, VolumeIntent) error
	AllocateVolume(context.Context, string, VolumeIntent) (CreatedVolume, error)
	PopulateVolume(context.Context, string, CreatedVolume, io.Reader) error
	VerifyCreatedVolume(context.Context, string, CreatedVolume) error
	DefineCreatedVM(context.Context, string, CreationTarget, []CreatedVolume, string) (VM, error)
	ObserveCreatedVM(context.Context, string, CreationTarget, []CreatedVolume, string) (VM, bool, error)
}
