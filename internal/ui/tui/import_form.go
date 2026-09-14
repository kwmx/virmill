package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/validation"
)

// ImportForm collects options only. Browsing, inspection, export and preview are
// intents handled by the workspace through its existing service boundary.
type ImportForm struct {
	VM                      *CreationForm
	VMBinding               string
	Draft                   ImportDraft
	StagingRoot             string // private default folder for prepared images
	Error                   string
	Page, Focus, Disk, File int
	cursor                  int
	cursorField             string
	advanced                bool
	errorText               string
	errorOffset             int
}

type importControl struct {
	id, label, help, kind, value string
	choices                      []string
}

func importText(id, label, help, value string) importControl {
	return importControl{id: id, label: label, help: help, kind: "text", value: value}
}
func importButton(id, label, help string) importControl {
	return importControl{id: id, label: label, help: help, kind: "button"}
}
func importPath(id, label, help, value string) importControl {
	return importControl{id: id, label: label, help: help, kind: "path", value: value}
}

func (f ImportForm) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	clean := func(s string) string {
		return ansi.Truncate(strings.NewReplacer("\n", " ", "\t", " ").Replace(validation.SafeText(s)), width, "…")
	}
	if width < 40 || height < 10 {
		return strings.Join([]string{clean("Resize to continue editing."), clean("Your options are retained.")}[:min(height, 2)], "\n")
	}
	title := map[string]string{"ova": "Import an appliance", "iso": "Prepare installation media", "disks": "Import disk images", "auto": "Import images"}[f.Draft.Kind]
	if title == "" {
		title = "Import options"
	}
	page := max(0, min(f.Page, 3))
	steps := []string{"Choose the source", "Choose where to save", "Prepare disk images", "Review the appliance"}
	purpose := []string{"Choose an image to prepare. Your original stays untouched.", "Save the prepared images in a new folder. VM setup follows.", "First prepare the images. CPU, RAM, firmware and networks come next.", "Read from appliance metadata. Files are verified during preparation."}
	if f.Draft.Kind == "ova" || f.Draft.HasSourceDescription() {
		purpose[1] = "Choose a destination. Your VM settings stay with this import."
		purpose[2] = "Review disk conversion, then confirm the VM before creation."
	}
	if page == 3 {
		if f.Draft.Kind != "ova" {
			purpose[3] = "Disk details are detected. CPU and memory are editable suggestions."
		}
		title = "Review source"
		if f.Draft.Kind == "ova" {
			title = "Review appliance"
		}
	}
	lines := []string{clean(title), ""}
	lines = append(lines, wrap(purpose[page], width)...)
	if page == 3 {
		lines = append(lines, "")
	} else {
		lines = append(lines, clean(fmt.Sprintf("Step %d of 3 · %s", page+1, steps[page])), "")
	}
	if page == 2 && (f.Draft.Kind == "ova" || f.Draft.HasSourceDescription()) {
		cpus, cpuOK := guidedNumber(f.Draft.VCPUs, 512)
		memory, memoryOK := guidedNumber(f.Draft.MemoryMiB, 1<<20)
		cpuLabel, memoryLabel := "CPU: review summary", "RAM: review summary"
		if cpuOK {
			cpuLabel = fmt.Sprintf("%d CPUs", cpus)
		}
		if memoryOK && memory >= 128 {
			memoryLabel = fmt.Sprintf("%d MiB RAM", memory)
		}
		lines = append(lines, clean("Selected VM: "+cpuLabel+" · "+memoryLabel))
	}
	controls := f.controls()
	if len(controls) == 0 {
		return strings.Join(append(lines, clean("Choose OVA, ISO or existing disks to continue.")), "\n")
	}
	focus := max(0, min(f.Focus, len(controls)-1))
	primary := -1
	back := -1
	advancedButton := -1
	body := []int{}
	for i, c := range controls {
		if c.id == "next" || c.id == "preview" {
			primary = i
		} else if page == 3 && c.id == "hardware" {
			advancedButton = i
		} else if f.Error != "" && page == 2 && c.id == "back" {
			back = i
		} else {
			body = append(body, i)
		}
	}
	// Keep the next step visible while a long disk list scrolls. Keyboard
	// guidance belongs to the workspace; this form shows only contextual help.
	footer := []string{}
	if f.Error != "" {
		message := validation.SafeText(f.Error)
		if strings.HasPrefix(message, "INSUFFICIENT_SPACE:") {
			message = "Not enough storage\n" + strings.TrimSpace(strings.TrimPrefix(message, "INSUFFICIENT_SPACE:"))
		}
		errorLines := wrap(message, width)
		reserved := 2 // primary action and help
		if advancedButton >= 0 {
			reserved++
		}
		if back >= 0 {
			reserved++
		}
		room := max(1, height-len(lines)-reserved-1)
		offset := 0
		if f.errorText == f.Error {
			offset = min(f.errorOffset, max(0, len(errorLines)-room))
		}
		footer = append(footer, errorLines[offset:min(len(errorLines), offset+room)]...)
	}
	if back >= 0 {
		prefix := "  "
		if focus == back {
			prefix = "> "
		}
		footer = append(footer, clean(prefix+"[ "+controls[back].label+" ]"))
	}
	if advancedButton >= 0 {
		prefix := "  "
		if focus == advancedButton {
			prefix = "> "
		}
		footer = append(footer, clean(prefix+"[ "+controls[advancedButton].label+" ]"))
	}
	if primary >= 0 {
		prefix := "  "
		if focus == primary {
			prefix = "> "
		}
		footer = append(footer, clean(prefix+"[ "+controls[primary].label+" ]"))
	}
	help := controls[focus].help
	if f.Error != "" {
		help = "PgUp/PgDn: read message · Esc: back"
	}
	footer = append(footer, clean(help))
	room := max(1, height-len(lines)-len(footer))
	bodyFocus := slices.Index(body, focus)
	first := max(0, bodyFocus-room+1)
	if bodyFocus < 0 {
		first = max(0, len(body)-room)
	}
	if first > 0 {
		lines[len(lines)-1] = clean(fmt.Sprintf("%d earlier options", first))
	}
	if page == 3 {
		below := max(0, len(body)-first-room)
		if below > 0 {
			lines[len(lines)-1] = clean(fmt.Sprintf("%d rows below · PgDn reads more", below))
			if first > 0 {
				lines[len(lines)-1] = clean(fmt.Sprintf("%d above · %d below · PgUp/PgDn reads more", first, below))
			}
		} else if first > 0 {
			lines[len(lines)-1] = clean(fmt.Sprintf("%d rows above · PgUp reads more", first))
		}
	}
	for row := first; row < min(len(body), first+room); row++ {
		i := body[row]
		c := controls[i]
		prefix := "  "
		if i == focus {
			prefix = "> "
		}
		value := strings.NewReplacer("\n", " ", "\t", " ").Replace(validation.SafeText(c.value))
		var row string
		switch c.kind {
		case "button":
			row = "[ " + c.label + " ]"
		case "toggle":
			mark := " "
			if c.value == "true" {
				mark = "x"
			}
			row = "[" + mark + "] " + c.label
		case "choice":
			row = c.label + ": < " + value + " >"
		case "path":
			if value == "" {
				value = "Choose…"
			}
			value = importTail(value, max(1, width-ansi.StringWidth(c.label)-8))
			row = c.label + ": [ " + value + " ]"
		case "summary":
			row = value
		case "info":
			row = c.label + ": " + importTail(value, max(1, width-ansi.StringWidth(c.label)-4))
		default:
			if i == focus {
				runes := []rune(value)
				cursor := len(runes)
				if f.cursorField == c.id {
					cursor = max(0, min(f.cursor, len(runes)))
				}
				before := string(runes[:cursor])
				available := max(2, width-ansi.StringWidth(c.label)-8)
				before = importTail(before, available-1)
				value = before + "|" + string(runes[cursor:])
			}
			row = c.label + ": [" + value + "]"
		}
		lines = append(lines, clean(prefix+row))
	}
	lines = append(lines, footer...)
	return strings.Join(lines[:min(height, len(lines))], "\n")
}

