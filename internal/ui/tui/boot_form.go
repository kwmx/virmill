package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// BootReport decodes vm.boot.get. The form consumes observed devices, never XML.
type BootReport struct {
	Resource          domain.ResourceKey `json:"resource"`
	State             string             `json:"state"`
	HasManagedSave    bool               `json:"hasManagedSave"`
	Persistent        *xmlpatch.BootView `json:"persistent"`
	Live              *xmlpatch.BootView `json:"live"`
	GuestBootVerified bool               `json:"guestBootVerified"`
}

type BootForm struct {
	VM           domain.VM
	Report       BootReport
	Order        []xmlpatch.BootChoice
	Eject        string
	Focus        int
	Error        string
	changedOrder bool
}

func NewBootForm(vm domain.VM, report BootReport) (BootForm, error) {
	f := BootForm{VM: vm, Report: report}
	if vm.Key != report.Resource || vm.Key.ProviderID != "libvirt" || vm.Key.Kind != "vm" || !guidedUUID.MatchString(vm.Key.UUID) || vm.Key.UUID == "00000000-0000-0000-0000-000000000000" || !guidedLocal(vm.Key.ConnectionID) {
		return f, fmt.Errorf("Choose the VM again; the boot report belongs to a different resource.")
	}
	if report.State != "stopped" || report.HasManagedSave || vm.State != "stopped" || vm.HasManagedSave || report.Persistent == nil {
		return f, fmt.Errorf("Shut down this persistent VM and resolve saved state before changing boot options.")
	}
	if report.Persistent.Mode == "direct-boot" || report.Persistent.Mode == "conflicting" {
		return f, fmt.Errorf("This VM uses a boot method that the boot-order editor cannot safely change.")
	}
	view := *report.Persistent
	view.Devices = slices.Clone(view.Devices)
	view.LegacyOrder = slices.Clone(view.LegacyOrder)
	f.Report.Persistent = &view
	devices := slices.Clone(view.Devices)
	sort.SliceStable(devices, func(i, j int) bool { return devices[i].Order < devices[j].Order })
	seen := map[string]bool{}
	for _, d := range devices {
		key := d.Kind + ":" + d.ID
		if seen[key] || d.ID == "" || (d.Kind != "disk" && d.Kind != "interface") {
			return f, fmt.Errorf("Boot devices are incomplete or ambiguous. Refresh the VM before editing.")
		}
		seen[key] = true
		if d.Order > 0 {
			f.Order = append(f.Order, xmlpatch.BootChoice{Kind: d.Kind, ID: d.ID})
		}
	}
	return f, nil
}

func (f BootForm) ejectChoices() []string {
	choices := []string{""}
	if f.Report.Persistent != nil {
		for _, d := range f.Report.Persistent.Devices {
			if d.Kind == "disk" && d.Device == "cdrom" && d.ReadOnly && d.MediaPresent {
				choices = append(choices, d.ID)
			}
		}
	}
	return choices
}

func (f BootForm) Update(key tea.KeyMsg) (BootForm, bool, bool) {
	f.Order = slices.Clone(f.Order)
	if key.Type == tea.KeyEsc {
		return f, false, true
	}
	if f.Report.Persistent == nil {
		f.Error = "Refresh the boot report before editing."
		return f, false, false
	}
	n := len(f.Report.Persistent.Devices)
	f.Focus = max(0, min(f.Focus, n+1))
	switch key.Type {
	case tea.KeyTab, tea.KeyDown:
		f.Focus = (f.Focus + 1) % (n + 2)
	case tea.KeyShiftTab, tea.KeyUp:
		f.Focus = (f.Focus + n + 1) % (n + 2)
	case tea.KeyEnter, tea.KeySpace, tea.KeyLeft, tea.KeyRight:
		f.Error = ""
		if f.Focus == n+1 {
			if key.Type == tea.KeyEnter || key.Type == tea.KeySpace {
				_, err := f.Request(f.VM.Key.ConnectionID)
				if err != nil {
					f.Error = err.Error()
				} else {
					return f, true, false
				}
			}
		} else if f.Focus == n {
			choices := f.ejectChoices()
			at := max(0, slices.Index(choices, f.Eject))
			delta := 1
			if key.Type == tea.KeyLeft {
				delta = -1
			}
			f.Eject = choices[(at+delta+len(choices))%len(choices)]
			// Ejecting a candidate requires an explicitly reviewed replacement order.
			if f.Eject != "" {
				for i, c := range f.Order {
					if c.Kind == "disk" && c.ID == f.Eject {
						f.Order = slices.Delete(f.Order, i, i+1)
						f.changedOrder = true
						break
					}
				}
			}
		} else {
			d := f.Report.Persistent.Devices[f.Focus]
			c := xmlpatch.BootChoice{Kind: d.Kind, ID: d.ID}
			at := slices.Index(f.Order, c)
			if key.Type == tea.KeyLeft || key.Type == tea.KeyRight {
				to := at - 1
				if key.Type == tea.KeyRight {
					to = at + 1
				}
				if at >= 0 && to >= 0 && to < len(f.Order) {
					f.Order[at], f.Order[to] = f.Order[to], f.Order[at]
					f.changedOrder = true
				}
			} else if at >= 0 {
				f.Order = slices.Delete(f.Order, at, at+1)
				f.changedOrder = true
			} else if !d.Selectable {
				f.Error = "This device cannot boot: " + d.Reason
			} else if d.Kind == "disk" && d.ID == f.Eject {
				f.Error = "Keep this CD-ROM's media before adding it to the boot order."
			} else {
				f.Order = append(f.Order, c)
				f.changedOrder = true
			}
		}
	}
	return f, false, false
}

