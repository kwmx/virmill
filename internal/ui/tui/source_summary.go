package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/validation"
)

// SelectedSource is the user's original file/folder. Source is the preparation
// service's root directory for disk sets and remains the original file for ISO/OVA.
func (d ImportDraft) HasSourceDescription() bool {
	r := d.Description
	if r == nil || d.SelectedSource == "" || r.Source != d.SelectedSource {
		return false
	}
	switch r.Kind {
	case "ova":
		return d.Kind == "ova" && d.Source == r.Source && d.Report != nil && d.Report.Source == r.Source
	case "iso":
		return d.Kind == "iso" && d.Source == r.Source
	case "disk", "disks":
		return d.Kind == "disks" && d.Source == r.Root
	}
	return false
}

func (d *ImportDraft) ApplySourceDescription(r importer.SourceDescription) error {
	selected := d.SelectedSource
	if selected == "" {
		selected = d.Source
	}
	if !guidedPath(selected) || r.Source != selected || r.PhysicalBytes < 0 {
		return fmt.Errorf("Choose the same source again; its description does not match the selection.")
	}
	next := *d
	preserve := d.HasSourceDescription() && d.Description.Source == r.Source && d.Description.Kind == r.Kind
	switch r.Kind {
	case "ova":
		if r.Appliance == nil || r.Appliance.Source != r.Source {
			return fmt.Errorf("The appliance metadata is incomplete. Choose the source again.")
		}
		next.Kind, next.Source = "ova", r.Source
		if err := next.ApplyInspection(*r.Appliance); err != nil {
			return err
		}
	case "iso":
		next.Kind, next.Source = "iso", r.Source
		next.Report = nil
		next.SystemID = ""
		next.Files = nil
		if !preserve || !guidedRootID.MatchString(next.MediaID) {
			next.MediaID = "installer"
		}
		if !preserve || len(next.Disks) == 0 {
			next.Disks = []ImportDisk{{ID: "disk1", SizeMiB: "32768"}}
		}
	case "disk", "disks":
		if !guidedPath(r.Root) || len(r.Disks) < 1 || len(r.Disks) > 64 || len(r.Files) < 1 || len(r.Files) > 10000 {
			return fmt.Errorf("Choose a disk or folder containing supported disk images.")
		}
		if (r.Kind == "disk" && r.Root != filepath.Dir(r.Source)) || (r.Kind == "disks" && r.Root != r.Source) {
			return fmt.Errorf("The disk description points outside the selected source folder.")
		}
		files := []ImportFile{}
		paths := map[string]bool{}
		for _, path := range r.Files {
			if importer.SafePath(path) != nil || paths[path] {
				return fmt.Errorf("The source file list contains an invalid or duplicate path.")
			}
			paths[path] = true
			files = append(files, ImportFile{Path: path})
		}
		if r.Kind == "disk" && !paths[filepath.Base(r.Source)] {
			return fmt.Errorf("The selected disk is missing from its source file list.")
		}
		rows := []ImportDisk{}
		ids := map[string]bool{}
		for _, disk := range r.Disks {
			if !guidedRootID.MatchString(disk.ID) || ids[disk.ID] || !paths[disk.Path] || disk.VirtualBytes < 0 || disk.PhysicalBytes < 0 {
				return fmt.Errorf("The disk description has an invalid identity, file or size.")
			}
			ids[disk.ID] = true
			row := ImportDisk{ID: disk.ID, Path: disk.Path}
			if slices.Contains([]string{"raw", "qcow2", "vmdk", "vdi", "vpc", "vhdx"}, disk.Format) {
				row.Format = disk.Format
			}
			if disk.VirtualBytes > 0 && disk.VirtualBytes <= 512<<30 {
				size := disk.VirtualBytes / (1 << 20)
				if disk.VirtualBytes%(1<<20) != 0 {
					size++
				}
				row.SizeMiB = strconv.FormatInt(size, 10)
			}
			if preserve {
				for _, old := range d.Disks {
					if old.ID == row.ID && old.Path == row.Path {
						if slices.Contains([]string{"raw", "qcow2", "vmdk", "vdi", "vpc", "vhdx"}, old.Format) {
							row.Format = old.Format
						}
						if size, ok := guidedNumber(old.SizeMiB, 524288); ok && int64(size<<20) >= disk.VirtualBytes {
							row.SizeMiB = old.SizeMiB
						}
					}
				}
			}
			rows = append(rows, row)
		}
		next.Kind, next.Source = "disks", r.Root
		next.Disks, next.Files = rows, files
		next.Report = nil
		next.SystemID = ""
		next.MediaID = ""
	default:
		return fmt.Errorf("This source type is not supported. Choose an OVA, ISO or supported disk image.")
	}
	if r.Kind != "ova" {
		if _, err := validation.DisplayName(next.VMName); !preserve || err != nil {
			next.VMName = suggestedVMName(r.Name)
			if next.VMName == "" {
				next.VMName = strings.TrimSuffix(filepath.Base(r.Source), filepath.Ext(r.Source))
			}
			if next.VMName == "" {
				next.VMName = "new-vm"
			}
		}
		if _, ok := guidedNumber(next.VCPUs, 512); !preserve || !ok {
			next.VCPUs = "2"
		}
		if memory, ok := guidedNumber(next.MemoryMiB, 1<<20); !preserve || !ok || memory < 128 {
			next.MemoryMiB = "2048"
		}
		next.selectionSource, next.selectionSystem = r.Source, ""
	}
	if !preserve {
		next.Offline = false
		next.SHA256 = ""
	}
	r.Disks = slices.Clone(r.Disks)
	r.Files = slices.Clone(r.Files)
	r.Warnings = slices.Clone(r.Warnings)
	next.SelectedSource = r.Source
	next.Description = &r
	*d = next
	return nil
}

