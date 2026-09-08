package tui

import (
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// GuidedField holds displayable values or file references, never file contents.
type GuidedField struct {
	Name, Label, Hint, Value string
	Limit                    int // UTF-8 bytes
	Cursor                   int // rune offset
}

// GuidedForm only collects a preview request. The workspace owns service calls
// and the separate immutable-plan approval flow.
type GuidedForm struct {
	Kind   string
	VM     domain.VM
	Fields []GuidedField
	Focus  int
	Error  string
}

var guidedUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var guidedRootID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var guidedUser = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)

func NewGuidedForm(kind string, vm domain.VM) (GuidedForm, error) {
	f := GuidedForm{Kind: kind, VM: vm}
	field := func(name, label, hint string, limit int) {
		f.Fields = append(f.Fields, GuidedField{Name: name, Label: label, Hint: hint, Limit: limit})
	}
	switch kind {
	case "resources":
		field("vcpus", "CPU count", "1–512; blank keeps the current value", 3)
		field("memoryMiB", "Memory (MiB)", "1–1048576; blank keeps the current value", 7)
	case "capture":
		field("sourceRoot", "Source directory", "Absolute directory containing the source disks", 4096)
		field("auxiliaryRootID", "Helper root ID (optional)", "Administrator-approved root for firmware/TPM state", 64)
	case "guest-recipe":
		field("recipe", "Recipe file", "Absolute path to the reviewed GuestRecipe file", 4096)
		field("address", "Guest IP address", "Explicit IPv4 or IPv6; no address discovery", 45)
		field("port", "SSH port", "1–65535", 5)
		f.Fields[len(f.Fields)-1].Value, f.Fields[len(f.Fields)-1].Cursor = "22", 2
		field("user", "Guest user", "Existing non-root SSH user", 32)
		field("identityFile", "SSH private-key file", "Absolute file path; never paste a private key", 4096)
		field("knownHostsFile", "Known-hosts file", "Absolute file path with the approved host key", 4096)
		field("arguments", "Arguments (optional)", "Space-separated; quote spaces. No expansion. No secrets.", 32768)
	case "repository-init", "repository-check":
		field("repository", "Repository directory", "Absolute local encrypted-repository path", 4096)
		field("passwordFile", "Password file", "Absolute file path outside the repository; no password text", 4096)
	default:
		return GuidedForm{}, domain.Fail("INVALID_INPUT", "unknown guided form")
	}
	if kind == "resources" || kind == "capture" || kind == "guest-recipe" {
		if vm.Key.ProviderID != "libvirt" || vm.Key.Kind != "vm" || !guidedUUID.MatchString(vm.Key.UUID) || vm.Key.UUID == "00000000-0000-0000-0000-000000000000" || !guidedLocal(vm.Key.ConnectionID) {
			return GuidedForm{}, domain.Fail("INVALID_INPUT", "select an exact local VM before opening this form")
		}
	}
	return f, nil
}

func (f GuidedForm) Title() string {
	switch f.Kind {
	case "resources":
		return "Edit CPU and memory for next boot"
	case "capture":
		return "Create a cold recovery point"
	case "guest-recipe":
		return "Run a reviewed guest recipe"
	case "repository-init":
		return "Initialize an encrypted repository"
	case "repository-check":
		return "Check an encrypted repository"
	default:
		return "Guided form unavailable"
	}
}

func (f GuidedForm) note() string {
	switch f.Kind {
	case "resources":
		return "Requires a stopped persistent VM. Enter opens a plan for review."
	case "capture":
		return "Requires a stopped VM. Enter previews capture; files stay private."
	case "guest-recipe":
		return "Requires a running VM. You approve its host-key binding; no root or reboot."
	default:
		return "Enter opens a plan for review. Only password-file references are accepted."
	}
}

func guidedPrintable(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) || unicode.In(r, unicode.Cf) {
			return false
		}
	}
	return true
}

