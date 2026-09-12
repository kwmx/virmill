package tui

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// NetworkForm collects declaration intent. Allocation, capability checks and
// mutations remain in the shared service's reviewed network creation workflow.
type NetworkForm struct {
	Name, DisplayName, Type, CIDR, HostAccess string
	DHCP, AdvertiseDefaultRoute               bool
	Advanced                                  bool
	Focus                                     int
	Error                                     string
	cursor                                    int
	cursorField                               string
	notice                                    string
	issueOpen                                 bool
	issueOffset, issueWidth, issueHeight      int
	issueText                                 string
}

func NewNetworkForm() NetworkForm {
	return NetworkForm{Type: "nat", CIDR: "auto", HostAccess: "services-only", DHCP: true, AdvertiseDefaultRoute: true}
}

func (f NetworkForm) controls() []importControl {
	controls := f.optionControls()
	if f.Error != "" {
		controls = append(controls, importButton("issue", "Read full issue", "Read the complete error and recovery instructions. Esc returns to these settings."))
	}
	return controls
}

func (f NetworkForm) optionControls() []importControl {
	choice := func(id, label, help, value string, values ...string) importControl {
		return importControl{id: id, label: label, help: help, kind: "choice", value: value, choices: values}
	}
	if !f.Advanced {
		subnetHelp := "Use auto to choose a free private subnet during review, or enter 192.168.50.0/24."
		if f.Type == "guest-only" {
			subnetHelp = "Optional: reserve a subnet for static guest addresses. No host address is added."
		}
		return []importControl{
			importText("name", "Network name", "A short ID: lowercase letters, numbers and hyphens; start with a letter.", f.Name),
			choice("purpose", "Purpose", "Changing purpose applies its defaults. Review host access under Advanced options.", f.Type, "nat", "lab", "guest-only"),
			importText("cidr", "IPv4 subnet", subnetHelp, f.CIDR),
			importButton("advanced", "Advanced options", "Set a display name, host access and address services."),
			importButton("preview", "Preview network", "Review subnet allocation, host changes and requirements before applying."),
			importButton("export", "Export settings", "Save this declaration for reuse. Export does not create a network."),
			importButton("back", "Back", "Return without creating a network."),
		}
	}
	host := choice("host", "Host access", "Services only permits managed DHCP/DNS; Allow exposes other host services.", f.HostAccess, "services-only", "allow")
	dhcp := choice("dhcp", "DHCP + DNS", "Managed addresses and DNS. NAT also advertises its default route when enabled.", fmt.Sprint(f.DHCP), "true", "false")
	route := importControl{id: "route", label: "Default route", kind: "readonly", value: "Off", help: "Lab and guest-only networks never advertise a default route."}
	if f.Type == "nat" {
		route.value = "Off (DHCP disabled)"
		if f.AdvertiseDefaultRoute {
			route.value = "On with DHCP"
		}
		route.help = "This adapter couples NAT's default-route advertisement to DHCP. Change DHCP above."
	}
	if f.Type == "guest-only" {
		host = importControl{id: "host", label: "Host access", kind: "readonly", value: "Deny", help: "Guest-only has no host IP address and denies host access."}
		dhcp = importControl{id: "dhcp", label: "DHCP + DNS", kind: "readonly", value: "Off", help: "Use static guest addresses or provide a router/DHCP VM yourself."}
	}
	return []importControl{
		importText("display", "Display name", "Optional friendly label retained in the declaration and review.", f.DisplayName),
		host, dhcp, route,
		{id: "ipv6", label: "IPv6", kind: "readonly", value: "Disabled", help: "Managed creation currently supports IPv6 disabled only."},
		importButton("advanced-file", "Advanced declaration file", "Open the declaration-file workflow. Unsupported policies are refused by the service."),
		importButton("done", "Done", "Return to the basic options with these settings retained."),
	}
}

