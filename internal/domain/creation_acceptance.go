package domain

import "context"

// CreationAcceptanceObservation binds a reviewed device-policy correction to
// exact existing configuration and file metadata, without persisting opaque XML.
type CreationAcceptanceObservation struct {
	VMFingerprint       string   `json:"vmFingerprint"`
	PersistentXMLSHA256 string   `json:"persistentXMLSHA256"`
	TargetFingerprint   string   `json:"targetFingerprint"`
	VolumeFingerprints  []string `json:"volumeFingerprints"`
}

// CreationAcceptanceBackend observes and reads only. It never modifies a domain
// or volume; accepted ownership is a separate durable application fact.
type CreationAcceptanceBackend interface {
	InspectCreationAcceptance(context.Context, string, CreationTarget, *CreationDevicePolicy, []CreatedVolume, string) (CreationAcceptanceObservation, error)
	VerifyCreationAcceptance(context.Context, string, CreationTarget, *CreationDevicePolicy, []CreatedVolume, string, CreationAcceptanceObservation) error
}