func (f GuidedForm) Update(key tea.KeyMsg) (GuidedForm, bool, bool) {
	f.Fields = slices.Clone(f.Fields)
	if key.Type == tea.KeyEsc {
		return f, false, true
	}
	if len(f.Fields) == 0 {
		f.Error = "Choose a supported form."
		return f, false, false
	}
	f.Focus = max(0, min(f.Focus, len(f.Fields)-1))
	if key.Type == tea.KeyEnter {
		connection := f.VM.Key.ConnectionID
		if connection == "" {
			connection = "qemu:///system"
		}
		_, _, field, err := f.request(connection)
		if err != nil {
			f.Error = err.Error()
			if field >= 0 {
				f.Focus = field
			}
			return f, false, false
		}
		f.Error = ""
		return f, true, false
	}
	switch key.Type {
	case tea.KeyTab, tea.KeyDown:
		f.Focus = (f.Focus + 1) % len(f.Fields)
		return f, false, false
	case tea.KeyShiftTab, tea.KeyUp:
		f.Focus = (f.Focus + len(f.Fields) - 1) % len(f.Fields)
		return f, false, false
	}
	field := &f.Fields[f.Focus]
	runes := []rune(field.Value)
	field.Cursor = max(0, min(field.Cursor, len(runes)))
	switch key.Type {
	case tea.KeyLeft:
		field.Cursor = max(0, field.Cursor-1)
	case tea.KeyRight:
		field.Cursor = min(len(runes), field.Cursor+1)
	case tea.KeyHome:
		field.Cursor = 0
	case tea.KeyEnd:
		field.Cursor = len(runes)
	case tea.KeyBackspace:
		if field.Cursor > 0 {
			field.Value = string(runes[:field.Cursor-1]) + string(runes[field.Cursor:])
			field.Cursor--
			f.Error = ""
		}
	case tea.KeyDelete:
		if field.Cursor < len(runes) {
			field.Value = string(runes[:field.Cursor]) + string(runes[field.Cursor+1:])
			f.Error = ""
		}
	case tea.KeyRunes, tea.KeySpace:
		text := string(key.Runes)
		if key.Type == tea.KeySpace {
			text = " "
		}
		if key.Alt || !guidedPrintable(text) {
			f.Error = "Use printable text; terminal controls were not inserted."
			break
		}
		if len(field.Value)+len(text) > field.Limit {
			f.Error = fmt.Sprintf("%s is limited to %d bytes.", field.Label, field.Limit)
			break
		}
		field.Value = string(runes[:field.Cursor]) + text + string(runes[field.Cursor:])
		field.Cursor += utf8.RuneCountInString(text)
		f.Error = ""
	}
	return f, false, false
}

func guidedLocal(connection string) bool {
	return connection == "qemu:///system" || connection == "qemu:///session"
}

func guidedPath(value string) bool {
	return value != "" && len(value) <= 4096 && guidedPrintable(value) && strings.TrimSpace(value) == value && filepath.IsAbs(value) && filepath.Clean(value) == value
}

func guidedNumber(value string, limit uint64) (uint64, bool) {
	n, err := strconv.ParseUint(value, 10, 64)
	return n, err == nil && n > 0 && n <= limit && strconv.FormatUint(n, 10) == value
}

func (f GuidedForm) Request(connection string) (string, app.Request, error) {
	method, request, _, err := f.request(connection)
	return method, request, err
}

func (f GuidedForm) request(connection string) (string, app.Request, int, error) {
	fail := func(name, message string) (string, app.Request, int, error) {
		index := slices.IndexFunc(f.Fields, func(field GuidedField) bool { return field.Name == name })
		return "", app.Request{}, index, domain.Fail("INVALID_INPUT", message)
	}
	expected, err := NewGuidedForm(f.Kind, f.VM)
	if err != nil || !guidedLocal(connection) || (f.VM.Key.UUID != "" && f.Kind != "repository-init" && f.Kind != "repository-check" && f.VM.Key.ConnectionID != connection) {
		return fail("", "Select the same local VM connection before reviewing this form.")
	}
	if len(f.Fields) != len(expected.Fields) {
		return fail("", "The form is incomplete; reopen it.")
	}
	values := make(map[string]string, len(f.Fields))
	for i, field := range f.Fields {
		if field.Name != expected.Fields[i].Name || len(field.Value) > expected.Fields[i].Limit || !guidedPrintable(field.Value) {
			return fail(field.Name, "A field contains unsupported or excessive text.")
		}
		values[field.Name] = field.Value
	}
	r := app.Request{Connection: connection, Input: map[string]any{}}
	switch f.Kind {
	case "resources":
		for _, field := range []struct {
			name, label string
			limit       uint64
		}{{"vcpus", "CPU count", 512}, {"memoryMiB", "Memory (MiB)", 1048576}} {
			if values[field.name] == "" {
				continue
			}
			n, ok := guidedNumber(values[field.name], field.limit)
			if !ok {
				return fail(field.name, fmt.Sprintf("%s must be a whole number from 1 to %d.", field.label, field.limit))
			}
			r.Input[field.name] = float64(n)
		}
		if len(r.Input) == 0 {
			return fail("vcpus", "Enter CPU count or memory; blank fields keep their current values.")
		}
		r.ID, r.Action, r.Input["applyMode"] = f.VM.Key.UUID, "set", "next-boot"
		return "vm.plan", r, -1, nil
	case "capture":
		if !guidedPath(values["sourceRoot"]) {
			return fail("sourceRoot", "Source directory must be a canonical absolute path.")
		}
		r.ID, r.Action, r.Input["sourceRoot"] = f.VM.Key.UUID, "create", values["sourceRoot"]
		if id := values["auxiliaryRootID"]; id != "" {
			if !guidedRootID.MatchString(id) {
				return fail("auxiliaryRootID", "Helper root ID must contain 1–64 letters, digits, dots, hyphens or underscores, beginning with a letter or digit.")
			}
			r.Input["auxiliaryRootID"] = id
		}
		return "snapshot.create", r, -1, nil
	case "guest-recipe":
		for _, name := range []string{"recipe", "identityFile", "knownHostsFile"} {
			if !guidedPath(values[name]) {
				return fail(name, "Recipe and SSH credential references must be canonical absolute file paths.")
			}
		}
		address, err := netip.ParseAddr(values["address"])
		if err != nil || address.String() != values["address"] || address.Is4In6() || address.Zone() != "" || address.IsLoopback() || !address.IsGlobalUnicast() {
			return fail("address", "Enter a canonical guest IPv4 or IPv6 address; DNS, loopback and link-local addresses are unsupported.")
		}
		port, ok := guidedNumber(values["port"], 65535)
		if !ok {
			return fail("port", "SSH port must be a whole number from 1 to 65535.")
		}
		if !guidedUser.MatchString(values["user"]) || strings.EqualFold(values["user"], "root") {
			return fail("user", "Enter an existing non-root SSH user (up to 32 letters, digits, underscores, dots or hyphens).")
		}
		args, err := guidedArguments(values["arguments"])
		if err != nil {
			return fail("arguments", err.Error())
		}
		r.ID, r.Path, r.Action = f.VM.Key.UUID, values["recipe"], "run"
		r.Input = map[string]any{"address": values["address"], "port": float64(port), "user": values["user"], "identityFile": values["identityFile"], "knownHostsFile": values["knownHostsFile"], "arguments": args}
		return "guest.recipe.run", r, -1, nil
	case "repository-init", "repository-check":
		for _, name := range []string{"repository", "passwordFile"} {
			if !guidedPath(values[name]) {
				return fail(name, "Repository and password-file references must be canonical absolute paths.")
			}
		}
		if values["passwordFile"] == values["repository"] || strings.HasPrefix(values["passwordFile"], strings.TrimSuffix(values["repository"], string(filepath.Separator))+string(filepath.Separator)) {
			return fail("passwordFile", "Keep the password file outside the repository directory.")
		}
		r.Path, r.Action, r.Input["passwordFile"] = values["repository"], strings.TrimPrefix(f.Kind, "repository-"), values["passwordFile"]
		return "backup.repository." + r.Action, r, -1, nil
	}
	return fail("", "Choose a supported form.")
}

