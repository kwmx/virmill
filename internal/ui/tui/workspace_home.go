package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"virmill.local/core/internal/buildinfo"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
)

// doctorChecks decodes the host check; ok is false until it loads.
func (m Workspace) doctorChecks() ([]domain.Capability, bool) {
	raw, loaded := m.Data["health"]
	if !loaded || m.Errors["health"] != "" {
		return nil, false
	}
	var checks []domain.Capability
	b, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(b, &checks) != nil {
		return nil, false
	}
	return checks, true
}

// homeSummary is the Overview's status block: VMs, storage, host, jobs and
// the latest job, each with the key that leads to the details.
func (m Workspace) homeSummary() []string {
	sep := m.separator()
	row := func(label, text string) string { return "  " + padCell(label, 10) + text }
	lines := []string{}
	if _, ok := m.Data["vms"]; ok {
		vms := array(m.Data["vms"])
		running, stopped := 0, 0
		for _, vm := range vms {
			switch rowState(vm) {
			case "running":
				running++
			case "stopped", "shut off":
				stopped++
			}
		}
		lines = append(lines, m.color(fmt.Sprintf("  %s%s%d running%s%d stopped", countLabel(len(vms), "VM", "VMs"), sep, running, sep, stopped), "1;36"))
	} else {
		lines = append(lines, "  Loading your virtual machines...")
	}
	if pools, ok := m.Data["pools"]; ok {
		text := "No storage pools" + sep + "press 4 for help"
		if list := array(pools); len(list) > 0 {
			text = countLabel(len(list), "pool", "pools")
			// Pools on one filesystem report the same free space, so show the largest, not the sum.
			best, known := 0.0, false
			for _, p := range list {
				if n, ok := sizeNumber(object(p)["availableBytes"]); ok && object(p)["active"] == true && n >= best {
					best, known = n, true
				}
			}
			if known {
				text += sep + "up to " + sizeText(best) + " free"
			}
		}
		lines = append(lines, row("Storage", text))
	}
	switch checks, ok := m.doctorChecks(); {
	case m.Errors["health"] != "":
		lines = append(lines, row("Host", "Check unavailable"+sep+"press , to retry"))
	case !ok:
		lines = append(lines, row("Host", "Checking..."))
	default:
		g := ui.GroupDoctor(checks)
		switch n := len(g.Missing) + len(g.Attention); {
		case n > 0:
			lines = append(lines, m.color(row("Host", countLabel(n, "problem needs fixing", "problems need fixing")+sep+"press , for steps"), "33"))
		case len(g.Optional) > 0:
			lines = append(lines, row("Host", "Ready"+sep+countLabel(len(g.Optional), "optional tool", "optional tools")+" not installed"+sep+"press , to see"))
		default:
			lines = append(lines, row("Host", "Ready"))
		}
	}
	if err := m.Errors["jobs"]; err != "" {
		lines = append(lines, row("Jobs", "Unavailable: "+err))
	} else {
		text := m.status()
		if strings.Contains(text, "attention") {
			text += sep + "press 9 to review"
		}
		lines = append(lines, row("Jobs", text))
	}
	var last any
	for _, j := range array(m.Data["jobs"]) {
		if last == nil || field(j, "createdAt") > field(last, "createdAt") {
			last = j
		}
	}
	// Older coordinators send no task name; an operation ID would not help here.
	if last != nil && field(last, "operation") != "" {
		lines = append(lines, row("Last job", m.jobName(last)+sep+rowState(last)+sep+ago(field(last, "createdAt"), clock())))
	}
	return lines
}

// settingsLines shows the connection, display options, version and the same
// host check as virmill doctor.
func (m Workspace) settingsLines(width int) []string {
	colors, symbols := "on", "Unicode"
	if m.NoColor {
		colors = "off"
	}
	if m.ASCII {
		symbols = "plain ASCII"
	}
	lines := []string{
		"Connection: " + m.Connection,
		"  Another connection: virmill tui --connection qemu:///session",
		"Display: colors " + colors + ", " + symbols + " symbols",
		"  Set NO_COLOR=1 to turn colors off, or VIRMILL_ASCII=1 for plain symbols",
		"Version: " + buildinfo.Version,
		"",
	}
	checks, ok := m.doctorChecks()
	if !ok {
		return append(lines, "Checking this host...")
	}
	// Wrap report lines under their own indent so steps stay aligned.
	for _, line := range strings.Split(strings.TrimRight(ui.DoctorReport(checks, m.ASCII), "\n"), "\n") {
		text := strings.TrimLeft(line, " ")
		indent := len(line) - len(text)
		if indent == 0 || width-indent < 20 {
			lines = append(lines, line)
			continue
		}
		for _, part := range wrap(text, width-indent) {
			lines = append(lines, strings.Repeat(" ", indent)+part)
		}
	}
	return lines
}