func (f NetworkForm) Update(key tea.KeyMsg) (NetworkForm, string) {
	f.syncIssue()
	if f.issueOpen {
		width, height := f.issueDimensions()
		rows := wrap(validation.SafeText(f.Error), width)
		page := max(1, height-4)
		last := max(0, len(rows)-page)
		f.issueOffset = min(f.issueOffset, last)
		switch key.Type {
		case tea.KeyEsc:
			f.issueOpen = false
		case tea.KeyUp:
			f.issueOffset = max(0, f.issueOffset-1)
		case tea.KeyDown:
			f.issueOffset = min(last, f.issueOffset+1)
		case tea.KeyPgUp:
			f.issueOffset = max(0, f.issueOffset-page)
		case tea.KeyPgDown:
			f.issueOffset = min(last, f.issueOffset+page)
		case tea.KeyHome:
			f.issueOffset = 0
		case tea.KeyEnd:
			f.issueOffset = last
		}
		return f, ""
	}
	if key.Type == tea.KeyEsc {
		if f.Advanced {
			f.Advanced, f.Focus, f.cursorField = false, 3, ""
			return f, ""
		}
		return f, "back"
	}
	controls := f.controls()
	f.Focus = max(0, min(f.Focus, len(controls)-1))
	c := controls[f.Focus]
	switch key.Type {
	case tea.KeyTab, tea.KeyDown:
		f.Focus = (f.Focus + 1) % len(controls)
		f.cursorField = ""
		return f, ""
	case tea.KeyShiftTab, tea.KeyUp:
		f.Focus = (f.Focus + len(controls) - 1) % len(controls)
		f.cursorField = ""
		return f, ""
	}
	activate := key.Type == tea.KeyEnter || key.Type == tea.KeySpace
	if c.kind == "choice" && (activate || key.Type == tea.KeyLeft || key.Type == tea.KeyRight) {
		direction := 1
		if key.Type == tea.KeyLeft {
			direction = -1
		}
		i := max(0, slices.Index(c.choices, c.value))
		value := c.choices[(i+direction+len(c.choices))%len(c.choices)]
		switch c.id {
		case "purpose":
			f.Type, f.HostAccess = value, "services-only"
			f.CIDR, f.DHCP, f.AdvertiseDefaultRoute = "auto", true, value == "nat"
			if value == "guest-only" {
				f.CIDR, f.HostAccess, f.DHCP = "", "deny", false
			}
			f.notice = "Purpose defaults applied. Check the subnet and Advanced options."
		case "host":
			f.HostAccess = value
		case "dhcp":
			f.DHCP = value == "true"
			f.AdvertiseDefaultRoute = f.Type == "nat" && f.DHCP
		}
		f.Error, f.cursorField = "", ""
		f.syncIssue()
		return f, ""
	}
	if c.kind == "button" && activate {
		switch c.id {
		case "issue":
			f.issueOpen, f.issueOffset = true, 0
		case "advanced":
			f.Advanced, f.Focus, f.cursorField = true, 0, ""
		case "done":
			f.Advanced, f.Focus, f.cursorField = false, 3, ""
		case "preview", "export":
			if _, err := f.Request(); err != nil {
				f.Error = err.Error()
				f.syncIssue()
				return f, ""
			}
			f.Error = ""
			f.syncIssue()
			return f, c.id
		case "back", "advanced-file":
			return f, c.id
		}
		return f, ""
	}
	if c.kind == "text" && key.Type != tea.KeyEnter {
		if f.cursorField != c.id {
			f.cursor, f.cursorField = utf8.RuneCountInString(c.value), c.id
		}
		limit := 256
		if c.id == "name" || c.id == "cidr" {
			limit = 63
		}
		input := GuidedForm{Fields: []GuidedField{{Name: c.id, Label: c.label, Value: c.value, Cursor: f.cursor, Limit: limit}}}
		if key.Type == tea.KeyCtrlU {
			input.Fields[0].Value, input.Fields[0].Cursor = "", 0
		} else {
			input, _, _ = input.Update(key)
		}
		if input.Error != "" {
			f.Error = input.Error
			return f, ""
		}
		f.cursor = input.Fields[0].Cursor
		value := input.Fields[0].Value
		switch c.id {
		case "name":
			f.Name = value
		case "display":
			f.DisplayName = value
		case "cidr":
			f.CIDR = value
		}
		f.Error, f.notice = "", ""
		f.syncIssue()
	}
	return f, ""
}

