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
	Choices                  []string
	Toggle                   bool
}

// GuidedForm only collects a preview request. The workspace owns service calls
// and the separate immutable-plan approval flow.
type GuidedForm struct {
	Kind                          string
	VM                            domain.VM
	Fields                        []GuidedField
	Focus                         int
	Error                         string
	ToolsAdvanced                 bool
	removalIssueOpen              bool
	removalIssueText              string
	removalIssueOffset            int
	viewportWidth, viewportHeight int
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
	case "remove-definition":
		field("confirmation", "Type VM name", "Type the exact VM name above, then choose Preview.", 256)
	case "autostart":
		field("enabled", "Requested", "Choose whether this VM should start with its libvirt service.", 5)
		f.Fields[0].Value, f.Fields[0].Toggle = strconv.FormatBool(vm.Autostart), true
	case "resources":
		field("vcpus", "CPU count", "1–512 CPUs. Leave blank to keep the current value.", 3)
		field("memoryMiB", "Memory (MiB)", "1–1048576 MiB. Leave blank to keep the current value.", 7)
	case "capture":
		field("sourceRoot", "Source directory", "Choose the directory containing this VM's disks.", 4096)
		field("auxiliaryRootID", "Helper root ID (optional)", "For firmware/TPM: use an administrator-approved root ID.", 64)
	case "guest-tools":
		field("profile", "Guest system", "Left/Right chooses a system. Automatic detection supports Debian, Ubuntu and Fedora only.", 32)
		f.Fields[0].Value = "linux-auto"
		f.Fields[0].Choices = []string{"linux-auto", "debian", "ubuntu", "fedora", "windows"}
		field("desktop", "Desktop tools", "Optional desktop agent; clipboard/resizing also need compatible SPICE channels and viewer.", 5)
		f.Fields[1].Value, f.Fields[1].Toggle = "false", true
		field("address", "Guest IP address", "Use this VM's IP address from its console or network settings.", 45)
		field("user", "Guest SSH user", "An existing guest account with SSH access and passwordless sudo.", 32)
		field("identityFile", "SSH key", "Choose the local private key used to log in to this guest. No key contents.", 4096)
		field("knownHostsFile", "Verified host keys", "Choose known_hosts after verifying the guest fingerprint through a trusted console.", 4096)
		field("port", "SSH port", "Usually 22.", 5)
		f.Fields[6].Value, f.Fields[6].Cursor = "22", 2
	case "guest-recipe":
		field("recipe", "Recipe file", "Choose the guest setup recipe you have reviewed.", 4096)
		field("address", "Guest IP address", "Enter this guest's IPv4 or IPv6 address.", 45)
		field("port", "SSH port", "Usually 22. Use the guest's SSH port (1–65535).", 5)
		f.Fields[len(f.Fields)-1].Value, f.Fields[len(f.Fields)-1].Cursor = "22", 2
		field("user", "Guest user", "An existing SSH user in the guest; root is not allowed.", 32)
		field("identityFile", "SSH private-key file", "Choose a key file; never paste a private key.", 4096)
		field("knownHostsFile", "Known-hosts file", "Choose the file holding the approved guest host key.", 4096)
		field("arguments", "Arguments (optional)", "Recipe inputs: quote spaces. No expansion or secrets.", 32768)
	case "repository-init", "repository-check":
		field("repository", "Repository directory", "Choose the local directory for encrypted backups.", 4096)
		field("passwordFile", "Password file", "Choose a file outside the repository; no password text.", 4096)
	default:
		return GuidedForm{}, domain.Fail("INVALID_INPUT", "unknown guided form")
	}
	if kind == "resources" || kind == "capture" || kind == "guest-recipe" || kind == "guest-tools" || kind == "autostart" || kind == "remove-definition" {
		if vm.Key.ProviderID != "libvirt" || vm.Key.Kind != "vm" || !guidedUUID.MatchString(vm.Key.UUID) || vm.Key.UUID == "00000000-0000-0000-0000-000000000000" || !guidedLocal(vm.Key.ConnectionID) {
			return GuidedForm{}, domain.Fail("INVALID_INPUT", "select an exact local VM before opening this form")
		}
	}
	if kind == "autostart" && strings.TrimSpace(vm.PersistentXML) == "" {
		return GuidedForm{}, domain.Fail("UNSUPPORTED_CAPABILITY", "Automatic startup requires a persistent VM. This VM has no saved definition.")
	}
	if kind == "remove-definition" {
		if vm.State != "stopped" || strings.TrimSpace(vm.PersistentXML) == "" || vm.Autostart || vm.HasManagedSave {
			return GuidedForm{}, domain.Fail("UNSUPPORTED_CAPABILITY", "Removal requires a stopped persistent VM with automatic startup off and no saved runtime state.")
		}
		if !guidedPrintable(vm.Name) || strings.TrimSpace(vm.Name) == "" || len(vm.Name) > 256 {
			return GuidedForm{}, domain.Fail("INVALID_INPUT", "This VM's name cannot be confirmed safely in the removal form.")
		}
	}
	return f, nil
}

