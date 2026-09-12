package tui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
)

func TestSetupDocumentKeepsCompleteCreationAndNoObservations(t *testing.T) {
	m := NewWorkspace(nil, "qemu:///system")
	f := creationFormFixture()
	f.Spec.DevicePolicy = &domain.CreationDevicePolicy{Version: 1, Chipset: "q35", PCIPlacement: "automatic", USBController: "xhci", Audio: "ich9"}
	f.Spec.NICs = append(f.Spec.NICs, domain.CreationNIC{ID: "nic2", SourceIndex: 1, NetworkID: "other", Model: "e1000e", Link: "down", MAC: "52:54:00:12:34:56"})
	f.Spec.GuestAgent = true
	f.CPUText = "12 unfinished"
	f.MemoryText = "18432"
	f.Source.System.Name = "metadata-sentinel"
	m.Creation = &f
	d := m.setupDocument()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "metadata-sentinel") || strings.Contains(string(raw), "Options") || strings.Contains(string(raw), "Networks") {
		t.Fatal("cached observations leaked", string(raw))
	}
	restored, err := decodeSetup(raw, m.Connection)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Creation.Spec, f.Spec) || restored.Creation.CPUText != f.CPUText || restored.Creation.MemoryText != f.MemoryText {
		t.Fatal("advanced settings lost")
	}
	f.Spec.NICs[0].Model = "mutated"
	if d.Creation.Spec.NICs[0].Model == "mutated" {
		t.Fatal("queued snapshot aliases live form")
	}
}
func TestSetupImportProjectionExcludesUntrustedMetadata(t *testing.T) {
	m := NewWorkspace(nil, "qemu:///system")
	f := NewImportForm("iso")
	f.Draft.Source = "/tmp/image.iso"
	f.Draft.Description = &importer.SourceDescription{Source: f.Draft.Source, Kind: "iso", Name: "secret-raw-metadata"}
	f.Draft.Report = &importer.Report{Source: "secret-raw-report"}
	f.Draft.Disks = []ImportDisk{{ID: "disk1", SizeMiB: "65536"}}
	m.Import = &f
	raw, _ := json.Marshal(m.setupDocument())
	if strings.Contains(string(raw), "secret-raw") || strings.Contains(string(raw), "Description") || strings.Contains(string(raw), "Report") {
		t.Fatal(string(raw))
	}
	if _, err := decodeSetup(raw, "qemu:///session"); err == nil {
		t.Fatal("connection mismatch accepted")
	}
	withPlan := strings.TrimSuffix(string(raw), "}") + `,"plan":{"id":"secret"}}`
	if _, err := decodeSetup([]byte(withPlan), m.Connection); err == nil {
		t.Fatal("unallowlisted plan accepted")
	}
}
