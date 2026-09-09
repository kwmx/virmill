package tui

import (
	"fmt"
	"strings"

	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/validation"
)

func (d ImportDraft) selectedAppliance() (importer.System, bool) {
	if d.Kind != "ova" || d.Report == nil || d.Report.Source != d.Source {
		return importer.System{}, false
	}
	for _, system := range d.Report.Systems {
		if system.ID == d.SystemID {
			return system, true
		}
	}
	return importer.System{}, false
}

// applianceControls displays declarations, not proof that a guest can boot or
// that its original devices or firmware state can be recreated on this host.
func (f ImportForm) applianceControls() []importControl {
	system, ok := f.Draft.selectedAppliance()
	if !ok {
		return []importControl{importButton("back", "Back", "Choose and read an appliance first.")}
	}
	rows := []importControl{}
	info := func(label, value string) {
		text := validation.SafeText(label + ": " + value)
		// Metadata lines remain fully available even at the narrow layout. Each
		// wrapped row participates in normal keyboard scrolling instead of hiding
		// filename suffixes or device descriptions in a truncated table cell.
		for _, line := range wrap(text, 56) {
			rows = append(rows, importControl{id: fmt.Sprintf("summary-%d", len(rows)), kind: "summary", value: line, help: "Source declaration only. Advanced settings controls the new VM."})
		}
	}
	osName := system.OS
	if osName == "" {
		osName = "Not specified by appliance"
	}
	info("Operating system", applianceOSLabel(osName))
	if system.OVFOS != "" && system.OVFOS != system.OS {
		info("Needs review", "OS metadata differs: OVF says "+system.OVFOS)
	}
	cpuHint := "CPU cores for the new VM. The source is not changed."
	memoryHint := "Memory for the new VM; 1024 MiB = 1 GiB."
	if _, origin := creationSourceValue(system.Items, "3", "2"); strings.HasPrefix(origin, "Suggested") {
		cpuHint = "Suggested 2 cores; the appliance did not declare a CPU count."
	}
	if _, origin := creationSourceValue(system.Items, "4", "2048"); strings.HasPrefix(origin, "Suggested") {
		memoryHint = "Suggested 2048 MiB; the appliance did not declare memory."
	}
	rows = append([]importControl{importText("vmName", "VM name", "Choose the name the imported VM should use.", f.Draft.VMName), importText("vcpus", "CPU cores", cpuHint, f.Draft.VCPUs), importText("memoryMiB", "Memory (MiB)", memoryHint, f.Draft.MemoryMiB)}, rows...)
	for _, id := range system.DiskIDs {
		found := false
		for _, disk := range f.Draft.Report.Disks {
			if disk.ID == id {
				capacity := "size needs review"
				if disk.CapacityBytes > 0 {
					capacity = fmt.Sprintf("%.2f GiB", float64(disk.CapacityBytes)/(1<<30))
				}
				info("Disk", disk.Path+" · "+capacity)
				found = true
				break
			}
		}
		if !found {
			info("Needs review", "Disk "+id+" was not found in the descriptor")
		}
	}
	if system.Firmware != "" {
		info("Firmware", strings.ToUpper(system.Firmware)+" declared; confirm compatibility in Advanced settings")
	}
	nic := 0
	for _, item := range system.Items {
		subtype := item.ResourceSubType
		switch item.ResourceType {
		case "3", "4", "17":
			continue
		case "5":
			info("Controller", applianceModel("IDE", subtype))
		case "6":
			info("Controller", applianceModel("SCSI", subtype))
		case "20":
			model := "Storage controller"
			switch strings.ToLower(subtype) {
			case "ahci", "sata", "ahcicontroller":
				model = "SATA (AHCI)"
				subtype = ""
			}
			info("Controller", applianceModel(model, subtype))
		case "10":
			nic++
			model := subtype
			if model == "" {
				model = "model not specified"
			}
			info(fmt.Sprintf("Network adapter %d", nic), model+" · network must be chosen")
		case "23":
			info("USB controller", applianceModel("Declared", subtype)+"; review host compatibility")
		case "32":
			info("Audio", applianceModel("Declared", subtype)+"; not carried over automatically")
		case "32768":
			if !applianceNVRAM(f.Draft.Report, item, info) {
				info("Needs review", applianceUnknown(item))
			}
		default:
			info("Needs review", applianceUnknown(item))
		}
	}
	for _, device := range system.Devices {
		state := "declared"
		if device.Enabled != nil {
			if *device.Enabled {
				state = "enabled"
			} else {
				state = "disabled"
			}
		}
		model := device.Model
		if model == "" {
			model = "unspecified model"
		}
		switch device.Kind {
		case "usb":
			info("USB controller", model+" · "+state+"; review in Advanced settings")
		case "audio":
			info("Audio", model+" · "+state+"; not carried over automatically")
		default:
			info("Needs review", device.Kind+" · "+model+" · "+state)
		}
	}
	if nic == 0 {
		info("Networking", "No original network adapters declared")
	}
	rows = append(rows, importButton("hardware", "Advanced settings", "Review firmware, controllers, networks and other VM options."), importButton("back", "Back", "Return to the source selection."), importButton("next", "Continue", "Choose where to save the imported images."))
	return rows
}
func applianceModel(label, subtype string) string {
	if subtype != "" {
		return label + " · " + subtype
	}
	return label
}
func applianceUnknown(item importer.Item) string {
	value := "Device type " + item.ResourceType
	if item.ResourceSubType != "" {
		value += " · " + item.ResourceSubType
	}
	if item.Description != "" {
		value += " · " + item.Description
	}
	if len(item.HostResources) > 0 {
		value += " · " + strings.Join(item.HostResources, ", ")
	}
	return value
}
func applianceNVRAM(report *importer.Report, item importer.Item, info func(string, string)) bool {
	referenced := false
	for _, resource := range item.HostResources {
		if !strings.HasPrefix(resource, "ovf:/file/") {
			continue
		}
		id := strings.TrimPrefix(resource, "ovf:/file/")
		path := report.FileReferences[id]
		if !strings.HasSuffix(strings.ToLower(path), ".nvram") {
			continue
		}
		referenced = true
		included := false
		for _, member := range report.Members {
			if member.Path == path {
				included = true
				break
			}
		}
		if included {
			info("Firmware state", "included; restoration not supported")
			info("Firmware state file", path)
		} else {
			info("Needs review", "Firmware state reference "+path+" is not included")
		}
	}
	return referenced
}

func applianceOSLabel(value string) string {
	switch value {
	case "Windows11_64":
		return "Windows 11 (64-bit)"
	case "Windows10_64":
		return "Windows 10 (64-bit)"
	case "Windows7_64":
		return "Windows 7 (64-bit)"
	case "Ubuntu_64":
		return "Ubuntu (64-bit)"
	case "Debian_64":
		return "Debian (64-bit)"
	}
	return value
}
