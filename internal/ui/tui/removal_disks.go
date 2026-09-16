package tui

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"virmill.local/core/internal/validation"
)

// XML supplies presentation choices only. The shared service re-inspects exact
// native volumes, generations and references before any deletion is authorized.
func removalDiskFields(raw string) []GuidedField {
	if len(raw) > 1<<20 {
		return nil
	}
	type source struct {
		File   string `xml:"file,attr"`
		Pool   string `xml:"pool,attr"`
		Volume string `xml:"volume,attr"`
	}
	type backing struct {
		Source source   `xml:"source"`
		Next   *backing `xml:"backingStore"`
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
				Backing   *backing  `xml:"backingStore"`
			} `xml:"disk"`
		} `xml:"devices"`
	}
	if xml.Unmarshal([]byte(raw), &doc) != nil || len(doc.Devices.Disks) > 64 {
		return nil
	}
	// One key per declared image: a file path, or a pool volume as the VMs
	// Virmill creates use.
	key := func(s source) string {
		if s.File != "" {
			return "file:" + s.File
		}
		if s.Pool != "" && s.Volume != "" {
			return "volume:" + s.Pool + "/" + s.Volume
		}
		return ""
	}
	parents, images, targets := map[string]bool{}, map[string]int{}, map[string]int{}
	for _, d := range doc.Devices.Disks {
		images[key(d.Source)]++
		targets[d.Target.Dev]++
		for b, n := d.Backing, 0; b != nil; b, n = b.Next, n+1 {
			if n > 64 {
				return nil
			}
			parents[key(b.Source)] = true
		}
	}
	out := []GuidedField{}
	for _, d := range doc.Devices.Disks {
		id := key(d.Source)
		hint := d.Source.File
		if d.Type == "volume" {
			hint = d.Source.Pool + "/" + d.Source.Volume
		}
		if d.Device != "disk" || (d.Type != "file" && d.Type != "volume") || id == "" || d.ReadOnly != nil || d.Shareable != nil || parents[id] || images[id] != 1 || targets[d.Target.Dev] != 1 || !guidedRootID.MatchString(d.Target.Dev) || !guidedPrintable(hint) {
			continue
		}
		out = append(out, GuidedField{Name: "delete:" + d.Target.Dev, Label: d.Target.Dev, Hint: hint, Value: "false", Limit: 5, Toggle: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (f GuidedForm) removalDiskView(width, height int) string {
	selected := 0
	for _, d := range f.Fields[1:] {
		if d.Value == "true" {
			selected++
		}
	}
	header := []string{"Remove VM", "VM: " + validation.SafeText(f.VM.Name)}
	header = append(header, wrap(f.note(), width)...)
	header = append(header, fmt.Sprintf("%d of %d disks selected for deletion. Backups are kept.", selected, len(f.Fields)-1))
	rows := []string{}
	focusLine := 0
	for i, field := range f.Fields {
		mark := "  "
		if i == f.Focus {
			mark = "> "
			focusLine = len(rows)
		}
		if i == 0 {
			rows = append(rows, wrap(mark+"Type VM name: ["+validation.SafeText(field.Value)+"]", width)...)
			continue
		}
		state := "[ ] Keep"
		if field.Value == "true" {
			state = "[x] DELETE"
		}
		rows = append(rows, mark+state+" "+field.Label)
		rows = append(rows, wrap("    "+validation.SafeText(field.Hint), width)...)
	}
	mark := "  "
	if f.Focus == len(f.Fields) {
		mark = "> "
		focusLine = len(rows)
	}
	rows = append(rows, mark+"[ Preview ]")
	footer := []string{"Tab/Arrows Select  Space Toggle  Enter Choose  Esc Back"}
	if selected > 0 {
		footer = append([]string{"Permanent deletion. Review exact files before applying."}, footer...)
	}
	if f.Error != "" {
		footer = append([]string{"Issue: " + validation.SafeText(f.Error), "F1 Read the full issue"}, footer...)
	}
	room := max(1, height-len(header)-len(footer))
	offset := max(0, focusLine-room+1)
	lines := append(header, pageLines(rows, width, room, offset)...)
	lines = append(lines, footer...)
	return strings.Join(pageLines(lines, width, height, 0), "\n")
}
