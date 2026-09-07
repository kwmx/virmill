package domain

import "context"

type ColdStateInspection struct {
	Resource       ResourceKey     `json:"resource"`
	State          string          `json:"state"`
	HasManagedSave bool            `json:"hasManagedSave"`
	Autostart      bool            `json:"autostart"`
	Fingerprint    string          `json:"fingerprint"`
	Layout         ColdStateLayout `json:"layout"`
	Warnings       []string        `json:"warnings"`
}

type ColdStateInspector interface {
	InspectColdState(context.Context, string, string) (ColdStateInspection, error)
}

// ColdStateLayout identifies confidential auxiliary state from observed native
// configuration. It is not a capture, copy authorization, or completeness proof.
// Empty optional fields remain unknown; adapters must not invent state paths.
type ColdStateLayout struct {
	VMID             string       `json:"vmID"`
	Firmware         ColdFirmware `json:"firmware"`
	TPM              *ColdTPM     `json:"tpm"`
	SecretReferences []string     `json:"secretReferences"`
}

type ColdFirmware struct {
	Loader          string     `json:"loader"`
	LoaderType      string     `json:"loaderType"`
	LoaderReadOnly  string     `json:"loaderReadOnly"`
	LoaderSecure    string     `json:"loaderSecure"`
	LoaderFormat    string     `json:"loaderFormat"`
	LoaderStateless string     `json:"loaderStateless"`
	NVRAM           *ColdNVRAM `json:"nvram"`
}

type ColdNVRAM struct {
	Path           string `json:"path"`
	Format         string `json:"format"`
	Template       string `json:"template"`
	TemplateFormat string `json:"templateFormat"`
}

type ColdTPM struct {
	Model                 string `json:"model"`
	Version               string `json:"version"`
	SourceType            string `json:"sourceType"`
	SourcePath            string `json:"sourcePath"`
	PersistentState       string `json:"persistentState"`
	Profile               string `json:"profile"`
	ProfileSource         string `json:"profileSource"`
	ProfileRemoveDisabled string `json:"profileRemoveDisabled"`
	EncryptionSecret      string `json:"encryptionSecret"`
}