// TruncateLeft removes columns; it does not impose a maximum width. Keep short
// values intact and remove only the overflow, including room for the marker.
func importTail(value string, width int) string {
	if ansi.StringWidth(value) <= width {
		return value
	}
	return ansi.TruncateLeft(value, ansi.StringWidth(value)-width+1, "…")
}

func (f *ImportForm) edit(c importControl, key tea.KeyMsg) {
	runes := []rune(c.value)
	if f.cursorField != c.id {
		f.cursor = len(runes)
		f.cursorField = c.id
	}
	f.cursor = max(0, min(f.cursor, len(runes)))
	value := c.value
	switch key.Type {
	case tea.KeyLeft:
		f.cursor = max(0, f.cursor-1)
	case tea.KeyRight:
		f.cursor = min(len(runes), f.cursor+1)
	case tea.KeyHome:
		f.cursor = 0
	case tea.KeyEnd:
		f.cursor = len(runes)
	case tea.KeyCtrlU:
		value = ""
		f.cursor = 0
	case tea.KeyBackspace:
		if f.cursor > 0 {
			value = string(runes[:f.cursor-1]) + string(runes[f.cursor:])
			f.cursor--
		}
	case tea.KeyDelete:
		if f.cursor < len(runes) {
			value = string(runes[:f.cursor]) + string(runes[f.cursor+1:])
		}
	case tea.KeyRunes, tea.KeySpace:
		text := string(key.Runes)
		if key.Type == tea.KeySpace {
			text = " "
		}
		if key.Alt || !guidedPrintable(text) {
			f.Error = "Use printable text."
			return
		}
		limit := 4096
		if c.id == "size" || c.id == "vcpus" || c.id == "memoryMiB" {
			limit = 12
		}
		if c.id == "diskID" || c.id == "mediaID" || c.id == "folder" || c.id == "vmName" {
			limit = 255
		}
		if len(value)+len(text) > limit {
			f.Error = "This value is too long."
			return
		}
		value = string(runes[:f.cursor]) + text + string(runes[f.cursor:])
		f.cursor += utf8.RuneCountInString(text)
	}
	if value != c.value {
		f.setText(c.id, value)
		f.Error = ""
	}
}

