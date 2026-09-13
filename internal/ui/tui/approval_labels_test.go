package tui

import (
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
)

func TestConfirmationWrapsCheckboxProseWithoutSplittingIDs(t *testing.T) {
	ids := []string{"host-mutation", "remove-vm-definition", "data-loss-delete-disks", "exclusive-lifecycle-writer", "exclusive-storage-writer"}
	m := Workspace{Plan: &domain.Plan{Acknowledgements: ids}, Approved: make([]bool, len(ids))}
	for i, id := range ids {
		m.AckIndex = i
		lines := m.confirmationLines(80, 18)
		if !strings.Contains(strings.Join(lines, "\n"), id) {
			t.Fatalf("focused acknowledgement %s split or hidden: %q", id, lines)
		}
	}
}
