package tui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/validation"
)

func networkFormFocus(t *testing.T, f NetworkForm, id string) NetworkForm {
	t.Helper()
	for i, c := range f.controls() {
		if c.id == id {
			f.Focus = i
			return f
		}
	}
	t.Fatal("missing control", id)
	return f
}

func networkFormActivate(t *testing.T, f NetworkForm, id string) (NetworkForm, string) {
	t.Helper()
	f = networkFormFocus(t, f, id)
	return f.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

func networkFormDocument(t *testing.T, f NetworkForm) (map[string]any, network.Spec) {
	t.Helper()
	r, err := f.Request()
	if err != nil {
		t.Fatal(err)
	}
	if r.Action != "create" || r.Path != "" || r.ID != "" || r.Connection != "" || r.Apply != nil || len(r.Input) != 1 {
		t.Fatal("form escaped inline reviewed creation contract", r)
	}
	doc, ok := r.Input["document"].(map[string]any)
	if !ok {
		t.Fatal("missing complete document")
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := validation.Document(raw); err != nil {
		t.Fatal("form produced invalid declaration", err, string(raw))
	}
	var envelope struct {
		Spec network.Spec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if err := network.Validate(envelope.Spec); err != nil {
		t.Fatal("invalid network intent", err)
	}
	return doc, envelope.Spec
}

func TestNetworkFormPurposeDefaultsProduceValidDeclarations(t *testing.T) {
	f := NewNetworkForm()
	f.Name = "test-network"
	for _, kind := range []string{"nat", "lab", "guest-only"} {
		t.Run(kind, func(t *testing.T) {
			doc, spec := networkFormDocument(t, f)
			if doc["apiVersion"] != "virmill/v1" || doc["kind"] != "Network" || spec.Type != kind || spec.IPv6.Mode != "disabled" {
				t.Fatal("wrong profile declaration", doc)
			}
			if kind == "guest-only" {
				if spec.IPv4 != nil || spec.HostAccess != "deny" || spec.Egress != "none" {
					t.Fatal("guest-only introduced host addressing/services", spec)
				}
			} else if spec.HostAccess != "services-only" || spec.IPv4 == nil || spec.IPv4.CIDR != "auto" || !spec.IPv4.DHCP.Enabled || spec.IPv4.DHCP.AdvertiseDefaultRoute != (kind == "nat") || (spec.Egress == "any") != (kind == "nat") {
				t.Fatal("incorrect NAT/lab defaults", spec)
			}
		})
		var action string
		f, action = networkFormActivate(t, f, "purpose")
		if action != "" || f.Error != "" || !strings.Contains(f.notice, "defaults applied") {
			t.Fatal("purpose change was not explicit")
		}
	}
}

func TestNetworkFormAdvancedChoicesPreservedAcrossPreviewAndBack(t *testing.T) {
	f := NewNetworkForm()
	f.Name, f.CIDR = "work-lab", "192.168.61.0/24"
	f, _ = networkFormActivate(t, f, "advanced")
	if !f.Advanced {
		t.Fatal("advanced options inaccessible")
	}
	f = networkFormFocus(t, f, "display")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Work lab")})
	f, _ = networkFormActivate(t, f, "host")
	f, _ = networkFormActivate(t, f, "dhcp")
	if f.HostAccess != "allow" || f.DHCP || f.AdvertiseDefaultRoute || f.DisplayName != "Work lab" {
		t.Fatal("advanced settings not applied", f)
	}
	f, action := f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if f.Advanced || action != "" {
		t.Fatal("advanced back canceled the whole form")
	}
	doc, _ := networkFormDocument(t, f)
	if doc["metadata"].(map[string]any)["displayName"] != "Work lab" {
		t.Fatal("display name was dropped")
	}
	for _, button := range []string{"preview", "export"} {
		f, action = networkFormActivate(t, f, button)
		if action != button || f.HostAccess != "allow" || f.CIDR != "192.168.61.0/24" || f.DHCP || f.DisplayName != "Work lab" {
			t.Fatal("review/export discarded settings", f, action)
		}
		after, _ := networkFormDocument(t, f)
		if !reflect.DeepEqual(doc, after) {
			t.Fatal("export and review differed")
		}
	}
	f.Error = "The host subnet changed. Review again."
	f, _ = networkFormActivate(t, f, "advanced")
	f, _ = networkFormActivate(t, f, "done")
	if f.Error == "" || f.HostAccess != "allow" || f.DisplayName != "Work lab" {
		t.Fatal("navigation hid a service error or discarded edits")
	}
	f, action = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if action != "back" || f.CIDR != "192.168.61.0/24" {
		t.Fatal("back altered the form")
	}
}

func TestNetworkFormGuestOnlyOptionalSubnetAndFixedServices(t *testing.T) {
	f := NewNetworkForm()
	f.Name = "private-segment"
	f, _ = networkFormActivate(t, f, "purpose")
	f, _ = networkFormActivate(t, f, "purpose")
	for _, cidr := range []string{"", "auto", "10.60.0.0/24"} {
		f.CIDR = cidr
		_, spec := networkFormDocument(t, f)
		if cidr == "" && spec.IPv4 != nil || cidr != "" && (spec.IPv4 == nil || spec.IPv4.CIDR != cidr || spec.IPv4.DHCP.Enabled || spec.IPv4.DHCP.AdvertiseDefaultRoute) {
			t.Fatal("guest-only optional logical reservation incorrect", spec)
		}
	}
	f, _ = networkFormActivate(t, f, "advanced")
	for _, id := range []string{"host", "dhcp", "route", "ipv6"} {
		prior := f
		f, action := networkFormActivate(t, f, id)
		if action != "" || f.HostAccess != prior.HostAccess || f.DHCP != prior.DHCP || f.AdvertiseDefaultRoute != prior.AdvertiseDefaultRoute {
			t.Fatal("fixed unsupported control changed intent", id)
		}
	}
	f, action := networkFormActivate(t, f, "advanced-file")
	if action != "advanced-file" || !f.Advanced || f.CIDR != "10.60.0.0/24" {
		t.Fatal("declaration fallback lost editable form")
	}
}

func TestNetworkFormRefusesInvalidAndUnsupportedIntent(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*NetworkForm)
	}{
		{"blank name", func(f *NetworkForm) { f.Name = "" }},
		{"invalid name", func(f *NetworkForm) { f.Name = "My network" }},
		{"unsafe display", func(f *NetworkForm) { f.DisplayName = "Name\x1b[2J" }},
		{"oversize display", func(f *NetworkForm) { f.DisplayName = strings.Repeat("a", 257) }},
		{"unknown purpose", func(f *NetworkForm) { f.Type = "bridge" }},
		{"missing required subnet", func(f *NetworkForm) { f.CIDR = "" }},
		{"host address not subnet", func(f *NetworkForm) { f.CIDR = "192.168.50.1/24" }},
		{"public subnet", func(f *NetworkForm) { f.CIDR = "8.8.8.0/24" }},
		{"IPv6 subnet", func(f *NetworkForm) { f.CIDR = "fd00::/64" }},
		{"too small subnet", func(f *NetworkForm) { f.CIDR = "192.168.50.0/31" }},
		{"spans outside private", func(f *NetworkForm) { f.CIDR = "172.0.0.0/8" }},
		{"NAT route mismatch", func(f *NetworkForm) { f.AdvertiseDefaultRoute = false }},
		{"lab route", func(f *NetworkForm) { f.Type = "lab" }},
		{"unsupported deny", func(f *NetworkForm) { f.HostAccess = "deny" }},
		{"guest services", func(f *NetworkForm) { f.Type, f.CIDR, f.HostAccess = "guest-only", "", "deny" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewNetworkForm()
			f.Name = "test-network"
			tc.edit(&f)
			if r, err := f.Request(); err == nil || r.Input != nil || r.Action != "" {
				t.Fatal("invalid intent produced a request", r, err)
			}
			before := f
			f, action := networkFormActivate(t, f, "preview")
			if action != "" || f.Error == "" || f.Name != before.Name || f.CIDR != before.CIDR || f.HostAccess != before.HostAccess {
				t.Fatal("invalid preview escaped or lost choices", f, action)
			}
		})
	}
}

func TestNetworkFormKeyboardEditsAndReachable80x24Controls(t *testing.T) {
	f := NewNetworkForm()
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("testing")})
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyLeft})
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if f.Name != "testig" {
		t.Fatal("cursor editing broken", f.Name)
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if f.Name != "" {
		t.Fatal("Ctrl-U did not clear")
	}
	f.Name = "test-network"
	for _, advanced := range []bool{false, true} {
		f.Advanced, f.Focus = advanced, 0
		for _, issue := range []string{"", "The chosen subnet now overlaps an existing network. Choose another subnet or auto and preview again."} {
			f.Error = issue
			for range f.controls() {
				controls := f.controls()
				focused := controls[f.Focus]
				lines := f.View(80, 17)
				view := strings.Join(lines, "\n")
				if len(lines) > 17 || !strings.Contains(view, "> ") || !strings.Contains(view, focused.label) || !strings.Contains(view, "Ctrl-U Clear") || !strings.Contains(view, "Esc Back") {
					t.Fatal("80x24 form body lost focus or footer", view)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > 80 {
						t.Fatal("width overflow", line)
					}
				}
				var action string
				f, action = f.Update(tea.KeyMsg{Type: tea.KeyTab})
				if action != "" {
					t.Fatal("navigation emitted intent")
				}
			}
			if f.Focus != 0 {
				t.Fatal("not all controls reachable by Tab")
			}
		}
	}
	f.Advanced, f.Focus, f.Error = false, 0, ""
	t.Log("Synthetic 80x24 network body:\n" + strings.Join(f.View(80, 17), "\n"))
}

func TestNetworkFormExposureAndAdapterLimitsAreExplicit(t *testing.T) {
	f := NewNetworkForm()
	view := strings.Join(f.View(80, 17), "\n")
	for _, want := range []string{"host-reachable LAN", "managed DHCP/DNS only"} {
		if !strings.Contains(view, want) {
			t.Fatal("NAT exposure missing", want, view)
		}
	}
	f.Advanced = true
	f = networkFormFocus(t, f, "ipv6")
	view = strings.Join(f.View(80, 17), "\n")
	if !strings.Contains(view, "IPv6: Disabled (fixed)") || !strings.Contains(view, "supports IPv6 disabled only") || !strings.Contains(view, "not a sandbox") {
		t.Fatal("IPv6 capability limit hidden", view)
	}
	f.HostAccess = "allow"
	if !strings.Contains(f.exposure(), "other host services allowed") {
		t.Fatal("allow profile hid host exposure")
	}
	f.Type = "lab"
	if !strings.Contains(f.exposure(), "No external forwarding") {
		t.Fatal("lab egress unclear")
	}
	f.Type = "guest-only"
	if !strings.Contains(f.exposure(), "Guests need their own addressing") {
		t.Fatal("guest-only addressing requirement hidden")
	}
}