var networkFormName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func (f NetworkForm) Request() (app.Request, error) {
	fail := func(message string) (app.Request, error) { return app.Request{}, fmt.Errorf("%s", message) }
	if !networkFormName.MatchString(f.Name) {
		return fail("Network name: use 1–63 lowercase letters, numbers or hyphens; start with a letter.")
	}
	if f.DisplayName != "" {
		if _, err := validation.DisplayName(f.DisplayName); err != nil || utf8.RuneCountInString(f.DisplayName) > 256 {
			return fail("Display name: enter up to 256 characters without control characters.")
		}
	}
	s := network.Spec{Type: f.Type, HostAccess: f.HostAccess, Egress: "none"}
	s.IPv6.Mode = "disabled"
	if f.Type == "nat" {
		s.Egress = "any"
	}
	if f.Type != "nat" && f.Type != "lab" && f.Type != "guest-only" {
		return fail("Purpose: choose NAT internet, Isolated lab or Guest-only.")
	}
	if f.Type != "guest-only" || f.CIDR != "" {
		s.IPv4 = &network.IPv4{CIDR: f.CIDR}
		s.IPv4.DHCP.Enabled, s.IPv4.DHCP.AdvertiseDefaultRoute = f.DHCP, f.AdvertiseDefaultRoute
	}
	if f.Type == "guest-only" && (f.DHCP || f.AdvertiseDefaultRoute || f.HostAccess != "deny") {
		return fail("Guest-only requires host access denied, no managed DHCP/DNS and no default route.")
	}
	if f.Type != "guest-only" && f.HostAccess != "services-only" && f.HostAccess != "allow" {
		return fail("Host access: choose Services only or Allow for this network purpose.")
	}
	if f.Type == "nat" && f.AdvertiseDefaultRoute != f.DHCP {
		return fail("NAT default route must match DHCP. This adapter requires both on or both off.")
	}
	if err := network.Validate(s); err != nil {
		return fail("Network settings: " + err.Error())
	}
	if s.IPv4 != nil && f.CIDR != "auto" {
		p, err := netip.ParsePrefix(f.CIDR)
		private := false
		if err == nil && p.Addr().Is4() && p == p.Masked() && p.Bits() >= 8 && p.Bits() <= 30 && p.String() == f.CIDR {
			for _, block := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
				scope := netip.MustParsePrefix(block)
				private = private || p.Bits() >= scope.Bits() && scope.Contains(p.Addr())
			}
		}
		if !private {
			return fail("IPv4 subnet: use auto or a private, masked subnet such as 192.168.50.0/24 (/8–/30).")
		}
	}
	metadata := map[string]any{"name": f.Name}
	if f.DisplayName != "" {
		metadata["displayName"] = f.DisplayName
	}
	spec := map[string]any{"type": s.Type, "hostAccess": s.HostAccess, "egress": s.Egress, "ipv6": map[string]any{"mode": "disabled"}}
	if s.IPv4 != nil {
		spec["ipv4"] = s.IPv4
	}
	document := map[string]any{"apiVersion": domain.APIVersion, "kind": "Network", "metadata": metadata, "spec": spec}
	return app.Request{Action: "create", Input: map[string]any{"document": document}}, nil
}

func (f NetworkForm) exposure() string {
	host := "other host services allowed"
	if f.HostAccess == "services-only" {
		host = "managed DHCP/DNS only"
		if !f.DHCP {
			host = "services off; other host access denied"
		}
	}
	switch f.Type {
	case "guest-only":
		return "No host IP, DHCP/DNS or forwarding. Guests need their own addressing."
	case "lab":
		return "No external forwarding. Host: " + host + "."
	default:
		return "Internet + host-reachable LAN; host access: " + host + "."
	}
}

