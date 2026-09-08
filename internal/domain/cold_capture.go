package domain

import "context"

// ColdCaptureBackend observes native identity and dependency versions. It does
// not grant file access or claim that a stopped observation freezes the source.
type ColdCaptureBackend interface {
	ColdStateInspector
	Get(context.Context, string, string) (VM, error)
	ColdRuntimeVersions(context.Context, string, bool) (map[string]string, error)
	CheckColdConfiguration(context.Context, string, string, string) error
}

type ColdResolvedVolume struct {
	Source          ColdStorageSource `json:"source"`
	PoolID          string            `json:"poolID"`
	PoolFingerprint string            `json:"poolFingerprint"`
	Key             string            `json:"key"`
	Path            string            `json:"path"`
}
type ColdVolumeResolver interface {
	ResolveColdVolume(context.Context, string, ColdStorageSource) (ColdResolvedVolume, error)
}
