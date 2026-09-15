package ui

import (
	"fmt"
	"slices"
	"strings"

	"virmill.local/core/internal/domain"
)

// DoctorGroups sorts host checks the way the report presents them.
type DoctorGroups struct {
	Ready, Missing, Attention, Optional, Notes []domain.Capability
}

// GroupDoctor puts each host check in one group. Older coordinators send no
// purpose and only a generic alternative, so their checks count as ready.
func GroupDoctor(checks []domain.Capability) DoctorGroups {
	var g DoctorGroups
	for _, c := range checks {
		switch {
		case c.ID == "host":
			g.Notes = append(g.Notes, c)
		case c.Status == "unsupported-on-this-configuration" && c.Optional:
			g.Optional = append(g.Optional, c)
		case c.Status == "unsupported-on-this-configuration":
			g.Missing = append(g.Missing, c)
		case c.Purpose != "" && len(c.Alternatives) > 0:
			g.Attention = append(g.Attention, c)
		default:
			g.Ready = append(g.Ready, c)
		}
	}
	return g
}

var doctorNames = map[string]string{"kvm": "KVM", "libvirt-access": "libvirt access", "network-firewall": "Network firewall", "host": "Host"}

// DoctorReport explains host checks with the commands that fix them, problems
// first. It is shared by virmill doctor and the TUI Settings page; ascii swaps
// the status marks for terminals without Unicode.
func DoctorReport(checks []domain.Capability, ascii bool) string {
	g := GroupDoctor(checks)
	width := 0
	for _, c := range checks {
		if c.ID != "host" {
			width = max(width, len(doctorName(c.ID)))
		}
	}
	marks := [4]string{"✗", "!", "✓", "-"}
	if ascii {
		marks = [4]string{"x", "!", "+", "-"}
	}
	var b strings.Builder
	b.WriteString("Virmill host check (read-only; nothing was changed)\n")
	for i, group := range []struct {
		title string
		items []domain.Capability
	}{{"Missing", g.Missing}, {"Needs attention", g.Attention}, {"Ready", g.Ready}, {"Optional, not installed", g.Optional}} {
		if len(group.items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n", group.title)
		indent := strings.Repeat(" ", width+6)
		for _, c := range group.items {
			detail := c.Purpose
			if detail == "" {
				detail = c.Reason
			}
			fmt.Fprintf(&b, "  %s %-*s  %s\n", marks[i], width, doctorName(c.ID), detail)
			if group.title == "Ready" {
				continue
			}
			// The group title already says a missing tool is not installed.
			if c.Purpose != "" && c.Reason != "not installed" {
				fmt.Fprintf(&b, "%s%s\n", indent, capitalize(c.Reason))
			}
			if c.Installer != "" && len(c.Packages) > 0 {
				fmt.Fprintf(&b, "%sInstall: %s %s\n", indent, c.Installer, strings.Join(c.Packages, " "))
			}
			for _, step := range c.Alternatives {
				fmt.Fprintf(&b, "%s%s\n", indent, step)
			}
		}
	}
	for _, note := range g.Notes {
		fmt.Fprintf(&b, "\nNote: %s\n", capitalize(note.Reason))
	}
	b.WriteString("\n")
	if len(g.Missing) == 0 && len(g.Attention) == 0 {
		b.WriteString("Everything Virmill needs is ready.\n")
	}
	for _, command := range installCommands(g.Missing) {
		fmt.Fprintf(&b, "Install what's missing:\n  %s\n", command)
	}
	for _, command := range installCommands(g.Optional) {
		fmt.Fprintf(&b, "Add the optional features:\n  %s\n", command)
	}
	return b.String()
}

func doctorName(id string) string {
	if name, ok := doctorNames[id]; ok {
		return name
	}
	return id
}

func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// installCommands merges package lists into one command per package manager.
func installCommands(checks []domain.Capability) []string {
	var installers []string
	packages := map[string][]string{}
	for _, c := range checks {
		if c.Installer == "" || len(c.Packages) == 0 {
			continue
		}
		if _, seen := packages[c.Installer]; !seen {
			installers = append(installers, c.Installer)
		}
		for _, p := range c.Packages {
			if !slices.Contains(packages[c.Installer], p) {
				packages[c.Installer] = append(packages[c.Installer], p)
			}
		}
	}
	commands := make([]string, 0, len(installers))
	for _, installer := range installers {
		commands = append(commands, installer+" "+strings.Join(packages[installer], " "))
	}
	return commands
}