func (f GuidedForm) Title() string {
	switch f.Kind {
	case "remove-definition":
		return "Remove VM"
	case "autostart":
		return "Start automatically"
	case "resources":
		return "Edit CPU and memory for next boot"
	case "capture":
		return "Create a cold recovery point"
	case "guest-tools":
		return "Install guest tools"
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
	case "remove-definition":
		return "Remove the VM definition. Disks and backups are kept."
	case "autostart":
		if f.VM.Key.ConnectionID == "qemu:///session" {
			return "Applies when your user libvirt service starts, not directly at host boot."
		}
		return "Applies when the system libvirt service starts, usually during host startup."
	case "resources":
		return "VM must be stopped and persistent. Changes apply next boot."
	case "capture":
		return "VM must be stopped. Save a private recovery point."
	case "guest-tools":
		return "QEMU guest agent improves VM status and shutdown. Installation uses guest sudo."
	case "guest-recipe":
		return "VM must be running. Review setup over SSH before it runs."
	case "repository-init":
		return "Create encrypted storage for your backups."
	case "repository-check":
		return "Check your backup storage for damage or missing data."
	default:
		return "Review the plan before making changes."
	}
}

// guidedBrowseKind describes a field's picker target without reading files or
// changing request validation. The workspace owns opening the picker.
func guidedBrowseKind(fieldName string) string {
	switch fieldName {
	case "repository", "sourceRoot":
		return "directory"
	case "path", "parametersFile", "recipe", "identityFile", "knownHostsFile", "passwordFile":
		return "file"
	default:
		return ""
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
	if f.Kind == "remove-definition" {
		return f.updateRemoval(key)
	}
	if key.Type == tea.KeyEsc {
		return f, false, true
	}
	if len(f.Fields) == 0 {
		f.Error = "Choose a supported form."
		return f, false, false
	}
	if f.Kind == "autostart" {
		return f.updateAutostart(key)
	}
	if f.Kind == "guest-tools" && len(f.Fields) == 7 {
		if f.Focus == 1 || f.Focus == 6 {
			f.ToolsAdvanced = true
		}
		if f.Fields[0].Value != "windows" {
			rows := f.guestToolsRows()
			switch key.Type {
			case tea.KeyTab, tea.KeyDown, tea.KeyShiftTab, tea.KeyUp:
				at := max(0, slices.Index(rows, f.Focus))
				delta := 1
				if key.Type == tea.KeyShiftTab || key.Type == tea.KeyUp {
					delta = -1
				}
				f.Focus = rows[(at+delta+len(rows))%len(rows)]
				return f, false, false
			}
			if key.Type == tea.KeyEnter && f.Focus >= 0 && f.Focus < len(f.Fields) {
				at := max(0, slices.Index(rows, f.Focus))
				f.Focus = rows[(at+1)%len(rows)]
				f.Error = ""
				return f, false, false
			}
			if f.Focus == len(f.Fields) {
				if key.Type == tea.KeyEnter || key.Type == tea.KeySpace || key.Type == tea.KeyLeft || key.Type == tea.KeyRight {
					f.ToolsAdvanced = !f.ToolsAdvanced
					f.Error = ""
				}
				return f, false, false
			}
			if f.Focus == len(f.Fields)+1 {
				if key.Type == tea.KeySpace {
					key = tea.KeyMsg{Type: tea.KeyEnter}
				}
				if key.Type != tea.KeyEnter {
					return f, false, false
				}
			}
		}
	}
	maxFocus := len(f.Fields) - 1
	if f.Kind == "guest-tools" && len(f.Fields) == 7 {
		maxFocus = len(f.Fields) + 1
	}
	f.Focus = max(0, min(f.Focus, maxFocus))
	if f.Kind == "guest-tools" && f.Fields[0].Value == "windows" {
		f.Focus = 0
		if key.Type != tea.KeyLeft && key.Type != tea.KeyRight && key.Type != tea.KeySpace {
			return f, false, false
		}
	}
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
				if f.Kind == "guest-tools" && (field == 1 || field == 6) {
					f.ToolsAdvanced = true
				}
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
	if len(field.Choices) > 0 {
		if key.Type == tea.KeyLeft || key.Type == tea.KeyRight || key.Type == tea.KeySpace {
			i := slices.Index(field.Choices, field.Value)
			step := 1
			if key.Type == tea.KeyLeft {
				step = -1
			}
			field.Value = field.Choices[(i+step+len(field.Choices))%len(field.Choices)]
		}
		return f, false, false
	}
	if field.Toggle {
		if key.Type == tea.KeySpace || key.Type == tea.KeyLeft || key.Type == tea.KeyRight {
			field.Value = strconv.FormatBool(field.Value != "true")
		}
		return f, false, false
	}
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
	case "remove-definition":
		if f.Fields[0].Toggle || len(f.Fields[0].Choices) != 0 || values["confirmation"] != f.VM.Name {
			return fail("confirmation", "Type the VM name exactly as shown before previewing removal.")
		}
		r.ID, r.Action = f.VM.Key.UUID, "remove"
		return "vm.remove", r, -1, nil
	case "autostart":
		if !f.Fields[0].Toggle || len(f.Fields[0].Choices) != 0 || values["enabled"] != "true" && values["enabled"] != "false" {
			return fail("enabled", "Choose On or Off using the requested startup toggle.")
		}
		enabled := values["enabled"] == "true"
		if enabled == f.VM.Autostart {
			return fail("enabled", "No change selected. Toggle the requested setting or go back.")
		}
		r.ID, r.Action, r.Input["enabled"] = f.VM.Key.UUID, "autostart", enabled
		return "vm.plan", r, -1, nil
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
	case "guest-recipe", "guest-tools":
		names := []string{"identityFile", "knownHostsFile"}
		if f.Kind == "guest-recipe" {
			names = append(names, "recipe")
		}
		if f.Kind == "guest-tools" && values["profile"] == "windows" {
			return fail("profile", "Windows: use the reviewed VirtIO driver ISO and guest installer. Automatic SSH installation is unavailable; see guest tools catalog.")
		}
		if f.Kind == "guest-tools" && f.VM.State != "running" {
			return fail("address", "Start this VM before installing guest tools. Guest SSH and an existing agent channel are required.")
		}
		for _, name := range names {
			if !guidedPath(values[name]) {
				if f.Kind == "guest-tools" {
					if name == "identityFile" {
						return fail(name, "Choose the guest login's private SSH key with Ctrl+O. Credential references use absolute file paths.")
					}
					return fail(name, "Choose a known_hosts file with this guest's verified fingerprint using Ctrl+O. Use absolute file paths.")
				}
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
		if f.Kind == "guest-tools" {
			if !slices.Contains([]string{"linux-auto", "debian", "ubuntu", "fedora"}, values["profile"]) || values["desktop"] != "true" && values["desktop"] != "false" {
				return fail("profile", "Choose supported guest tools.")
			}
			r.ID, r.Action = f.VM.Key.UUID, "install"
			r.Input = map[string]any{"profile": values["profile"], "desktop": values["desktop"] == "true", "address": values["address"], "port": float64(port), "user": values["user"], "identityFile": values["identityFile"], "knownHostsFile": values["knownHostsFile"]}
			return "guest.tools.install", r, -1, nil
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
	if f.Kind == "remove-definition" {
		return f.removalView(width, height)
	}
	if f.Kind == "autostart" {
		return f.autostartView(width, height)
	}
	if f.Kind == "guest-tools" && len(f.Fields) > 0 && f.Fields[0].Value == "windows" {
		lines := []string{clip(f.Title()), clip("> Guest system: < Windows (manual) >"), "",
			clip("Install inside Windows using trusted VirtIO driver media."),
			clip("1. Open the driver disc already attached to your VM."),
			clip("2. Run its signed guest-tools installer as administrator."),
			clip("Need a disc? Include the ISO when creating a new VM,"),
			clip("or attach it to an existing VM through libvirt."), "",
			clip("Automatic Windows installation is not available."),
			clip("Left/Right Change system   Esc Back")}
		return strings.Join(lines[:min(height, len(lines))], "\n")
	}
	if f.Kind == "guest-tools" && len(f.Fields) == 7 {
		return f.guestToolsView(width, height)
	}
	lines := []string{clip(f.Title()), clip(f.note())}
	footer := []string{}
	if f.Error != "" {
		footer = append(footer, clip("Error: "+f.Error))
	}
	keys := "[Enter Preview]  Tab Next field  Esc Back"
	if width < 40 {
		keys = "Enter Preview | Esc Back"
	}
	footer = append(footer, clip(keys))
	if len(f.Fields) == 0 {
		lines = append(lines, clip("No fields; reopen a supported form."))
	} else {
		focus := max(0, min(f.Focus, len(f.Fields)-1))
		hint := f.Fields[focus].Hint
		if guidedBrowseKind(f.Fields[focus].Name) != "" {
			hint = "Ctrl+O Browse | " + hint
		}
		count := max(1, height-len(lines)-len(footer)-1)
		first := max(0, focus-count+1)
		labelWidth := 0
		for _, field := range f.Fields {
			labelWidth = max(labelWidth, ansi.StringWidth(clip(field.Label)))
		}
		labelWidth = min(labelWidth, width-12)
		inputWidth := width - labelWidth - 6
		for i := first; i < min(len(f.Fields), first+count); i++ {
			field := f.Fields[i]
			prefix := "  "
			if i == focus {
				prefix = "> "
			}
			label := ansi.Truncate(clip(field.Label), labelWidth, "")
			label += strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(label)))
			value := validation.SafeText(field.Value)
			if i == focus && len(field.Choices) == 0 && !field.Toggle {
				runes := []rune(value)
				cursor := max(0, min(field.Cursor, len(runes)))
				before := string(runes[:cursor])
				if cells := ansi.StringWidth(before); cells >= inputWidth {
					before = "…" + ansi.Cut(before, cells-(inputWidth-2), cells)
				}
				value = before + "|" + string(runes[cursor:])
			}
			if len(field.Choices) > 0 {
				lines = append(lines, clip(prefix+label+": < "+value+" >"))
			} else if field.Toggle {
				mark := " "
				if value == "true" {
					mark = "x"
				}
				lines = append(lines, clip(prefix+"["+mark+"] "+field.Label))
			} else {
				lines = append(lines, clip(prefix+label+": ["+ansi.Truncate(value, inputWidth, "")+"]"))
			}
		}
		lines = append(lines, clip(hint))
	}
	lines = append(lines, footer...)
	return strings.Join(lines[:min(height, len(lines))], "\n")
}

// SetViewport supplies input-time dimensions for the removal issue reader.
// View stays pure; other guided forms do not use this state.
func (f *GuidedForm) SetViewport(width, height int) {
	f.viewportWidth, f.viewportHeight = width, height
}

func (f *GuidedForm) syncRemovalIssue() {
	if f.removalIssueText != f.Error {
		f.removalIssueText, f.removalIssueOffset, f.removalIssueOpen = f.Error, 0, false
	}
}

func (f GuidedForm) updateRemoval(key tea.KeyMsg) (GuidedForm, bool, bool) {
	f.syncRemovalIssue()
	if f.removalIssueOpen {
		width, height := f.viewportWidth, f.viewportHeight
		if width <= 0 {
			width = 80
		}
		if height <= 0 {
			height = 17
		}
		page := max(1, height-4)
		last := max(0, len(wrap(validation.SafeText(f.Error), width))-page)
		f.removalIssueOffset = min(f.removalIssueOffset, last)
		switch key.Type {
		case tea.KeyEsc:
			f.removalIssueOpen = false
		case tea.KeyUp:
			f.removalIssueOffset = max(0, f.removalIssueOffset-1)
		case tea.KeyDown:
			f.removalIssueOffset = min(last, f.removalIssueOffset+1)
		case tea.KeyPgUp:
			f.removalIssueOffset = max(0, f.removalIssueOffset-page)
		case tea.KeyPgDown:
			f.removalIssueOffset = min(last, f.removalIssueOffset+page)
		case tea.KeyHome:
			f.removalIssueOffset = 0
		case tea.KeyEnd:
			f.removalIssueOffset = last
		}
		return f, false, false
	}
	if key.Type == tea.KeyEsc {
		return f, false, true
	}
	if key.Type == tea.KeyF1 && f.Error != "" {
		f.removalIssueOpen, f.removalIssueOffset = true, 0
		return f, false, false
	}
	if len(f.Fields) != 1 || f.Fields[0].Name != "confirmation" || f.Fields[0].Toggle || len(f.Fields[0].Choices) != 0 {
		f.Error = "The removal form is incomplete; reopen it."
		f.syncRemovalIssue()
		return f, false, false
	}
	f.Focus = max(0, min(f.Focus, 1))
	switch key.Type {
	case tea.KeyTab, tea.KeyDown, tea.KeyShiftTab, tea.KeyUp:
		f.Focus = 1 - f.Focus
		return f, false, false
	}
	if f.Focus == 0 {
		if key.Type == tea.KeyEnter {
			f.Focus = 1
			return f, false, false
		}
		// Reuse the bounded text editor without its Enter-to-preview behavior.
		editor := GuidedForm{Fields: slices.Clone(f.Fields)}
		if key.Type == tea.KeyCtrlU {
			editor.Fields[0].Value, editor.Fields[0].Cursor = "", 0
		} else {
			editor, _, _ = editor.Update(key)
		}
		f.Fields, f.Error = editor.Fields, editor.Error
		f.syncRemovalIssue()
		return f, false, false
	}
	if key.Type == tea.KeyEnter || key.Type == tea.KeySpace {
		if _, _, _, err := f.request(f.VM.Key.ConnectionID); err != nil {
			f.Error, f.Focus = err.Error(), 0
			f.syncRemovalIssue()
			return f, false, false
		}
		f.Error = ""
		f.syncRemovalIssue()
		return f, true, false
	}
	return f, false, false
}

func (f GuidedForm) removalView(width, height int) string {
	f.syncRemovalIssue()
	if f.removalIssueOpen {
		rows := wrap(validation.SafeText(f.Error), width)
		count := max(1, height-4)
		offset := min(f.removalIssueOffset, max(0, len(rows)-count))
		end := min(len(rows), offset+count)
		lines := append([]string{"Removal issue", ""}, rows[offset:end]...)
		lines = append(lines, fmt.Sprintf("Lines %d–%d of %d", offset+1, end, len(rows)), "Up/Down Scroll   PgUp/PgDn Page   Esc Back to settings")
		return strings.Join(pageLines(lines, width, height, 0), "\n")
	}
	lines := []string{f.Title()}
	lines = append(lines, wrap("VM: "+validation.SafeText(f.VM.Name), width)...)
	lines = append(lines, "UUID: "+f.VM.Key.UUID, "State: "+validation.SafeText(f.VM.State))
	lines = append(lines, wrap(f.note(), width)...)
	lines = append(lines, wrap("The saved VM configuration will be removed. Back up or export it first if needed.", width)...)
	value := ""
	if len(f.Fields) == 1 {
		value = validation.SafeText(f.Fields[0].Value)
		if f.Focus == 0 {
			runes := []rune(value)
			cursor := max(0, min(f.Fields[0].Cursor, len(runes)))
			before := string(runes[:cursor])
			room := max(3, width-19)
			if cells := ansi.StringWidth(before); cells >= room {
				before = "…" + ansi.Cut(before, cells-(room-2), cells)
			}
			value = before + "|" + string(runes[cursor:])
		}
	}
	inputMark, previewMark := "> ", "  "
	if f.Focus == 1 {
		inputMark, previewMark = "  ", "> "
	}
	lines = append(lines, "", inputMark+"Type VM name: ["+ansi.Truncate(value, max(1, width-18), "…")+"]", previewMark+"[ Preview ]")
	if f.Error != "" {
		lines = append(lines, pageLines([]string{"Issue: " + validation.SafeText(f.Error)}, width, 2, 0)...)
		lines = append(lines, "F1 Read full issue and recovery instructions")
	}
	lines = append(lines, "Tab Select  Enter Next/Preview  Ctrl-U Clear  Esc Back")
	return strings.Join(pageLines(lines, width, height, 0), "\n")
}

func (f GuidedForm) updateAutostart(key tea.KeyMsg) (GuidedForm, bool, bool) {
	f.Focus = max(0, min(f.Focus, 1))
	switch key.Type {
	case tea.KeyTab, tea.KeyDown, tea.KeyShiftTab, tea.KeyUp:
		f.Focus = 1 - f.Focus
		return f, false, false
	}
	if len(f.Fields) != 1 || f.Fields[0].Name != "enabled" || !f.Fields[0].Toggle || len(f.Fields[0].Choices) != 0 {
		f.Error = "The startup form is incomplete; reopen it."
		return f, false, false
	}
	if f.Focus == 0 {
		if key.Type == tea.KeyEnter || key.Type == tea.KeySpace || key.Type == tea.KeyLeft || key.Type == tea.KeyRight {
			f.Fields[0].Value = strconv.FormatBool(f.Fields[0].Value != "true")
			f.Error = ""
		}
		return f, false, false
	}
	if key.Type == tea.KeyEnter || key.Type == tea.KeySpace {
		if _, _, _, err := f.request(f.VM.Key.ConnectionID); err != nil {
			f.Error, f.Focus = err.Error(), 0
			return f, false, false
		}
		f.Error = ""
		return f, true, false
	}
	return f, false, false
}

func (f GuidedForm) autostartView(width, height int) string {
	current, requested := "Off", "Unavailable"
	if f.VM.Autostart {
		current = "On"
	}
	if len(f.Fields) == 1 {
		switch f.Fields[0].Value {
		case "true":
			requested = "On"
		case "false":
			requested = "Off"
		}
	}
	lines := []string{f.Title(), "VM: " + validation.SafeText(f.VM.Name)}
	lines = append(lines, wrap(f.note(), width)...)
	lines = append(lines, "Changes startup policy only; does not start or stop the VM now.", "", "Current: "+current)
	toggleMark, previewMark := "> ", "  "
	if f.Focus == 1 {
		toggleMark, previewMark = "  ", "> "
	}
	lines = append(lines, toggleMark+"Requested: < "+requested+" >", previewMark+"[ Preview ]", "")
	if f.Error != "" {
		lines = append(lines, wrap("Issue: "+validation.SafeText(f.Error), width)...)
	}
	lines = append(lines, "Tab/Arrows Select   Enter/Space Choose   Esc Back")
	return strings.Join(pageLines(lines, width, height, 0), "\n")
}

// Wire field indexes remain stable for the workspace's file picker.
func (f GuidedForm) guestToolsRows() []int {
	rows := []int{0, 2, 3, 4, 5, 7}
	if f.ToolsAdvanced || f.Focus == 1 || f.Focus == 6 {
		rows = append(rows, 1, 6)
	}
	return append(rows, 8)
}

func (f GuidedForm) guestToolsView(width, height int) string {
	clean := func(s string) string {
		return ansi.Truncate(strings.NewReplacer("\n", " ", "\t", " ").Replace(validation.SafeText(s)), width, "…")
	}
	lines := []string{"Install guest tools", "Adds QEMU guest agent for VM status and graceful shutdown."}
	if f.VM.State != "running" {
		lines = append(lines, "Start this VM first, then return here.")
	} else {
		lines = append(lines, "Needs guest SSH, passwordless sudo and package repository access.")
	}
	lines = append(lines, "Guest tools run inside this VM; no host software is installed.", "")
	footer := []string{}
	if f.Error != "" {
		footer = append(footer, wrap("Issue: "+validation.SafeText(f.Error), width)...)
	}
	footer = append(footer, "Tab/Arrows Select   Enter Next/Choose   Ctrl+O Browse   Esc Back")
	if len(footer) > height/3 {
		footer = append(footer[:max(1, height/3-1)], footer[len(footer)-1])
	}
	rows := f.guestToolsRows()
	at := max(0, slices.Index(rows, f.Focus))
	count := max(1, height-len(lines)-len(footer)-2)
	first := max(0, at-count+1)
	for _, index := range rows[first:min(len(rows), first+count)] {
		label := ""
		if index == 7 {
			label = "[ Advanced: desktop tools and SSH port ]"
			if f.Fields[1].Value == "true" || f.Fields[6].Value != "22" {
				desktop := "off"
				if f.Fields[1].Value == "true" {
					desktop = "on"
				}
				label = "[ Advanced: desktop " + desktop + ", SSH " + f.Fields[6].Value + " ]"
			}
			if f.ToolsAdvanced {
				label = "[ Hide advanced options ]"
			}
		} else if index == 8 {
			label = "[ Preview installation ]"
		} else {
			field := f.Fields[index]
			value := validation.SafeText(field.Value)
			if index == 0 {
				labels := map[string]string{"linux-auto": "Detect Linux (recommended)", "debian": "Debian", "ubuntu": "Ubuntu", "fedora": "Fedora", "windows": "Windows (manual)"}
				value = labels[value]
				if value == "" {
					value = "Choose a supported system"
				}
				label = field.Label + ": < " + value + " >"
			} else if field.Toggle {
				mark := " "
				if value == "true" {
					mark = "x"
				}
				label = "[" + mark + "] " + field.Label
			} else {
				room := max(4, width-ansi.StringWidth(field.Label)-7)
				if index == f.Focus {
					runes := []rune(value)
					cursor := max(0, min(field.Cursor, len(runes)))
					before := string(runes[:cursor])
					cells := ansi.StringWidth(before)
					if cells >= room {
						before = "…" + ansi.Cut(before, cells-room+2, cells)
					}
					value = before + "|" + string(runes[cursor:])
				}
				label = field.Label + ": [" + ansi.Truncate(value, room, "…") + "]"
			}
		}
		prefix := "  "
		if index == f.Focus {
			prefix = "> "
		}
		lines = append(lines, prefix+label)
	}
	hint := "Review the packages and permissions before installation."
	if f.Focus >= 0 && f.Focus < len(f.Fields) {
		hint = f.Fields[f.Focus].Hint
	}
	if f.Focus == 7 {
		hint = "Optional SPICE desktop tools and a different SSH port."
	}
	lines = append(lines, wrap(validation.SafeText(hint), width)...)
	if len(lines) > height-len(footer) {
		lines = lines[:height-len(footer)]
	}
	lines = append(lines, footer...)
	for i := range lines {
		lines[i] = clean(lines[i])
	}
	return strings.Join(lines, "\n")
}