func importCycle(value string, options []string, direction int) string {
	if len(options) == 0 {
		return value
	}
	i := slices.Index(options, value)
	if i < 0 {
		if direction < 0 {
			return options[len(options)-1]
		}
		return options[0]
	}
	return options[(i+direction+len(options))%len(options)]
}

func NewImportForm(kind string) ImportForm {
	f := ImportForm{Draft: ImportDraft{Kind: kind}}
	if kind == "iso" {
		f.Draft.MediaID = "installer"
		f.Draft.Disks = []ImportDisk{{ID: "disk1", SizeMiB: "32768"}}
	}
	return f
}

func (f ImportForm) controls() []importControl {
	if !slices.Contains([]string{"auto", "ova", "iso", "disks"}, f.Draft.Kind) {
		return nil
	}
	d := f.Draft
	var controls []importControl
	switch f.Page {
	case 0:
		label, help := "OVA file", "Choose an appliance archive. Original files stay untouched."
		if d.Kind == "iso" {
			label, help = "ISO file", "Choose installation media. This prepares disks; it does not install the OS."
		}
		if d.Kind == "disks" {
			label, help = "Source folder", "Choose the folder containing your disks and any backing files."
		}
		source := d.SelectedSource
		if source == "" {
			source = d.Source
		}
		if d.Kind == "auto" || d.SelectedSource != "" {
			label, help = "File or folder", "Choose an appliance, ISO, disk image or image folder."
		}
		controls = append(controls, importPath("source", label, help, source))
		if d.Kind == "ova" {
			if d.Report != nil && d.Report.Source == d.Source {
				controls = append(controls, importButton("inspect", "Recheck source", "Read this archive again if its contents have changed."))
				choices := []string{}
				for _, sys := range d.Report.Systems {
					choices = append(choices, sys.ID)
				}
				value := d.SystemID
				for _, system := range d.Report.Systems {
					if system.ID == d.SystemID && system.Name != "" && system.Name != system.ID {
						value = system.Name + " (" + system.ID + ")"
					}
				}
				if value == "" {
					value = "Choose an appliance"
				}
				controls = append(controls, importControl{id: "system", label: "Appliance", kind: "choice", value: value, choices: choices, help: "Left/Right selects a system. Its complete disk list is included."})
			}
		}
		if d.Kind != "ova" && d.HasSourceDescription() {
			controls = append(controls, importButton("inspect", "Recheck source", "Read this source again if its contents changed."))
		}
		if d.Kind == "iso" {
			controls = append(controls, importText("mediaID", "Media name", "A short name identifying the installer in the prepared image.", d.MediaID))
			controls = append(controls, importButton("advanced", "Advanced verification", "Optional: compare the ISO against a publisher's SHA-256 checksum."))
			if f.advanced {
				controls = append(controls, importText("sha256", "Expected SHA-256", "Optional publisher checksum; leave blank if unavailable.", d.SHA256))
			}
		}
		nextHelp := "Read the source type and sizes, then review its VM settings."
		if d.Kind == "ova" && (d.Report == nil || d.Report.Source != d.Source) {
			nextHelp = "Check the appliance, then choose where to save its copy."
		}
		controls = append(controls, importButton("next", "Continue", nextHelp))
	case 3:
		return f.sourceControls()
	case 1:
		backLabel := "Back: Source"
		if f.Draft.Kind == "ova" && f.Draft.Report != nil && f.Draft.SystemID != "" {
			backLabel = "Back: Appliance"
		} else if f.Draft.HasSourceDescription() {
			backLabel = "Back: Source summary"
		}
		controls = append(controls,
			importPath("destination", "Save in", "Choose an existing parent folder with enough free space.", d.DestinationParent),
			importText("folder", "New folder name", "A new folder for this import. Existing files are never overwritten.", d.DestinationName),
			importButton("back", backLabel, "Return to the appliance or source without losing these choices."),
			importButton("next", "Continue", "Set disk sizes and review which files will be copied."))
	case 2:
		if d.DestinationParent != "" && d.DestinationName != "" {
			controls = append(controls, importControl{id: "saveInfo", label: "Saved in", kind: "info", value: importDestination(d), help: "Prepared copies go in this new folder. Back: Destination changes it."})
		}
		if len(d.Disks) > 0 {
			index := max(0, min(f.Disk, len(d.Disks)-1))
			disk := d.Disks[index]
			choices := []string{}
			for i, row := range d.Disks {
				choices = append(choices, fmt.Sprintf("%d/%d  %s", i+1, len(d.Disks), row.ID))
			}
			controls = append(controls, importControl{id: "disk", label: "Disk", kind: "choice", value: choices[index], choices: choices, help: "Left/Right switches disks. Each disk keeps its own options."})
			if d.Kind == "ova" {
				controls = append(controls, importControl{id: "diskInfo", label: "Source", value: disk.Path, kind: "info", help: "The inspected source and disk ID are preserved for this appliance."})
			} else {
				controls = append(controls, importText("diskID", "Disk name", "A unique short name; letters, numbers, dots, dashes and underscores.", disk.ID))
			}
			if d.Kind == "disks" {
				controls = append(controls, importPath("diskPath", "Disk file", "Choose the top-level disk file within the source folder.", disk.Path))
			}
			label, help := "Size (MiB)", "Blank disk capacity: 32768 MiB = 32 GiB. It is not allocated in full now."
			if d.Kind != "iso" {
				label, help = "Maximum size (MiB)", "Safety limit for the source disk's virtual capacity. This does not resize it."
			}
			controls = append(controls, importText("size", label, help, disk.SizeMiB))
			if d.Kind == "ova" && disk.Format != "" {
				controls = append(controls, importButton("advanced", "Advanced disk options", "Review or correct the source format declared by this appliance."))
			}
			if d.Kind != "iso" && (d.Kind != "ova" || disk.Format == "" || f.advanced) {
				controls = append(controls, importControl{id: "format", label: "Source format", kind: "choice", value: disk.Format, choices: []string{"qcow2", "raw", "vmdk", "vdi", "vpc", "vhdx"}, help: "Left/Right chooses the existing format. VPC means VHD; this is not auto-detection."})
			}
		}
		if d.Kind != "ova" {
			label := "Add blank disk"
			if d.Kind == "disks" {
				label = "Add disk file"
			}
			controls = append(controls, importButton("addDisk", label, "Add another disk to this import."))
			if len(d.Disks) > 0 {
				controls = append(controls, importButton("removeDisk", "Remove selected disk", "Remove this disk from the draft. No source file is deleted."))
			}
		}
		if d.Kind == "disks" {
			controls = append(controls, importButton("addBacking", "Add backing / extent file", "Include files needed by the disk chain, without creating extra guest disks."))
			if len(d.Files) > 0 {
				index := max(0, min(f.File, len(d.Files)-1))
				choices := []string{}
				for i, file := range d.Files {
					choices = append(choices, fmt.Sprintf("%d/%d  %s", i+1, len(d.Files), file.Path))
				}
				controls = append(controls, importControl{id: "file", label: "Included file", kind: "choice", value: choices[index], choices: choices, help: "Left/Right checks every source file included in the copy."})
				controls = append(controls, importButton("fileOptions", "File verification / removal", "Optional checksum or remove an unneeded backing file from the draft."))
				if f.advanced {
					controls = append(controls, importText("fileSHA256", "Expected SHA-256", "Optional publisher checksum for this selected source file.", d.Files[index].SHA256), importButton("removeFile", "Remove included file", "A disk's root file cannot be removed while its disk is selected for import."))
				}
			}
		}
		if d.Kind != "ova" {
			controls = append(controls, importControl{id: "offline", label: "Source images are not in use", kind: "toggle", value: fmt.Sprint(d.Offline), help: "Space toggles. Stop any VM or program using these source images first."})
		}
		controls = append(controls, importButton("hardware", "CPU, RAM and VM settings", "Set up the VM before copying images. Preparation and creation each have a review."), importButton("back", "Back: Destination", "Choose another folder with enough space for the prepared images."), importButton("export", "Export settings", "Save these image-preparation options for reuse; no import starts."), importButton("preview", "Preview image preparation", "Review image copies and storage needs. Create and configure the VM afterward."))
	}
	return controls
}

