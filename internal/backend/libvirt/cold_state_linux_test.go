//go:build linux && cgo

package libvirt

import (
	"context"
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
)

func TestColdStateInspectionDoesNotReadOrInferCapturedState(t *testing.T) {
	id := "b9496482-2eeb-40e1-892b-4e291c108c52"
	vm := domain.VM{Key: domain.ResourceKey{UUID: id}, State: "running", Autostart: true, HasManagedSave: true, Fingerprint: "observed", PersistentXML: `<domain type="kvm"><name>synthetic</name><uuid>` + id + `</uuid><os><type arch="x86_64" machine="pc-q35-10.2">hvm</type></os><devices><tpm model="tpm-crb"><backend type="emulator" version="2.0"/></tpm></devices></domain>`}
	v, err := coldStateInspection(context.Background(), vm)
	if err != nil {
		t.Fatal(err)
	}
	if v.Fingerprint != vm.Fingerprint || v.State != "running" || v.Layout.TPM == nil || v.Layout.TPM.SourcePath != "" || len(v.Warnings) < 4 {
		t.Fatal("capture or path inferred", v)
	}
	if !strings.Contains(strings.Join(v.Warnings, " "), "No disk, NVRAM, TPM or secret bytes") {
		t.Fatal("inspection overstated", v)
	}
	vm.Key.UUID = "different"
	if _, err = coldStateInspection(context.Background(), vm); err == nil {
		t.Fatal("mismatched native identity accepted")
	}
	vm.PersistentXML = ""
	if _, err = coldStateInspection(context.Background(), vm); err == nil {
		t.Fatal("missing persistent definition accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = coldStateInspection(ctx, vm); err == nil {
		t.Fatal("canceled observation accepted")
	}
}
