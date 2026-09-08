package tui

import (
	"fmt"
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
	case strings.HasPrefix(c, "vm creation "), strings.HasPrefix(c, "network creation "), c == "operation reconcile":
		return 80, "Recovery and troubleshooting"
	case c == "vm start" || c == "vm stop" || c == "vm reboot" || c == "vm pause" || c == "vm resume" || c == "vm save" || c == "vm restore-saved":
		return 10, "Power"
	case strings.HasPrefix(c, "import ") || c == "vm create":
		return 30, "Create and import"
	case c == "vm set" || c == "vm autostart" || strings.HasPrefix(c, "guest recipe "):
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
	case c == "operation cancel" || c == "operation watch":
		return 10, "Manage job"
	case strings.HasSuffix(c, "validate"):
		return 20, "Validate documents"
	default:
		return 90, "Inspect"
	}
}
func (m Workspace) catalog() []ui.Action {
	out := []ui.Action{}
	for _, a := range ui.Actions {
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
		priority := map[string]int{"vm start": 1, "vm stop": 2, "vm reboot": 3, "vm pause": 4, "vm resume": 5, "vm save": 6, "vm restore-saved": 7,
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
	out := []string{m.color(title, "1")}
	if m.CatalogMode == "all" {
		out = append(out, "Left / Right: change section    /: search tasks")
	} else if vm := m.selectedVM(); vm.Key.UUID != "" {
		out = append(out, clipCell("Selected VM: "+vm.Name+" ("+vm.State+")", width))
	} else {
		out = append(out, "Choose a task. Changes open a review before applying.")
	}
	if m.CatalogSearching || m.CatalogSearch != "" {
		out = append(out, "Find task: "+validation.SafeText(m.CatalogSearch))
	}
	rows := m.catalog()
	if len(rows) == 0 {
		return append(out, "", "No matching tasks. Esc clears your search or goes back.")
	}
	selected := max(0, min(m.CatalogIndex, len(rows)-1))
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
	// Keep the selected task and its explanation on screen; do not force users
	// to enter a form just to discover what an action does.
	count := max(1, height-len(out)-5)
	start := max(0, selectedLine-count+1)
	if start > 0 && lines[start].action >= 0 {
		count = max(1, count-1)
		start = max(0, selectedLine-count+1)
		if lines[start].action >= 0 {
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
	out = append(out, "", m.color(actionLabel(rows[selected]), "1"))
	desc := wrap(actionDescription(rows[selected]), width)
	for _, line := range desc[:min(2, len(desc))] {
		out = append(out, line)
	}
	out = append(out, fmt.Sprintf("Task %d of %d   |   Up/Down choose   Enter open   Esc back", selected+1, len(rows)))
	return out
}