func (f ImportForm) Update(key tea.KeyMsg) (ImportForm, ImportIntent) {
	f.Draft.Disks = slices.Clone(f.Draft.Disks)
	f.Draft.Files = slices.Clone(f.Draft.Files)
	none := ImportIntent{}
	if f.errorText != f.Error {
		f.errorText, f.errorOffset = f.Error, 0
	}
	if f.Error != "" {
		if key.Type == tea.KeyPgDown {
			f.errorOffset++
			return f, none
		}
		if key.Type == tea.KeyPgUp {
			f.errorOffset = max(0, f.errorOffset-1)
			return f, none
		}
	}
	if key.Type == tea.KeyEsc {
		if f.Page > 0 {
			f.Page = f.previousPage()
			f.Focus = 0
			f.Error = ""
			return f, none
		}
		return f, ImportIntent{Kind: "cancel"}
	}
	controls := f.controls()
	if len(controls) == 0 {
		f.Error = "Choose a supported source type."
		return f, none
	}
	f.Focus = max(0, min(f.Focus, len(controls)-1))
	if f.Page == 3 {
		if key.Type == tea.KeyTab || key.Type == tea.KeyShiftTab {
			direction := 1
			if key.Type == tea.KeyShiftTab {
				direction = -1
			}
			for range controls {
				f.Focus = (f.Focus + direction + len(controls)) % len(controls)
				if controls[f.Focus].kind != "summary" {
					break
				}
			}
			return f, none
		}
		if key.Type == tea.KeyPgDown || key.Type == tea.KeyPgUp {
			direction := 8
			if key.Type == tea.KeyPgUp {
				direction = -8
			}
			f.Focus = max(0, min(len(controls)-1, f.Focus+direction))
			return f, none
		}
	}
	switch key.Type {
	case tea.KeyTab, tea.KeyDown:
		f.Focus = (f.Focus + 1) % len(controls)
		return f, none
	case tea.KeyShiftTab, tea.KeyUp:
		f.Focus = (f.Focus + len(controls) - 1) % len(controls)
		return f, none
	}
	c := controls[f.Focus]
	if key.Type == tea.KeyCtrlO && c.kind == "path" {
		return f, f.browse(c.id)
	}
	activate := key.Type == tea.KeyEnter || key.Type == tea.KeySpace
	if c.kind == "choice" && (key.Type == tea.KeyLeft || key.Type == tea.KeyRight || activate) {
		direction := 1
		if key.Type == tea.KeyLeft {
			direction = -1
		}
		switch c.id {
		case "disk":
			f.Disk = (max(0, min(f.Disk, len(f.Draft.Disks)-1)) + direction + len(f.Draft.Disks)) % len(f.Draft.Disks)
		case "file":
			f.File = (max(0, min(f.File, len(f.Draft.Files)-1)) + direction + len(f.Draft.Files)) % len(f.Draft.Files)
		case "system":
			id := importCycle(f.Draft.SystemID, c.choices, direction)
			if id != f.Draft.SystemID {
				if err := f.Draft.SelectSystem(id); err != nil {
					f.Error = err.Error()
				} else {
					f.Disk = 0
					f.Error = ""
				}
			}
		case "format":
			f.Draft.Disks[max(0, min(f.Disk, len(f.Draft.Disks)-1))].Format = importCycle(c.value, c.choices, direction)
		}
		f.cursorField = ""
		return f, none
	}
	if c.kind == "toggle" && activate {
		f.Draft.Offline = !f.Draft.Offline
		f.Error = ""
		return f, none
	}
	if c.kind == "path" && key.Type == tea.KeyEnter {
		return f, f.browse(c.id)
	}
	if c.kind == "button" && activate {
		switch c.id {
		case "back":
			f.Page = f.previousPage()
			f.Focus = 0
			f.Error = ""
		case "next":
			if f.Page == 3 {
				field, err := f.Draft.SummaryValidation()
				if err != nil {
					f.Error = err.Error()
					for i, c := range controls {
						if c.id == field {
							f.Focus = i
						}
					}
					return f, none
				}
				f.Page = 1
				f.Focus = 0
				f.Error = ""
				// A private default folder skips this step; Back: Destination reaches it.
				if f.Draft.DestinationParent == "" && f.Draft.DestinationName == "" && f.StagingRoot != "" {
					f.Draft.DestinationParent, f.Draft.DestinationName = f.StagingRoot, defaultImportFolder(f.Draft)
					f.Page = 2
				}
				return f, none
			}
			if f.Page == 0 {
				selected := f.Draft.SelectedSource
				if selected == "" {
					selected = f.Draft.Source
				}
				if !guidedPath(selected) {
					f.Error = map[string]string{"ova": "Choose an OVA file to continue.", "iso": "Choose an ISO file to continue.", "disks": "Choose a disk image or folder.", "auto": "Choose a file or folder to continue."}[f.Draft.Kind]
					f.Focus = 0
					return f, none
				}
				if f.Draft.Kind != "ova" {
					if !f.Draft.HasSourceDescription() {
						f.Error = ""
						return f, ImportIntent{Kind: "inspect"}
					}
					f.Page = 3
					f.Focus = 0
					f.Error = ""
					return f, none
				}
				if f.Draft.Kind == "ova" {
					if f.Draft.Report == nil || f.Draft.Report.Source != f.Draft.Source {
						f.Draft.Report = nil
						f.Draft.SystemID = ""
						f.Draft.Disks = nil
						f.Error = ""
						return f, ImportIntent{Kind: "inspect"}
					}
					selected := false
					for _, system := range f.Draft.Report.Systems {
						selected = selected || system.ID == f.Draft.SystemID
					}
					if !selected {
						f.Error = "Choose which appliance to import."
						for i, option := range controls {
							if option.id == "system" {
								f.Focus = i
							}
						}
						return f, none
					}
					f.Page = 3
					f.Focus = 0
					f.Error = ""
					return f, none
				}
			}
			if f.Page == 1 {
				if !guidedPath(f.Draft.DestinationParent) {
					f.Error = "Choose a folder using Save in."
					f.Focus = 0
					return f, none
				}
				if f.Draft.DestinationName == "" {
					f.Error = "Enter a name for the new folder."
					f.Focus = 1
					return f, none
				}
			}
			f.Page = min(2, f.Page+1)
			f.Focus = 0
			f.Error = ""
		case "inspect":
			return f, ImportIntent{Kind: "inspect"}
		case "preview", "export", "hardware":
			return f, ImportIntent{Kind: c.id}
		case "advanced", "fileOptions":
			f.advanced = !f.advanced
		case "addDisk":
			if len(f.Draft.Disks) >= 64 {
				f.Error = "An import supports up to 64 disks."
				return f, none
			}
			id := f.nextDiskID()
			disk := ImportDisk{ID: id}
			if f.Draft.Kind == "iso" {
				disk.SizeMiB = "32768"
			}
			f.Draft.Disks = append(f.Draft.Disks, disk)
			f.Disk = len(f.Draft.Disks) - 1
			f.Focus = 0
			if f.Draft.Kind == "disks" {
				return f, ImportIntent{Kind: "browse", Target: "disk", Index: f.Disk}
			}
		case "removeDisk":
			i := max(0, min(f.Disk, len(f.Draft.Disks)-1))
			f.Draft.Disks = slices.Delete(f.Draft.Disks, i, i+1)
			f.Disk = max(0, min(i, len(f.Draft.Disks)-1))
			f.Focus = 0
		case "addBacking":
			return f, ImportIntent{Kind: "browse", Target: "backing"}
		case "removeFile":
			i := max(0, min(f.File, len(f.Draft.Files)-1))
			for _, disk := range f.Draft.Disks {
				if disk.Path == f.Draft.Files[i].Path {
					f.Error = "Remove the corresponding disk before removing its root file."
					return f, none
				}
			}
			f.Draft.Files = slices.Delete(f.Draft.Files, i, i+1)
			f.File = max(0, min(i, len(f.Draft.Files)-1))
			f.Focus = 0
		}
		f.cursorField = ""
		return f, none
	}
	if c.kind == "text" || c.kind == "path" {
		f.edit(c, key)
	}
	return f, none
}

