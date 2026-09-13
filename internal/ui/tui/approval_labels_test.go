package tui

import (
	tea "github.com/charmbracelet/bubbletea"
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

func TestEnterConfirmationStartsAtTopAfterScrollingPlan(t *testing.T) {
	m := Workspace{Plan: &domain.Plan{Acknowledgements: []string{"host-mutation"}}, Approved: []bool{false}, Offset: 39, Width: 80, Height: 24}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Workspace)
	if !got.Reviewing || got.Offset != 0 || got.AckIndex != 0 {
		t.Fatalf("confirmation inherited plan scroll: %+v", got)
	}
	if !strings.Contains(got.View(), "Confirm reviewed changes") {
		t.Fatal(got.View())
	}
}
