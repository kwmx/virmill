package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/backend/xmlpatch"
)

func bootFixture(t *testing.T) BootForm {
	t.Helper()
	vm := workspaceVM(workspaceVMID, "Installed guest")
	r := BootReport{Resource: vm.Key, State: "stopped", Persistent: &xmlpatch.BootView{Mode: "per-device", Devices: []xmlpatch.BootDevice{
		{Kind: "disk", ID: "vda", Device: "disk", Order: 2, MediaPresent: true, Selectable: true},
		{Kind: "disk", ID: "sda", Device: "cdrom", Order: 1, MediaPresent: true, Selectable: true, ReadOnly: true},
		{Kind: "interface", ID: "52:54:00:11:22:33", Device: "network", Selectable: true},
		{Kind: "interface", ID: "52:54:00:11:22:44", Device: "network", Selectable: false, Reason: "interface link is explicitly down"},
	}}}
	f, err := NewBootForm(vm, r)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestBootFormEjectInstallerRetainsDiskAndStableIdentity(t *testing.T) {
	f := bootFixture(t)
	f.Focus = len(f.Report.Persistent.Devices)
	f, preview, cancel := f.Update(tea.KeyMsg{Type: tea.KeyRight})
	if preview || cancel {
		t.Fatal("selection submitted")
	}
	r, err := f.Request(f.VM.Key.ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	order := r.Input["bootOrder"].([]xmlpatch.BootChoice)
	if r.Action != "set" || r.ID != workspaceVMID || r.Input["applyMode"] != "next-boot" || r.Input["ejectMedia"] != "sda" || len(order) != 1 || order[0].ID != "vda" {
		t.Fatal(r)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "path") || strings.Contains(string(raw), "<domain") {
		t.Fatal("form emitted host path/XML")
	}
	f.Focus++
	_, preview, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !preview || cancel {
		t.Fatal("preview button did not preview")
	}
	if len(f.Report.Persistent.Devices) != 4 || f.Report.Persistent.Devices[1].MediaPresent != true {
		t.Fatal("observed state changed")
	}
}

func TestBootFormSelectMoveAndRefuseUnavailable(t *testing.T) {
	f := bootFixture(t)
	if _, err := f.Request(f.VM.Key.ConnectionID); err == nil {
		t.Fatal("no-op accepted")
	}
	f.Focus = 2
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeySpace})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if len(f.Order) != 3 || f.Order[1].Kind != "interface" {
		t.Fatal(f.Order)
	}
	f.Focus = 3
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeySpace})
	if f.Error == "" || len(f.Order) != 3 {
		t.Fatal("unavailable NIC selected")
	}
	if _, err := f.Request("qemu:///session"); err == nil {
		t.Fatal("connection switch accepted")
	}
	_, preview, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if preview || !cancel {
		t.Fatal("Esc failed")
	}
}

func TestBootFormLegacyEjectionRequiresReplacement(t *testing.T) {
	f := bootFixture(t)
	r := f.Report
	r.Persistent.Mode = "legacy"
	r.Persistent.LegacyOrder = []string{"cdrom", "hd"}
	for i := range r.Persistent.Devices {
		r.Persistent.Devices[i].Order = 0
	}
	f, err := NewBootForm(f.VM, r)
	if err != nil {
		t.Fatal(err)
	}
	f.Focus = 4
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
	if _, err = f.Request(f.VM.Key.ConnectionID); err == nil || !strings.Contains(err.Error(), "replacement") {
		t.Fatal(err)
	}
	f.Focus = 0
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeySpace})
	if _, err = f.Request(f.VM.Key.ConnectionID); err != nil {
		t.Fatal(err)
	}
}

func TestBootFormRefusesUnsafeObservation(t *testing.T) {
	for _, kind := range []string{"wrong-vm", "suspended", "managed-save", "no-persistent", "direct-boot", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			f := bootFixture(t)
			r := f.Report
			switch kind {
			case "wrong-vm":
				r.Resource.UUID = "22345678-1234-4234-8234-123456789abc"
			case "suspended":
				r.State = "pmsuspended"
			case "managed-save":
				r.HasManagedSave = true
			case "no-persistent":
				r.Persistent = nil
			case "direct-boot":
				r.Persistent.Mode = "direct-boot"
			case "duplicate":
				r.Persistent.Devices = append(r.Persistent.Devices, r.Persistent.Devices[0])
			}
			if _, err := NewBootForm(f.VM, r); err == nil {
				t.Fatal("unsafe report accepted")
			}
		})
	}
}

func TestBootFormResponsiveAndSanitized(t *testing.T) {
	f := bootFixture(t)
	f.VM.Name = "guest\x1b[2J\u202e"
	f.Error = "Installer removal needs a replacement boot disk. Choose the system disk before trying again."
	for _, size := range [][2]int{{80, 24}, {120, 36}, {40, 10}, {20, 5}} {
		view := f.View(size[0], size[1])
		lines := strings.Split(view, "\n")
		if len(lines) > size[1] {
			t.Fatal("height", size, view)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] || strings.ContainsAny(line, "\x1b\u202e") {
				t.Fatal("width/control", line)
			}
		}
	}
}
