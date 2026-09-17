package tui

import (
	"encoding/xml"
	"sort"
	"unicode/utf8"
)

// movableDiskChoices lists the disks a move can take: writable data disks that
// already live in a storage pool. A plain file disk has no pool to move from,
// so it is left out here rather than refused later. The service repeats every
// safety check; this only fills the form.
func movableDiskChoices(raw string) []string {
	if len(raw) > 1<<20 {
		return nil
	}
	var doc struct {
		Devices struct {
			Disks []struct {
				Device string `xml:"device,attr"`
				Type   string `xml:"type,attr"`
				Target struct {
					Dev string `xml:"dev,attr"`
				} `xml:"target"`
				Source struct {
					Pool   string `xml:"pool,attr"`
					Volume string `xml:"volume,attr"`
				} `xml:"source"`
				Driver struct {
					Type string `xml:"type,attr"`
				} `xml:"driver"`
				ReadOnly     *struct{} `xml:"readonly"`
				Shareable    *struct{} `xml:"shareable"`
				BackingStore *struct{} `xml:"backingStore"`
			} `xml:"disk"`
		} `xml:"devices"`
	}
	if xml.Unmarshal([]byte(raw), &doc) != nil || len(doc.Devices.Disks) > 64 {
		return nil
	}
	out := []string{}
	for _, d := range doc.Devices.Disks {
		if d.Device != "disk" || d.Type != "volume" || d.Source.Pool == "" || d.Source.Volume == "" {
			continue
		}
		if d.ReadOnly != nil || d.Shareable != nil || d.BackingStore != nil || d.Driver.Type != "qcow2" {
			continue
		}
		if guidedRootID.MatchString(d.Target.Dev) {
			out = append(out, d.Target.Dev)
		}
	}
	sort.Strings(out)
	return out
}

// cloneNameSuggestion is the original's name with "-clone", kept within the
// display-name limit.
func cloneNameSuggestion(name string) string {
	const suffix = "-clone"
	for utf8.RuneCountInString(name)+len(suffix) > 128 {
		r := []rune(name)
		name = string(r[:len(r)-1])
	}
	return name + suffix
}
