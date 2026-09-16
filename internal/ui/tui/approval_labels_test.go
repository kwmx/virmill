package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/domain"
)

const confirmationPlanID = "11111111-2222-4333-8444-555555555555"

func confirmationText(m Workspace) string {
	return strings.Join(strings.Fields(strings.Join(m.confirmationLines(100, 400), " ")), " ")
}

// The confirmation says in plain words everything the user agrees to, keeps an
// identifier visible only where Virmill has no words for it, and shows no plan
// identity (ADR 0065).
func TestConfirmationListsEveryConsequenceInPlainWords(t *testing.T) {
	ids := []string{"host-mutation", "data-loss-delete-disks", "exclusive-storage-writer", "made-up-plugin-scope"}
	m := Workspace{Plan: &domain.Plan{ID: confirmationPlanID, Acknowledgements: ids}}
	text := confirmationText(m)
	for _, id := range ids {
		label, known := plainAcknowledgement(id)
		if !strings.Contains(text, strings.Join(strings.Fields(label), " ")) {
			t.Fatalf("%s is not listed: %s", id, text)
		}
		if known && strings.Contains(text, "["+id+"]") {
			t.Fatalf("%s shows its raw identifier: %s", id, text)
		}
	}
	if !strings.Contains(text, "made-up-plugin-scope") {
		t.Fatal("an unexplained acknowledgement must keep its identifier visible")
	}
	if strings.Contains(text, confirmationPlanID) || !strings.Contains(text, "By confirming, you agree that:") || !strings.Contains(text, "[ Confirm ]") {
		t.Fatal(text)
	}
}

func TestTechnicalPlanIsOneKeyAwayAndEscReturnsToTheConfirmation(t *testing.T) {
	m := Workspace{Plan: &domain.Plan{ID: confirmationPlanID, Acknowledgements: []string{"host-mutation"}}, Offset: 39, Width: 100, Height: 40}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got := next.(Workspace)
	if !got.PlanDetails || got.Offset != 0 || !strings.Contains(got.View(), confirmationPlanID) {
		t.Fatalf("technical details: %s", got.View())
	}
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got = next.(Workspace)
	if got.PlanDetails || got.Plan == nil || !strings.Contains(got.View(), "By confirming, you agree that:") {
		t.Fatal("Esc from technical details should return to the confirmation, not cancel it")
	}
}
