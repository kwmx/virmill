package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/validation"
)

// ImportDraft holds editable UI values. Request emits the existing language-neutral
// service input; no file import, disk conversion or VM mutation happens here.
type ImportDraft struct {
	Kind, Source, DestinationParent, DestinationName string
	SystemID, MediaID, SHA256                        string
	VMName, VCPUs, MemoryMiB                         string
	Offline                                          bool
	Disks                                            []ImportDisk
	Files                                            []ImportFile
	Report                                           *importer.Report
	Description                                      *importer.SourceDescription
	SelectedSource                                   string
	selectionSource, selectionSystem                 string
}
type ImportDisk struct{ ID, Path, Format, SizeMiB string }
type ImportFile struct{ Path, SHA256 string }
type ImportIntent struct {
	Kind, Target string
	Index        int
}

// Intent Kind: browse, inspect, preview, export, cancel. Browse Target:
// source, destination, disk, backing. Index identifies the draft disk row.

func (d *ImportDraft) ApplyInspection(r importer.Report) error {
	if d.Kind != "ova" || r.Source != d.Source || len(r.Systems) == 0 {
		return fmt.Errorf("Choose and inspect a valid OVA first.")
	}
	d.Report = &r
	if len(r.Systems) == 1 {
		d.SystemID = r.Systems[0].ID
		return d.SelectSystem(d.SystemID)
	}
	if d.selectionSource == d.Source && d.selectionSystem == d.SystemID {
		for _, system := range r.Systems {
			if system.ID == d.SystemID {
				return d.SelectSystem(system.ID)
			}
		}
	}
	d.SystemID = ""
	d.Disks = nil
	d.VMName, d.VCPUs, d.MemoryMiB = "", "", ""
	return nil
}
func (d *ImportDraft) SelectSystem(id string) error {
	if d.Report == nil {
		return fmt.Errorf("Inspect the OVA first.")
	}
	for _, sys := range d.Report.Systems {
		if sys.ID != id {
			continue
		}
		if len(sys.DiskIDs) < 1 || len(sys.DiskIDs) > 64 {
			return fmt.Errorf("This appliance needs 1–64 attached disks.")
		}
		rows := []ImportDisk{}
		for _, diskID := range sys.DiskIDs {
			found := false
			for _, disk := range d.Report.Disks {
				if disk.ID == diskID {
					format := ""
					for _, candidate := range []string{"qcow2", "vmdk", "vhdx", "vdi", "vpc", "raw"} {
						if strings.Contains(strings.ToLower(disk.Format), candidate) {
							format = candidate
							break
						}
					}
					// An undeclared format gets a labelled suggestion from the file
					// name; a declared but unrecognised one still needs a choice.
					if format == "" && disk.Format == "" {
						format = formatFromName(disk.Path)
					}
					row := ImportDisk{ID: disk.ID, Path: disk.Path, Format: format}
					if disk.CapacityBytes > 0 && disk.CapacityBytes <= 512<<30 {
						// Round the verified unit conversion up; never lower a capacity
						// limit to make a destination appear to have sufficient space.
						mib := disk.CapacityBytes / (1 << 20)
						if disk.CapacityBytes%(1<<20) != 0 {
							mib++
						}
						row.SizeMiB = strconv.FormatInt(mib, 10)
					}
					rows = append(rows, row)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("The appliance has an unresolved disk attachment.")
			}
		}
		d.SystemID = id
		d.Disks = rows
		preserve := d.selectionSource == d.Source && d.selectionSystem == id
		if _, err := validation.DisplayName(d.VMName); !preserve || err != nil {
			d.VMName = sys.Name
			if d.VMName == "" {
				d.VMName = sys.ID
			}
			if d.VMName == "" {
				d.VMName = "new-vm"
			}
		}
		if _, ok := guidedNumber(d.VCPUs, 512); !preserve || !ok {
			d.VCPUs, _ = creationSourceValue(sys.Items, "3", "2")
		}
		if memory, ok := guidedNumber(d.MemoryMiB, 1<<20); !preserve || !ok || memory < 128 {
			d.MemoryMiB, _ = creationSourceValue(sys.Items, "4", "2048")
		}
		d.selectionSource, d.selectionSystem = d.Source, id
		return nil
	}
	return fmt.Errorf("Choose an appliance from the inspected list.")
}

// SummaryValidation names the editable appliance field that needs attention.
// These choices become VM hardware; they are not image-conversion parameters.
func (d ImportDraft) SummaryValidation() (string, error) {
	if _, ok := d.selectedAppliance(); !ok && !d.HasSourceDescription() {
		return "", fmt.Errorf("Choose and inspect a source to review first.")
	}
	if _, err := validation.DisplayName(d.VMName); err != nil {
		return "vmName", fmt.Errorf("VM name: enter a nonempty name without control characters.")
	}
	if _, ok := guidedNumber(d.VCPUs, 512); !ok {
		return "vcpus", fmt.Errorf("CPU cores: enter a whole number from 1 to 512.")
	}
	if memory, ok := guidedNumber(d.MemoryMiB, 1<<20); !ok || memory < 128 {
		return "memoryMiB", fmt.Errorf("Memory: enter 128–1048576 MiB.")
	}
	return "", nil
}

// DetectedResources returns only unambiguous hints from the selected system.
// Zero means the next step needs an explicit choice, not a guessed allocation.
func (d ImportDraft) DetectedResources() (cpus, memoryMiB int64) {
	if d.Kind != "ova" || d.Report == nil || d.Report.Source != d.Source {
		return 0, 0
	}
	for _, system := range d.Report.Systems {
		if system.ID != d.SystemID {
			continue
		}
		cpuItems, memoryItems := 0, 0
		for _, item := range system.Items {
			switch item.ResourceType {
			case "3":
				cpuItems++
				value, err := strconv.ParseInt(item.Quantity, 10, 64)
				if err == nil && value > 0 && value <= 512 && strconv.FormatInt(value, 10) == item.Quantity {
					cpus = value
				}
			case "4":
				memoryItems++
				if item.MemoryMiB > 0 && item.MemoryMiB <= 1048576 {
					memoryMiB = item.MemoryMiB
				}
			}
		}
		if cpuItems != 1 {
			cpus = 0
		}
		if memoryItems != 1 {
			memoryMiB = 0
		}
		return cpus, memoryMiB
	}
	return 0, 0
}
func (d ImportDraft) Request(connection string) (string, app.Request, error) {
	fail := func(message string) (string, app.Request, error) { return "", app.Request{}, fmt.Errorf("%s", message) }
	if !guidedLocal(connection) || !guidedPath(d.Source) {
		return fail("Choose a source on this computer.")
	}
	if !guidedPath(d.DestinationParent) || d.DestinationName == "" || !guidedPrintable(d.DestinationName) || strings.TrimSpace(d.DestinationName) != d.DestinationName || filepath.Base(d.DestinationName) != d.DestinationName || d.DestinationName == "." || d.DestinationName == ".." || strings.ContainsAny(d.DestinationName, "/\\") {
		return fail("Choose a parent folder and enter a new output folder name.")
	}
	destination := filepath.Join(d.DestinationParent, d.DestinationName)
	if !guidedPath(destination) {
		return fail("Choose a shorter output path.")
	}
	input := map[string]any{"destination": destination}
	if len(d.Disks) < 1 || len(d.Disks) > 64 {
		return fail("Add between 1 and 64 disks.")
	}
	disks := []map[string]any{}
	seen := map[string]bool{}
	for _, disk := range d.Disks {
		if disk.ID == "" || seen[disk.ID] || !guidedPrintable(disk.ID) || len(disk.ID) > 256 {
			return fail("Each disk needs a unique ID.")
		}
		seen[disk.ID] = true
		size, err := strconv.ParseInt(disk.SizeMiB, 10, 64)
		if err != nil || size < 1 || size > 524288 || strconv.FormatInt(size, 10) != disk.SizeMiB {
			return fail("Enter a disk size between 1 and 524288 MiB.")
		}
		if d.Kind == "ova" && d.Report != nil && d.Report.Source == d.Source {
			for _, detected := range d.Report.Disks {
				if detected.ID == disk.ID && detected.CapacityBytes > size<<20 {
					return fail("The disk limit cannot be smaller than its detected capacity. Choose more storage; this option does not resize the source disk.")
				}
			}
		}
		row := map[string]any{"id": disk.ID}
		if d.Kind == "iso" {
			row["virtualBytes"] = size << 20
		} else {
			if !slices.Contains([]string{"raw", "qcow2", "vmdk", "vdi", "vpc", "vhdx"}, disk.Format) {
				return fail("Choose the source format for each disk.")
			}
			row["format"] = disk.Format
			row["maximumVirtualBytes"] = size << 20
		}
		if d.Kind != "ova" && !guidedRootID.MatchString(disk.ID) {
			return fail("Use letters, numbers, dots, underscores or dashes in disk IDs.")
		}
		if d.Kind == "disks" {
			if err := importer.SafePath(disk.Path); err != nil {
				return fail("Choose each disk within the selected source folder.")
			}
			row["path"] = disk.Path
		}
		disks = append(disks, row)
	}
	input["disks"] = disks
	method, action := "", ""
	switch d.Kind {
	case "ova":
		if d.Report == nil || d.Report.Source != d.Source {
			return fail("Inspect this OVA before continuing.")
		}
		var system *importer.System
		for i := range d.Report.Systems {
			if d.Report.Systems[i].ID == d.SystemID {
				system = &d.Report.Systems[i]
				break
			}
		}
		if system == nil || len(system.DiskIDs) != len(disks) {
			return fail("Choose an appliance and include every attached disk.")
		}
		for _, id := range system.DiskIDs {
			if !seen[id] {
				return fail("Include every disk attached to the selected appliance.")
			}
		}
		input["systemID"] = d.SystemID
		method, action = "import.prepare", "prepare"
	case "iso", "disks":
		if !d.Offline {
			return fail("Confirm the source images are not in use.")
		}
		input["offlineSources"] = true
		if d.Kind == "iso" {
			if !guidedRootID.MatchString(d.MediaID) || seen[d.MediaID] {
				return fail("Use a unique installation-media ID.")
			}
			input["mediaID"] = d.MediaID
			if d.SHA256 != "" {
				if !importDigest(d.SHA256) {
					return fail("Expected SHA-256 must contain 64 lowercase hexadecimal characters.")
				}
				input["sha256"] = d.SHA256
			}
			method, action = "import.prepare-install", "prepare-install"
		} else {
			files := []map[string]any{}
			paths := map[string]bool{}
			for _, f := range d.Files {
				if importer.SafePath(f.Path) != nil || paths[f.Path] {
					return fail("Choose distinct source files within the source folder.")
				}
				paths[f.Path] = true
				row := map[string]any{"path": f.Path}
				if f.SHA256 != "" {
					if !importDigest(f.SHA256) {
						return fail("Expected SHA-256 must contain 64 lowercase hexadecimal characters.")
					}
					row["sha256"] = f.SHA256
				}
				files = append(files, row)
			}
			if len(files) < 1 || len(files) > 10000 {
				return fail("Choose source files, including any backing or extent files.")
			}
			for _, disk := range d.Disks {
				if !paths[disk.Path] {
					return fail("Each disk must be included in the selected source files.")
				}
			}
			input["files"] = files
			method, action = "import.prepare-disks", "prepare-disks"
		}
	default:
		return fail("Choose OVA, ISO or existing disks.")
	}
	return method, app.Request{Connection: connection, Path: d.Source, Action: action, Input: input}, nil
}
func importDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
