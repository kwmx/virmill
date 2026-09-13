package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// ShownError wraps an error whose message the command already printed.
type ShownError struct{ Err error }

func (s ShownError) Error() string { return s.Err.Error() }
func (s ShownError) Unwrap() error { return s.Err }

var doctorNames = map[string]string{"kvm": "KVM", "libvirt-access": "libvirt access", "host": "Host"}

// writeDoctor shows host checks as a readable report with install commands.
// JSON and NDJSON output keep the full machine-readable form.
func writeDoctor(out io.Writer, response app.Response) error {
	if response.Error != nil {
		if response.Error.Code == "COORDINATOR_UNAVAILABLE" {
			_, err := io.WriteString(out, "The Virmill coordinator isn't running, so the host check could not run.\n\n"+
				"Start it:\n  systemctl --user start virmilld.service\n\nThen run virmill doctor again.\n")
			if err != nil {
				return err
			}
			return ShownError{response.Error}
		}
		return writeResponse(out, "table", false, response)
	}
	var checks []domain.Capability
	data, err := json.Marshal(response.Data)
	if err == nil {
		err = json.Unmarshal(data, &checks)
	}
	if err != nil {
		return writeResponse(out, "table", false, response)
	}
	ready, missing, attention, optional, notes := []domain.Capability{}, []domain.Capability{}, []domain.Capability{}, []domain.Capability{}, []domain.Capability{}
	width := 0
	for _, c := range checks {
		switch {
		case c.ID == "host":
			notes = append(notes, c)
			continue
		case c.Status == "unsupported-on-this-configuration" && c.Optional:
			optional = append(optional, c)
		case c.Status == "unsupported-on-this-configuration":
			missing = append(missing, c)
		// Older coordinators send only a generic alternative and no purpose.
		case c.Purpose != "" && len(c.Alternatives) > 0:
			attention = append(attention, c)
		default:
			ready = append(ready, c)
		}
		width = max(width, len(doctorName(c.ID)))
	}
	var b strings.Builder
	b.WriteString("Virmill host check (read-only; nothing was changed)\n")
	for _, group := range []struct {
		title, mark string
		items       []domain.Capability
	}{{"Ready", "✓", ready}, {"Missing", "✗", missing}, {"Needs attention", "!", attention}, {"Optional, not installed", "-", optional}} {
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
			fmt.Fprintf(&b, "  %s %-*s  %s\n", group.mark, width, doctorName(c.ID), detail)
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
	for _, note := range notes {
		fmt.Fprintf(&b, "\nNote: %s\n", capitalize(note.Reason))
	}
	b.WriteString("\n")
	if len(missing) == 0 && len(attention) == 0 {
		b.WriteString("Everything Virmill needs is ready.\n")
	}
	for _, command := range installCommands(missing) {
		fmt.Fprintf(&b, "Install what's missing:\n  %s\n", command)
	}
	for _, command := range installCommands(optional) {
		fmt.Fprintf(&b, "Add the optional features:\n  %s\n", command)
	}
	_, err = io.WriteString(out, validation.SafeText(b.String()))
	return err
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
