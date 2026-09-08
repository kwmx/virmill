package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const actionParametersLimit = 1 << 20

// ActionForm is the catalog fallback for commands without a dedicated form.
// selectedID is already scoped by the caller to this action's resource type;
// it is never inferred from a VM, used as a path, or sent as shell text.
type ActionForm struct {
	Action ui.Action
	Form   GuidedForm
}

func actionParameterFields(a ui.Action) bool {
	return a.Argument == "parameters" || a.Mutation == "set" || a.Mutation == "autostart" ||
		a.Method == "guest.recipe.run" || a.Method == "storage.access.grant" || a.Method == "snapshot.create" || a.Method == "snapshot.restore" ||
		a.Method == "vm.recovery.auxiliary.inspect" || a.Method == "backup.verify-manifest" || a.Method == "backup.policy.preview" ||
		a.Method == "vm.create" || a.Method == "vm.creation.cleanup" || a.Method == "vm.creation.accept" ||
		(a.Mutation != "" && (strings.HasPrefix(a.Command, "plugin ") || strings.HasPrefix(a.Command, "import ") || strings.HasPrefix(a.Command, "backup ")))
}

func NewActionForm(action ui.Action, selectedID string) (ActionForm, error) {
	if !slices.Contains(ui.Actions, action) {
		return ActionForm{}, domain.Fail("INVALID_INPUT", "select an action from the current catalog")
	}
	f := ActionForm{Action: action, Form: GuidedForm{Kind: "catalog-action"}}
	field := func(name, label, hint string, limit int) {
		f.Form.Fields = append(f.Form.Fields, GuidedField{Name: name, Label: label, Hint: hint, Limit: limit})
	}
	switch action.Argument {
	case "id":
		field("id", "Resource ID", "Choose the resource this task should use", 1024)
		if guidedPrintable(selectedID) && len(selectedID) <= 1024 {
			f.Form.Fields[0].Value, f.Form.Fields[0].Cursor = selectedID, len([]rune(selectedID))
		}
	case "path":
		field("path", actionPathLabel(action), "Choose a file or type its path", 4096)
	case "", "parameters":
	default:
		return ActionForm{}, domain.Fail("UNSUPPORTED_CAPABILITY", "this catalog argument needs a dedicated form")
	}
	if action.Method == "guest.recipe.run" {
		field("path", "Recipe file", "Canonical absolute path to the GuestRecipe file", 4096)
	}
	if action.Method == "plugin.call" {
		field("pluginAction", "Plugin action ID", "Exact action exposed by the selected plugin", 1024)
	}
	if action.Method == "operation.watch" {
		field("after", "Event cursor (optional)", "Nonnegative sequence number; blank starts at zero", 19)
	}
	if actionParameterFields(action) {
		label, hint := "Settings file (optional)", "Extra settings from a JSON file"
		if strings.HasPrefix(action.Method, "import.prepare") {
			label, hint = "Import settings", "Choose settings for the destination and disks"
		}
		field("parametersFile", label, hint, 4096)
	}
	return f, nil
}

func (f ActionForm) Title() string { return actionLabel(f.Action) }

func (f ActionForm) Update(key tea.KeyMsg) (ActionForm, bool, bool) {
	if key.Type != tea.KeyEnter {
		var submit, cancel bool
		f.Form, submit, cancel = f.Form.Update(key)
		return f, submit, cancel
	}
	_, _, index, err := f.request("qemu:///system")
	if err != nil {
		f.Form.Error = err.Error()
		if index >= 0 {
			f.Form.Focus = index
		}
		return f, false, false
	}
	f.Form.Error = ""
	return f, true, false
}

func (f ActionForm) View(width, height int) string {
	view := f.Form.View(width, height)
	if width < 24 || height < 6 {
		return view
	}
	clean := func(s string) string {
		s = strings.NewReplacer("\n", " ", "\t", " ").Replace(validation.SafeText(s))
		return ansi.Truncate(s, width, "")
	}
	lines := strings.Split(view, "\n")
	if len(lines) > 0 {
		lines[0] = clean(f.Title())
	}
	if len(lines) > 1 {
		lines[1] = clean(actionDescription(f.Action))
	}
	if len(f.Form.Fields) == 0 && len(lines) > 2 {
		lines[2] = clean("No input is required for this action.")
	}
	// Read-only catalog actions and explicit job controls don't create a plan.
	// A mutation is always labeled as a separate preview, never an apply.
	if len(lines) > 0 {
		label := "Tab fields | Enter continue | Esc cancel"
		if f.Action.Mutation != "" {
			label = "Tab fields | Enter preview | Esc cancel"
		}
		if width < 40 {
			label = "Enter continue; Esc cancel"
			if f.Action.Mutation != "" {
				label = "Enter preview; Esc cancel"
			}
		}
		lines[len(lines)-1] = clean(label)
	}
	return strings.Join(lines, "\n")
}

