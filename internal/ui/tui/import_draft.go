package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
)

// ImportDraft holds editable UI values. Request emits the existing language-neutral
// service input; no file import, disk conversion or VM mutation happens here.
type ImportDraft struct {
	Kind, Source, DestinationParent, DestinationName string
	SystemID, MediaID, SHA256                        string
	Offline                                          bool
	Disks                                            []ImportDisk
	Files                                            []ImportFile
	Report                                           *importer.Report
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
	d.SystemID = ""
	d.Disks = nil
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
					// Capacity units are not normalized by the importer report. Require an
					// explicit bound instead of guessing bytes from a descriptor's raw number.
					rows = append(rows, ImportDisk{ID: disk.ID, Path: disk.Path, Format: format})
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
		return nil
	}
	return fmt.Errorf("Choose an appliance from the inspected list.")
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