func (f ImportForm) sourceControls() []importControl {
	if f.Draft.Kind == "ova" {
		return f.applianceControls()
	}
	if !f.Draft.HasSourceDescription() {
		return []importControl{importButton("back", "Back", "Choose and inspect a source first.")}
	}
	d := f.Draft
	r := d.Description
	rows := []importControl{importText("vmName", "VM name", "Choose the name for the new VM.", d.VMName), importText("vcpus", "CPU cores", "Suggested: 2 cores. Disk and ISO files do not describe VM hardware.", d.VCPUs), importText("memoryMiB", "Memory (MiB)", "Suggested: 2048 MiB. This is your VM choice, not detected memory.", d.MemoryMiB)}
	info := func(label, value string) {
		for _, line := range applianceWrap(label+": "+value, 56) {
			rows = append(rows, importControl{id: fmt.Sprintf("summary-%d", len(rows)), kind: "summary", value: line, help: "Source metadata only. Preparation verifies the selected files."})
		}
	}
	info("Source", r.Source)
	kind := map[string]string{"iso": "Installation ISO", "disk": "Disk image", "disks": "Disk image folder"}[r.Kind]
	if r.Format != "" {
		kind += " (" + r.Format + ")"
	}
	info("Type", kind)
	info("Source size", sourceBytes(r.PhysicalBytes))
	info("Operating system", "Not determined from this source")
	if r.Kind == "iso" {
		for _, disk := range d.Disks {
			info("New blank disk", disk.ID+" · "+disk.SizeMiB+" MiB (VM option; editable)")
		}
		info("Installation", "Boot the ISO to install an OS; no automatic installation is assumed")
	} else {
		roots := map[string]bool{}
		for _, disk := range r.Disks {
			roots[disk.Path] = true
			format := disk.Format
			if format == "" {
				format = "format needs review"
			}
			info("Disk", disk.Path+" · "+format+" · "+sourceBytes(disk.VirtualBytes)+" virtual")
			if disk.BackingPath != "" {
				value := disk.BackingPath
				if disk.BackingFormat != "" {
					value += " (" + disk.BackingFormat + ")"
				}
				info("Backing dependency", value)
			}
		}
		for _, path := range r.Files {
			if !roots[path] {
				info("Included backing / extent", path)
			}
		}
	}
	for _, warning := range r.Warnings {
		info("Needs review", warning)
	}
	rows = append(rows, importButton("hardware", "Advanced settings", "Review firmware, disk controllers and networks."), importButton("back", "Back", "Return to the source selection."), importButton("next", "Continue", "Choose where to save the prepared images."))
	return rows
}
func sourceBytes(n int64) string {
	if n <= 0 {
		return "size not determined"
	}
	if n >= 1<<30 {
		return fmt.Sprintf("%.2f GiB", float64(n)/(1<<30))
	}
	if n >= 1<<20 {
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d bytes", n)
}
