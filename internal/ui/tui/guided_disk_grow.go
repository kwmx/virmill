package tui

import (
	"encoding/xml"
	"sort"
)

// growDiskChoices lists a VM's writable file and pool-volume disks by target.
// The service repeats every safety check; this only fills the form.
func growDiskChoices(raw string) []string {
	if len(raw) > 1<<20 {
		return nil
	}
	type source struct {
		File   string `xml:"file,attr"`
		Pool   string `xml:"pool,attr"`
		Volume string `xml:"volume,attr"`
	}
	var doc struct {
		Devices struct {
			Disks []struct {
				Device string `xml:"device,attr"`
				Type   string `xml:"type,attr"`
				Target struct {
					Dev string `xml:"dev,attr"`
				} `xml:"target"`
				Source    source    `xml:"source"`
				ReadOnly  *struct{} `xml:"readonly"`
				Shareable *struct{} `xml:"shareable"`
			} `xml:"disk"`
		} `xml:"devices"`
	}
	if xml.Unmarshal([]byte(raw), &doc) != nil || len(doc.Devices.Disks) > 64 {
		return nil
	}
	out := []string{}
	for _, d := range doc.Devices.Disks {
		declared := d.Type == "file" && d.Source.File != "" || d.Type == "volume" && d.Source.Pool != "" && d.Source.Volume != ""
		if d.Device == "disk" && declared && d.ReadOnly == nil && d.Shareable == nil && guidedRootID.MatchString(d.Target.Dev) {
			out = append(out, d.Target.Dev)
		}
	}
	sort.Strings(out)
	return out
}
