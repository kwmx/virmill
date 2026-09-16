package cli

import (
	"strings"
	"testing"

	"virmill.local/core/internal/ui"
)

// Multi-word commands become cobra parents plus one leaf, so a command that is
// a strict prefix of another cannot be reached: its leaf swallows the next word
// as a positional argument. `vm disk add dispose` did exactly that to
// `vm disk add` and only failed on the host, never in tests.
func TestNoRegistryCommandIsAPrefixOfAnother(t *testing.T) {
	for _, outer := range ui.Actions {
		for _, inner := range ui.Actions {
			if outer.Command == inner.Command {
				continue
			}
			if strings.HasPrefix(inner.Command, outer.Command+" ") {
				t.Fatalf("%q cannot be reached: %q is already a command", inner.Command, outer.Command)
			}
		}
	}
}