func (f NetworkForm) View(width, height int) []string {
	f.syncIssue()
	if width <= 0 || height <= 0 {
		return nil
	}
	if f.issueOpen {
		return f.issueView(width, height)
	}
	clean := func(s string) string { return ansi.Truncate(validation.SafeText(s), width, "…") }
	if width < 40 || height < 10 {
		return pageLines([]string{"Resize to continue. Your network settings are retained."}, width, height, 0)
	}
	title := "Create network"
	if f.Advanced {
		title += " · Advanced options"
	}
	lines := []string{clean(title)}
	lines = append(lines, wrap(validation.SafeText(f.exposure()), width)...)
	if f.Advanced {
		lines = append(lines, wrap("Network policy is not a sandbox; another guest NIC can provide a route.", width)...)
	}
	lines = append(lines, "")
	controls := f.controls()
	focus := max(0, min(f.Focus, len(controls)-1))
	footer := wrap(validation.SafeText(controls[focus].help), width)
	if f.Error != "" {
		footer = append(footer, pageLines([]string{"Issue: " + f.Error}, width, 2, 0)...)
		footer = append(footer, clean("Choose Read full issue for complete details and recovery steps."))
	} else if f.notice != "" {
		footer = append(footer, clean(f.notice))
	}
	footer = append(footer, clean("Tab/Arrows Select   Enter Choose   Ctrl-U Clear   Esc Back"))
	count := max(1, height-len(lines)-len(footer))
	start := max(0, focus-count+1)
	for i := start; i < min(len(controls), start+count); i++ {
		c := controls[i]
		mark := "  "
		if i == focus {
			mark = "> "
		}
		value := c.value
		switch c.id {
		case "purpose":
			value = map[string]string{"nat": "NAT internet", "lab": "Isolated lab", "guest-only": "Guest-only"}[value]
		case "host":
			if value == "services-only" {
				value = "Services only"
			} else if value == "allow" {
				value = "Allow"
			}
		case "dhcp":
			if c.kind == "choice" {
				value = "Off"
				if f.DHCP {
					value = "On"
				}
			}
		}
		switch c.kind {
		case "button":
			lines = append(lines, clean(mark+"[ "+c.label+" ]"))
		case "choice":
			lines = append(lines, clean(mark+c.label+": < "+value+" >"))
		case "readonly":
			lines = append(lines, clean(mark+c.label+": "+value+" (fixed)"))
		default:
			if i == focus {
				cursor := utf8.RuneCountInString(value)
				if f.cursorField == c.id {
					cursor = max(0, min(f.cursor, cursor))
				}
				r := []rune(value)
				value = string(r[:cursor]) + "|" + string(r[cursor:])
			}
			lines = append(lines, clean(mark+c.label+": ["+value+"]"))
		}
	}
	lines = append(lines, footer...)
	return lines[:min(height, len(lines))]
}

func (f *NetworkForm) syncIssue() {
	if f.issueText != f.Error {
		f.issueText, f.issueOffset, f.issueOpen = f.Error, 0, false
	}
}

func (f NetworkForm) issueDimensions() (int, int) {
	width, height := f.issueWidth, f.issueHeight
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 17
	}
	return width, height
}

// SetViewport is called before handling input; rendering remains read-only.
func (f *NetworkForm) SetViewport(width, height int) {
	f.issueWidth, f.issueHeight = width, height
}

func (f NetworkForm) issueView(width, height int) []string {
	rows := wrap(validation.SafeText(f.Error), width)
	count := max(1, height-4)
	f.issueOffset = min(f.issueOffset, max(0, len(rows)-count))
	end := min(len(rows), f.issueOffset+count)
	lines := []string{"Network creation issue", ""}
	lines = append(lines, rows[f.issueOffset:end]...)
	lines = append(lines, fmt.Sprintf("Lines %d–%d of %d", f.issueOffset+1, end, len(rows)), "Up/Down Scroll   PgUp/PgDn Page   Esc Back to settings")
	return pageLines(lines, width, height, 0)
}
