package protection

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestFirmwareTPMCompletenessAndIndependentArtifactChecks(t *testing.T) {
	dir := t.TempDir()
	m := Manifest{APIVersion: "virmill/v1", ID: "fixture-backup", VMID: "fixture-vm", Mode: "cold", Consistency: "cold-complete", HasNVRAM: true, HasTPM: true, IndependentlyRecoverable: true}
	for _, kind := range []string{"disk", "persistent-xml", "nvram", "tpm"} {
		b := []byte("synthetic " + kind)
		h := sha256.Sum256(b)
		os.WriteFile(filepath.Join(dir, kind), b, 0600)
		m.Required = append(m.Required, kind)
		m.Members = append(m.Members, Member{ID: kind, Kind: kind, Path: kind, Size: int64(len(b)), SHA256: hex.EncodeToString(h[:])})
	}
	if e := m.Verify(dir); e != nil {
		t.Fatal(e)
	}
	missing := m
	missing.Members = missing.Members[:3]
	if missing.Validate() == nil {
		t.Fatal("missing TPM accepted")
	}
	live := m
	live.Mode = "live-disk"
	if live.Validate() == nil {
		t.Fatal("live auxiliary completeness fabricated")
	}
	dependent := m
	dependent.ExternalSecrets = []string{"unavailable-secret"}
	if dependent.Validate() == nil {
		t.Fatal("external secret called independent")
	}
	os.WriteFile(filepath.Join(dir, "nvram"), []byte("corrupt"), 0600)
	if m.Verify(dir) == nil {
		t.Fatal("firmware corruption ignored")
	}
}