func (f ImportForm) browse(id string) ImportIntent {
	target := id
	if id == "diskPath" {
		target = "disk"
	}
	return ImportIntent{Kind: "browse", Target: target, Index: f.Disk}
}

func (f ImportForm) nextDiskID() string {
	for i := 1; ; i++ {
		id := fmt.Sprintf("disk%d", i)
		if !slices.ContainsFunc(f.Draft.Disks, func(d ImportDisk) bool { return d.ID == id }) {
			return id
		}
	}
}

func (f *ImportForm) setText(id, value string) {
	switch id {
	case "source":
		f.Draft.Source = value
		f.Draft.SelectedSource = value
		f.Draft.Description = nil
		f.Draft.VMName, f.Draft.VCPUs, f.Draft.MemoryMiB = "", "", ""
		f.Draft.selectionSource, f.Draft.selectionSystem = "", ""
		f.Draft.Report = nil
		f.Draft.SystemID = ""
		if f.Draft.Kind != "iso" {
			f.Draft.Disks = nil
			f.Draft.Files = nil
			f.Disk = 0
			f.File = 0
		}
		f.Draft.SHA256 = ""
		f.Draft.Offline = false
	case "vmName":
		f.Draft.VMName = value
	case "vcpus":
		f.Draft.VCPUs = value
	case "memoryMiB":
		f.Draft.MemoryMiB = value
	case "destination":
		f.Draft.DestinationParent = value
	case "folder":
		f.Draft.DestinationName = value
	case "mediaID":
		f.Draft.MediaID = value
	case "sha256":
		f.Draft.SHA256 = value
	case "fileSHA256":
		if len(f.Draft.Files) > 0 {
			f.Draft.Files[max(0, min(f.File, len(f.Draft.Files)-1))].SHA256 = value
		}
	case "diskID", "size", "diskPath":
		if len(f.Draft.Disks) == 0 {
			return
		}
		d := &f.Draft.Disks[max(0, min(f.Disk, len(f.Draft.Disks)-1))]
		switch id {
		case "diskID":
			d.ID = value
		case "size":
			d.SizeMiB = value
		case "diskPath":
			d.Path = value
			f.Draft.Offline = false
		}
	}
}

func (f ImportForm) previousPage() int {
	if f.Page == 3 {
		return 0
	}
	if f.Page == 1 && f.Draft.HasSourceDescription() {
		return 3
	}
	if f.Page == 1 && f.Draft.Kind == "ova" && f.Draft.Report != nil && f.Draft.Report.Source == f.Draft.Source && f.Draft.SystemID != "" {
		return 3
	}
	return max(0, f.Page-1)
}
