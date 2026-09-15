package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
)

// Task groups are presentation only; request construction stays in the shared
// registry and forms. All registered actions remain reachable through All tools.
func actionGroup(a ui.Action) (int, string) {
	c := a.Command
	switch {
	case c == "vm remove":
		return 70, "Remove"
	case strings.HasPrefix(c, "vm creation "), strings.HasPrefix(c, "network creation "), c == "operation reconcile":
		return 80, "Recovery and troubleshooting"
	case c == "vm start" || c == "vm stop" || c == forceOff.Command || c == "vm reboot" || c == "vm pause" || c == "vm resume" || c == "vm save" || c == "vm restore-saved":
		return 10, "Power"
	case strings.HasPrefix(c, "import ") || c == "vm create":
		return 30, "Create and import"
	case strings.HasPrefix(c, "vm guest-agent ") || c == "vm console show" || c == "vm set" || c == "vm boot set" || c == "vm autostart" || strings.HasPrefix(c, "guest recipe ") || strings.HasPrefix(c, "guest tools "):
		return 20, "Configure and guest setup"
	case strings.HasPrefix(c, "snapshot "):
		return 10, "Capture and restore"
	case strings.HasPrefix(c, "backup repository "):
		return 30, "Backup repositories"
	case strings.HasPrefix(c, "backup policy "):
		return 40, "Backup policies"
	case strings.HasPrefix(c, "backup "):
		return 20, "Back up and recover"
	case strings.HasPrefix(c, "vm recovery "):
		return 80, "Recovery and troubleshooting"
	case strings.HasPrefix(c, "plugin permissions "):
		return 30, "Permissions"
	case c == "plugin new" || c == "plugin pack" || c == "plugin validate" || c == "plugin test":
		return 80, "Plugin development"
	case c == "plugin install" || c == "plugin update" || c == "plugin enable" || c == "plugin disable" || c == "plugin rollback" || c == "plugin remove":
		return 10, "Manage plugins"
	case strings.HasPrefix(c, "storage access "):
		return 20, "Volume access"
	case c == "network create" || c == "network cidr check":
		return 10, "Network setup"
	case c == "storage pool create" || c == "storage pool start":
		return 10, "Storage setup"
	case c == "operation cancel" || c == "operation watch":
		return 10, "Manage job"
	case strings.HasSuffix(c, "validate"):
		return 20, "Validate documents"
	default:
		return 90, "Inspect"
	}
}

// The short menu is intentionally curated. Advanced holds the rest of the
// section's tasks, and All tools and search keep the full shared registry,
// including specialist recovery and development actions.
func (m Workspace) commonAction(a ui.Action) bool {
	if m.CatalogMode == "import" {
		return a.Command == "import prepare" || a.Command == "import prepare-install" || a.Command == "import prepare-disks"
	}
	switch a.Command {
	case "vm create":
		return m.selectedVM().Key.UUID == ""
	case "vm start":
		return m.selectedVM().State != "running" && m.selectedVM().State != "paused"
	case "guest tools install":
		return true
	case "vm stop", "vm reboot", "guest recipe run":
		return m.selectedVM().State == "running" || m.selectedVM().Key.UUID == ""
	case "vm resume":
		return m.selectedVM().State == "paused"
	case "vm remove":
		return m.selectedVM().State == "stopped" || m.selectedVM().State == "shut off"
	case "vm console show", "vm set", "vm boot set", "vm autostart", "host inspect", "network create", "network show", "network cidr check", "storage pool show", "storage pool create", "storage pool start", "lab validate", "snapshot show", "snapshot restore", "backup create", "backup restore", "backup receipts", "backup repository init", "backup repository check", "device usb list", "host pci list", "operation show", "operation watch", "operation cancel", "plugin install", "plugin show", "plugin enable", "plugin disable", "host capabilities", "config validate":
		return true
	}
	return false
}

// powerFits hides power tasks the selected VM's state cannot use. Unknown
// states keep every task so nothing becomes unreachable.
func (m Workspace) powerFits(a ui.Action) bool {
	vm := m.selectedVM()
	stopped := vm.State == "stopped" || vm.State == "shut off"
	if vm.Key.UUID == "" || !(stopped || vm.State == "running" || vm.State == "paused") {
		return true
	}
	switch a.Command {
	case "vm start":
		return stopped
	case "vm stop", "vm reboot", "vm pause":
		return vm.State == "running"
	case "vm resume":
		return vm.State == "paused"
	case forceOff.Command:
		return vm.State == "running" || vm.State == "paused"
	case "vm save":
		// The service saves only a running VM; resume a paused one first.
		return vm.State == "running"
	case "vm restore-saved":
		return stopped && vm.HasManagedSave
	}
	return true
}
func (m Workspace) catalogHasAdvanced() bool {
	return m.CatalogSection >= 0 && m.CatalogMode != "all" && m.CatalogMode != "import" && !m.CatalogExpert && m.CatalogSearch == ""
}
func (m Workspace) catalogCount() int {
	n := len(m.catalog())
	if m.catalogHasAdvanced() {
		n++
	}
	return n
}

// forceOff is the TUI entry for the CLI's `vm stop --hard`: the same vm.plan
// request with the hard-stop action, whose review asks for its own data-loss
// acknowledgement. It is not a separate registry command.
var forceOff = ui.Action{Command: "vm stop --hard", Method: "vm.plan", Section: "VMs", Summary: "Plan abrupt power-off, requiring data-loss acknowledgement", Argument: "id", Mutation: "hard-stop"}