func (f ActionForm) Request(connection string) (string, app.Request, error) {
	method, request, _, err := f.request(connection)
	return method, request, err
}

func (f ActionForm) request(connection string) (string, app.Request, int, error) {
	fail := func(name, message string) (string, app.Request, int, error) {
		index := slices.IndexFunc(f.Form.Fields, func(field GuidedField) bool { return field.Name == name })
		return "", app.Request{}, index, domain.Fail("INVALID_INPUT", message)
	}
	expected, err := NewActionForm(f.Action, "")
	if err != nil || connection == "" || !guidedPrintable(connection) || len(connection) > 1024 || len(f.Form.Fields) != len(expected.Form.Fields) {
		return fail("", "Choose a current catalog action and explicit connection.")
	}
	values := make(map[string]string, len(f.Form.Fields))
	for i, field := range f.Form.Fields {
		if field.Name != expected.Form.Fields[i].Name || len(field.Value) > expected.Form.Fields[i].Limit || !guidedPrintable(field.Value) {
			return fail(field.Name, "A field contains unsupported or excessive text.")
		}
		values[field.Name] = field.Value
	}
	r := app.Request{Connection: connection, Action: f.Action.Mutation, Input: map[string]any{}}
	if f.Action.Argument == "id" {
		if id := values["id"]; id == "" || strings.TrimSpace(id) != id {
			return fail("id", "Enter the exact stable ID required by this action.")
		}
		r.ID = values["id"]
	}
	if _, present := values["path"]; present {
		if !guidedPath(values["path"]) {
			return fail("path", "Enter a canonical absolute file or directory path.")
		}
		r.Path = values["path"]
	}
	if file := values["parametersFile"]; file != "" {
		if !guidedPath(file) {
			return fail("parametersFile", "Parameters file must be a canonical absolute path.")
		}
		data, err := readActionParameters(file)
		if err != nil {
			if typed, ok := err.(*domain.Error); ok && (typed.Code == "SOURCE_CHANGED" || typed.Code == "UNSUPPORTED_CAPABILITY") {
				index := slices.IndexFunc(f.Form.Fields, func(field GuidedField) bool { return field.Name == "parametersFile" })
				message := "The parameters file changed while being read; inspect it before trying again."
				if typed.Code == "UNSUPPORTED_CAPABILITY" {
					message = "Safe parameter-file observation requires Linux and a filesystem with complete ordinary-file identity support."
				}
				return "", app.Request{}, index, domain.Fail(typed.Code, message)
			}
			return fail("parametersFile", "Parameters file must be a stable, readable, single-link ordinary file of at most 1 MiB; symlinks and special files are refused.")
		}
		if err := wire.Decode(data, &r.Input); err != nil || r.Input == nil {
			return fail("parametersFile", "Parameters file must contain one JSON object of input fields, without duplicate keys or trailing values.")
		}
	}
	if f.Action.Method == "plugin.call" {
		id := values["pluginAction"]
		if id == "" || strings.TrimSpace(id) != id {
			return fail("pluginAction", "Enter the exact plugin action ID.")
		}
		if _, duplicate := r.Input["action"]; duplicate {
			return fail("parametersFile", "Plugin action ID is already supplied by its labeled field; omit action from the parameters file.")
		}
		r.Input["action"] = id
	}
	if value := values["after"]; value != "" {
		after, err := strconv.ParseInt(value, 10, 64)
		if err != nil || after < 0 || strconv.FormatInt(after, 10) != value {
			return fail("after", "Event cursor must be a canonical nonnegative integer.")
		}
		r.After = after
	}
	return f.Action.Method, r, -1, nil
}

func actionPathLabel(a ui.Action) string {
	switch a.Command {
	case "network create":
		return "Network definition"
	case "plugin new":
		return "New project folder"
	case "plugin pack", "plugin validate", "plugin test":
		return "Project folder"
	case "plugin install", "plugin update":
		return "Plugin package"
	case "lab validate":
		return "Lab file"
	case "config validate":
		return "Configuration file"
	case "import prepare":
		return "OVA file"
	case "import prepare-install":
		return "ISO file"
	case "import prepare-disks":
		return "Source folder"
	case "import verify":
		return "Image manifest"
	case "backup verify-manifest":
		return "Recovery manifest"
	default:
		return "File or folder"
	}
}