// Quotes group arguments and backslash escapes the following character outside
// single quotes. Nothing expands: $, backticks, globbing and semicolons are data.
func guidedArguments(text string) ([]string, error) {
	args := []string{}
	var word strings.Builder
	quote := rune(0)
	escaped, started, total := false, false, 0
	flush := func() bool {
		if !started {
			return true
		}
		total += word.Len()
		if len(args) == 64 || word.Len() > 4096 || total > 16384 {
			return false
		}
		args = append(args, word.String())
		word.Reset()
		started = false
		return true
	}
	for _, r := range text {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, started = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case unicode.IsSpace(r):
			if !flush() {
				return nil, fmt.Errorf("Arguments allow at most 64 values, 4096 bytes each and 16384 bytes total.")
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("Close argument quotes and finish any backslash escape before reviewing.")
	}
	if !flush() {
		return nil, fmt.Errorf("Arguments allow at most 64 values, 4096 bytes each and 16384 bytes total.")
	}
	return args, nil
}

func (f GuidedForm) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	clip := func(s string) string {
		s = strings.NewReplacer("\n", " ", "\t", " ").Replace(validation.SafeText(s))
		return ansi.Truncate(s, width, "")
	}
	if width < 24 || height < 6 {
		lines := []string{clip("Resize to edit this form."), clip("Esc cancels; input is retained.")}
		return strings.Join(lines[:min(height, len(lines))], "\n")
	}
	lines := []string{clip(f.Title()), clip(f.note())}
	footer := []string{}
	if f.Error != "" {
		footer = append(footer, clip("Error: "+f.Error))
	}
	keys := "Tab fields | Enter review | Esc cancel"
	if width < 38 {
		keys = "Enter review; Esc cancel"
	}
	footer = append(footer, clip(keys))
	if len(f.Fields) == 0 {
		lines = append(lines, clip("No fields; reopen a supported form."))
	} else {
		focus := max(0, min(f.Focus, len(f.Fields)-1))
		count := max(1, (height-len(lines)-len(footer))/2)
		first := max(0, focus-count+1)
		for i := first; i < min(len(f.Fields), first+count); i++ {
			field := f.Fields[i]
			prefix := "  "
			if i == focus {
				prefix = "> "
			}
			lines = append(lines, clip(fmt.Sprintf("%s%d/%d %s", prefix, i+1, len(f.Fields), field.Label)))
			value := validation.SafeText(field.Value)
			if value == "" {
				value = "[" + field.Hint + "]"
			} else if i == focus {
				runes := []rune(value)
				cursor := max(0, min(field.Cursor, len(runes)))
				before := string(runes[:cursor])
				if cells := ansi.StringWidth(before); cells >= width-4 {
					before = "…" + ansi.Cut(before, cells-(width-5), cells)
				}
				value = before + "|" + string(runes[cursor:])
			}
			lines = append(lines, clip("  "+value))
		}
	}
	lines = append(lines, footer...)
	return strings.Join(lines[:min(height, len(lines))], "\n")
}