func (m Workspace) catalog() []ui.Action {
	out := []ui.Action{}
	scoped := m.CatalogSection >= 0 && m.CatalogMode != "all" && m.CatalogSearch == ""
	for _, a := range append(slices.Clip(ui.Actions), forceOff) {
		if scoped && !m.CatalogExpert && !m.commonAction(a) {
			continue
		}
		// Advanced lists what More does not, so no task appears twice.
		if scoped && m.CatalogExpert && (m.commonAction(a) || !m.powerFits(a)) {
			continue
		}
		if m.CatalogMode == "import" && !strings.HasPrefix(a.Command, "import ") {
			continue
		}
		if m.CatalogSection >= 0 && a.Section != sections[m.CatalogSection] && !(m.CatalogMode != "all" && m.CatalogSection == 0 && a.Section == "VMs") {
			continue
		}
		haystack := actionLabel(a) + " " + actionDescription(a) + " " + a.Command + " " + a.Section
		if m.CatalogSearch != "" && !strings.Contains(strings.ToLower(haystack), strings.ToLower(m.CatalogSearch)) {
			continue
		}
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if m.CatalogSection < 0 && out[i].Section != out[j].Section {
			return sectionOrder(out[i].Section) < sectionOrder(out[j].Section)
		}
		gi, _ := actionGroup(out[i])
		gj, _ := actionGroup(out[j])
		if gi != gj {
			return gi < gj
		}
		// Power tasks follow their familiar operational order, not alphabetic CLI order.
		priority := map[string]int{"vm start": 1, "vm stop": 2, "vm reboot": 3, "vm pause": 4, "vm resume": 5, "vm save": 6, "vm restore-saved": 7, forceOff.Command: 8,
			"import prepare": 1, "import prepare-install": 2, "import prepare-disks": 3, "import inspect": 4, "import verify": 5, "import result": 6}
		if priority[out[i].Command] != priority[out[j].Command] {
			return priority[out[i].Command] < priority[out[j].Command]
		}
		return actionLabel(out[i]) < actionLabel(out[j])
	})
	return out
}
func sectionOrder(section string) int {
	for i, s := range sections {
		if s == section {
			return i
		}
	}
	return len(sections)
}
func (m Workspace) catalogLines(width, height int) []string {
	title := "All tools"
	if m.CatalogSection >= 0 {
		title = sections[m.CatalogSection] + " / More tasks"
	}
	if m.CatalogMode == "all" {
		title = "All tools / All sections"
		if m.CatalogSection >= 0 {
			title = "All tools / " + sections[m.CatalogSection]
		}
	}
	if m.CatalogMode == "import" {
		title = "Import / Choose a source"
	}
	if m.CatalogExpert {
		title = sections[m.CatalogSection] + " / Advanced tools"
	}
	out := []string{m.color(title, "1")}
	if m.CatalogMode == "all" {
		out = append(out, "Left / Right: change section    /: search tasks")
	} else if m.CatalogMode == "import" {
		out = append(out, "Prepare a source. Keep your original files.")
	} else if vm := m.selectedVM(); vm.Key.UUID != "" {
		out = append(out, clipCell("Selected VM: "+vm.Name+" ("+vm.State+")", width))
	} else {
		out = append(out, "Choose a task. Changes open a review before applying.")
	}
	if m.CatalogSearching || m.CatalogSearch != "" {
		out = append(out, "Find task: "+validation.SafeText(m.CatalogSearch))
	}
	rows := m.catalog()
	if len(rows) == 0 && !m.catalogHasAdvanced() {
		return append(out, "", "No matching tasks. Esc clears your search or goes back.")
	}
	selected := max(0, min(m.CatalogIndex, m.catalogCount()-1))
	type menuLine struct {
		text   string
		action int
	}
	lines := []menuLine{}
	last := ""
	selectedLine := 0
	for i, a := range rows {
		_, group := actionGroup(a)
		if m.CatalogSection < 0 {
			group = a.Section + " / " + group
		}
		if group != last {
			lines = append(lines, menuLine{m.color(group, "1;36"), -1})
			last = group
		}
		label := "    " + actionLabel(a)
		if i == selected {
			label = m.color(" >  "+actionLabel(a), "1;30;46")
			selectedLine = len(lines)
		}
		lines = append(lines, menuLine{label, i})
	}
	if m.catalogHasAdvanced() {
		label := "    Advanced tools... (A)"
		if selected == len(rows) {
			label = m.color(" >  Advanced tools... (A)", "1;30;46")
			selectedLine = len(lines)
		}
		lines = append(lines, menuLine{label, len(rows)})
	}
	// Keep the selected task and its explanation on screen; do not force users
	// to enter a form just to discover what an action does.
	count := max(1, height-len(out)-4)
	start := max(0, selectedLine-count+1)
	if start > 0 && lines[start].action >= 0 {
		count = max(1, count-1)
		start = max(0, selectedLine-count+1)
		if lines[start].action >= 0 && lines[start].action < len(rows) {
			first := rows[lines[start].action]
			_, group := actionGroup(first)
			if m.CatalogSection < 0 {
				group = first.Section + " / " + group
			}
			out = append(out, m.color(group+" (continued)", "1;36"))
		}
	}
	for i := start; i < min(len(lines), start+count); i++ {
		out = append(out, lines[i].text)
	}
	description := "Recovery, saved state and other specialist tools."
	if selected < len(rows) {
		description = actionDescription(rows[selected])
	}
	out = append(out, "")
	desc := wrap(description, width)
	out = append(out, desc[:min(2, len(desc))]...)
	out = append(out, fmt.Sprintf("%d of %d", selected+1, m.catalogCount()))
	return out
}
