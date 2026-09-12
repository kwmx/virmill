package tui

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/domain"
)

func TestPlanSummaryShowsCreationChoicesBeforeTechnicalDetails(t *testing.T) {
	target := domain.CreationTarget{Spec: domain.CreationSpec{Name: "Learning Linux", VCPUs: 2, MemoryMiB: 4096}}
	p := domain.Plan{Operation: "vm.create.devices-v1", ID: "exact-plan", Digest: strings.Repeat("a", 64), ResourceIDs: []string{"exact-vm-resource"}, Review: map[string]any{"target": target, "guestBootVerified": false}, Risks: []string{"Selected network permits internet access."}, Estimates: domain.Estimates{AdditionalBytes: 3 << 30}}
	// In-process and socket clients must see the same summary.
	var decoded domain.Plan
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []domain.Plan{p, decoded} {
		got := strings.Join(PlanDetails(plan, 80), "\n")
		summary, _, ok := strings.Cut(got, "Complete plan details")
		if !ok {
			t.Fatal(got)
		}
		for _, want := range []string{"Create VM from prepared source", "Nothing has been applied", "VM name: Learning Linux", "CPU cores: 2", "Memory (MiB): 4096", "exact-vm-resource", "3.0 GiB (3221225472 bytes)", "Selected network permits internet access."} {
			if !strings.Contains(summary, want) {
				t.Errorf("missing %q in summary:\n%s", want, summary)
			}
		}
		for _, want := range []string{p.ID, p.Digest, "Guest boot verified: false"} {
			if !strings.Contains(got, want) {
				t.Errorf("lost full review %q", want)
			}
		}
	}
}

func TestPlanSummaryExplainsHardwareChangeAndKeepsEveryWarning(t *testing.T) {
	p := domain.Plan{Operation: "vm.configure-hardware", Review: map[string]any{"requested": map[string]any{"vcpus": float64(4), "memoryMiB": float64(8192), "applyMode": "next-boot"}}, Estimates: domain.Estimates{RequiresDowntime: true}, Risks: []string{"First critical warning.", "Second critical warning."}}
	got := strings.Join(PlanDetails(p, 80), "\n")
	summary, _, _ := strings.Cut(got, "Complete plan details")
	for _, want := range []string{"Edit VM hardware", "CPU cores requested: 4", "Memory requested (MiB): 8192", "Apply mode: next-boot", "Downtime: required", p.Risks[0], p.Risks[1]} {
		if !strings.Contains(summary, want) {
			t.Errorf("missing %q:\n%s", want, summary)
		}
	}
}

func TestPlanSummaryDoesNotInventUnknownOperationEffects(t *testing.T) {
	p := domain.Plan{Operation: "extension.mystery", Review: map[string]any{"target": map[string]any{"spec": map[string]any{"name": "untrusted\x1b[2Jname"}}}}
	got := strings.Join(PlanDetails(p, 80), "\n")
	if !strings.Contains(got, "Review action: extension.mystery") || !strings.Contains(got, "Warnings: none supplied") || strings.Contains(got, "Downtime: not required") || strings.Contains(got, "Extra space estimate") || strings.Contains(got, "\x1b") {
		t.Fatal(got)
	}
	for _, line := range PlanDetails(p, 24) {
		if ansi.StringWidth(line) > 24 {
			t.Fatalf("overflow: %q", line)
		}
	}
}

func TestResourcePlanSummaryShowsExactBeforeAndAfter(t *testing.T) {
	p := domain.Plan{Operation: "vm.configure-resources", Review: map[string]any{
		"requested":       map[string]any{"vcpus": float64(2), "memoryMiB": float64(1), "applyMode": "next-boot"},
		"beforeResources": domain.ResourceValues{VCPUs: resourceNumber(1), MemoryBytes: resourceNumber(1000000)},
		"afterResources":  domain.ResourceValues{VCPUs: resourceNumber(2), MemoryBytes: resourceNumber(1 << 20)},
	}}
	var socket domain.Plan
	b, _ := json.Marshal(p)
	if err := json.Unmarshal(b, &socket); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []domain.Plan{p, socket} {
		summary, _, _ := strings.Cut(strings.Join(PlanDetails(plan, 80), "\n"), "Complete plan details")
		for _, want := range []string{"CPU cores (next boot): 1 -> 2", "RAM (next boot): 1000000 bytes -> 1 MiB", "Apply mode: next-boot"} {
			if !strings.Contains(summary, want) {
				t.Fatal(summary)
			}
		}
		if strings.Contains(summary, "CPU cores requested:") || strings.Contains(summary, "Memory requested (MiB):") {
			t.Fatal("duplicated resource choices", summary)
		}
	}
}

func TestPlanSummarySpaceRoundsUpWithoutOverflow(t *testing.T) {
	for _, tt := range []struct {
		n    uint64
		want string
	}{
		{1, "1 bytes"}, {1 << 20, "1.0 MiB (1048576 bytes)"}, {1<<30 + 1, "1.1 GiB (1073741825 bytes)"}, {math.MaxUint64, "17179869184.0 GiB (18446744073709551615 bytes)"},
	} {
		if got := planSpace(tt.n); got != tt.want {
			t.Errorf("%d: %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestPlanSummaryGuestToolsDoesNotMislabelSudoAsNonRoot(t *testing.T) {
	p := domain.Plan{Operation: "guest.recipe.run", Review: map[string]any{"recipe": map[string]any{"metadata": map[string]any{"name": "guest-tools"}, "spec": map[string]any{"privilege": "sudo"}}}, Risks: []string{"Installs packages as guest administrator."}}
	got := strings.Join(PlanDetails(p, 80), "\n")
	summary, _, _ := strings.Cut(got, "Complete plan details")
	if !strings.Contains(summary, "Guest privilege: sudo") || !strings.Contains(summary, "Installs packages as guest administrator.") || strings.Contains(summary, "non-root recipe") {
		t.Fatal(summary)
	}
}
