package tui

import (
	"strings"
	"testing"

	"virmill.local/core/internal/ui"
)

func TestActionLanguageCoversRegistry(t *testing.T) {
	labels := map[string]string{}
	registered := map[string]bool{}
	for _, action := range ui.Actions {
		t.Run(action.Command, func(t *testing.T) {
			registered[action.Command] = true
			text, ok := actionText[action.Command]
			if !ok {
				t.Fatal("registered action needs explicit task language")
			}
			if other, duplicate := labels[text.label]; duplicate {
				t.Errorf("same label as %q", other)
			}
			labels[text.label] = action.Command
			if strings.EqualFold(text.label, action.Command) && strings.Count(action.Command, " ") > 1 {
				t.Errorf("label repeats multi-part CLI command: %q", text.label)
			}
			if len(text.label) < 6 || len(text.label) > 40 || strings.TrimSpace(text.label) != text.label {
				t.Errorf("expected concise task label: %q", text.label)
			}
			if len(text.description) < 20 || len(text.description) > 110 || !strings.HasSuffix(text.description, ".") {
				t.Errorf("expected one concise explanatory sentence: %q", text.description)
			}
			if actionLabel(action) != text.label || actionDescription(action) != text.description {
				t.Fatal("action lookup did not use explicit text")
			}
		})
	}
	for command := range actionText {
		if !registered[command] {
			t.Errorf("obsolete action language for %q", command)
		}
	}
}

func TestActionLanguageExplainsDisruptionAndRecovery(t *testing.T) {
	for command, phrase := range map[string]string{
		"vm creation cleanup": "delete failed-creation volumes",
		"plugin remove":       "keep its data",
		"vm stop":             "never forces power off",
		"vm reboot":           "without a forced-stop fallback",
		"operation reconcile": "without repeating",
		"snapshot restore":    "new disconnected VM",
		"operation cancel":    "safe boundary",
		"vm save":             "not an independent backup",
	} {
		if !strings.Contains(actionDescription(ui.Action{Command: command}), phrase) {
			t.Errorf("%s must explain %q", command, phrase)
		}
	}
}

func TestUnknownActionLanguageDoesNotExposeUntrustedCommand(t *testing.T) {
	a := ui.Action{Command: "\x1b[2J", Summary: "\x1b[2J"}
	if actionLabel(a) != "Unrecognized action" || strings.Contains(actionDescription(a), "\x1b") {
		t.Fatal("unknown action leaked command text")
	}
}