func (f BootForm) Request(connection string) (app.Request, error) {
	r := app.Request{Connection: connection, ID: f.VM.Key.UUID, Action: "set", Input: map[string]any{"applyMode": "next-boot"}}
	if _, err := NewBootForm(f.VM, f.Report); err != nil {
		return r, err
	}
	if connection != f.VM.Key.ConnectionID {
		return r, fmt.Errorf("Return to the selected VM's connection before editing.")
	}
	if !f.changedOrder && f.Eject == "" {
		return r, fmt.Errorf("Choose a boot-order change or media to eject, then preview.")
	}
	if f.changedOrder {
		if len(f.Order) == 0 || len(f.Order) > 128 {
			return r, fmt.Errorf("Select at least one boot device after removing the installer.")
		}
		seen := map[xmlpatch.BootChoice]bool{}
		for _, c := range f.Order {
			valid := false
			for _, d := range f.Report.Persistent.Devices {
				if c.Kind == d.Kind && c.ID == d.ID {
					valid = d.Selectable && !(c.Kind == "disk" && c.ID == f.Eject)
				}
			}
			if !valid || seen[c] {
				return r, fmt.Errorf("Select unique, available devices from this VM's boot list.")
			}
			seen[c] = true
		}
		r.Input["bootOrder"] = slices.Clone(f.Order)
	}
	if f.Eject != "" {
		if !slices.Contains(f.ejectChoices(), f.Eject) {
			return r, fmt.Errorf("Choose a loaded, read-only CD-ROM from this VM.")
		}
		if !f.changedOrder && slices.Contains(f.Report.Persistent.LegacyOrder, "cdrom") {
			return r, fmt.Errorf("Choose a replacement boot order before ejecting this legacy boot CD-ROM.")
		}
		r.Input["ejectMedia"] = f.Eject
	}
	return r, nil
}

func (f BootForm) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	clean := func(s string) string {
		return ansi.Truncate(strings.NewReplacer("\n", " ", "\t", " ").Replace(validation.SafeText(s)), width, "…")
	}
	if width < 40 || height < 10 {
		return strings.Join([]string{clean("Resize to edit boot options."), clean("Your choices are retained.")}[:min(2, height)], "\n")
	}
	lines := []string{"Boot order and installer media", "VM: " + f.VM.Name, "Changes apply next boot. Ejected files are kept.", ""}
	rows := []string{}
	if f.Report.Persistent != nil {
		for _, d := range f.Report.Persistent.Devices {
			at := slices.Index(f.Order, xmlpatch.BootChoice{Kind: d.Kind, ID: d.ID})
			label := "[ ] "
			if at >= 0 {
				label = fmt.Sprintf("[%d] ", at+1)
			}
			kind := d.Device
			if d.Kind == "interface" {
				kind = "Network adapter"
			}
			label += kind + " " + d.ID
			if !d.Selectable {
				label += " (unavailable)"
			}
			rows = append(rows, label)
		}
	}
	eject := "Keep all media"
	if f.Eject != "" {
		eject = "Eject " + f.Eject + " (keep file)"
	}
	rows = append(rows, "Installer media: < "+eject+" >", "[ Preview changes ]")
	focus := max(0, min(f.Focus, len(rows)-1))
	count := max(1, height-11)
	start := max(0, focus-count+1)
	for i := start; i < min(len(rows), start+count); i++ {
		mark := "  "
		if i == focus {
			mark = "> "
		}
		lines = append(lines, mark+rows[i])
	}
	help := "Space selects a device; Left/Right moves its boot priority."
	if focus == len(rows)-2 {
		help = "Left/Right chooses a loaded CD-ROM to eject; its file stays intact."
	}
	if focus == len(rows)-1 {
		help = "Review the exact changes before applying anything."
	}
	lines = append(lines, "", help)
	if f.Report.Persistent != nil && len(f.Order) == 0 {
		lines = append(lines, "Current firmware policy is kept until you select boot devices.")
	}
	if f.Error != "" {
		lines = append(lines, wrap("Issue: "+validation.SafeText(f.Error), width)...)
	}
	footer := "Tab/Up/Down Select   Space Toggle   Left/Right Change   Esc Back"
	if len(lines) >= height {
		lines = lines[:height-1]
	}
	lines = append(lines, footer)
	for i := range lines {
		lines[i] = clean(lines[i])
	}
	return strings.Join(lines, "\n")
}
