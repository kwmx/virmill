package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestConsoleOpenRequiresInteractiveTerminal(t *testing.T) {
	var output bytes.Buffer
	command := New(nil, &output, &output)
	command.SetArgs([]string{"vm", "console", "open", "12345678-1234-4234-8234-123456789abc", "--non-interactive"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatal(err)
	}
}
