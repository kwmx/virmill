package tui

import (
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

// ADR 0061: a running VM's next-boot CPU and RAM can be changed in place; the
// screen says when the change applies and still offers a reviewed shutdown.
func TestResourceEditorRunningEditsNextBootInPlace(t *testing.T) {
	m := resourceWorkspace(t, func(r *domain.VMResourceView) {
		r.State = "running"
		r.RequiresShutdown = true
		r.Live = &domain.ResourceValues{VCPUs: resourceNumber(2), MemoryBytes: resourceNumber(2048 << 20)}
	})
	view := m.View()
	for _, text := range []string{"Live", "Next boot", "Requested CPU cores: [", "Preview changes", "Preview graceful shutdown", "keeps its current values until it shuts down"} {
		if !strings.Contains(view, text) {
			t.Fatal(text, view)
		}
	}
	if strings.Contains(view, "Shut down first") {
		t.Fatal("running VM told to shut down before editing", view)
	}
	g := *m.Resources
	g.Form.Fields[0].Value = "6"
	r, err := g.request(m.Connection)
	if err != nil || r.Action != "set" || r.Input["vcpus"] != float64(6) || r.Input["applyMode"] != "next-boot" {
		t.Fatal(r, err)
	}
}

// ADR 0068: a running VM that has room to change offers it on the same screen,
// compared with what it is running with rather than with its next-boot values.
func TestResourceEditorOffersAChangeWhileTheVMRuns(t *testing.T) {
	m := resourceWorkspace(t, func(r *domain.VMResourceView) {
		r.State = "running"
		r.RequiresShutdown = true
		r.ApplyModes = []string{"next-boot", "now"}
		r.CanChangeLiveCPU, r.CanChangeLiveMemory, r.MemoryBalloon = true, true, "virtio"
		// Running with its full CPU count but ballooned below its memory maximum.
		r.Live = &domain.ResourceValues{VCPUs: resourceNumber(4), MaximumVCPUs: resourceNumber(6),
			MemoryBytes: resourceNumber(1536 << 20), MaximumMemoryBytes: resourceNumber(4096 << 20)}
	})
	view := m.View()
	for _, text := range []string{"Change while it runs", "applies CPU cores and RAM to the running VM now"} {
		if !strings.Contains(view, text) {
			t.Fatal(text, view)
		}
	}
	g := *m.Resources
	// The fields hold the next-boot values, so 4096 MiB is a change for the
	// running VM, which is at 1536, and no change at all for the next boot.
	if _, err := g.request(m.Connection); err == nil {
		t.Fatal("an unchanged next-boot edit was accepted")
	}
	r, err := g.liveRequest(m.Connection)
	if err != nil || r.Input["memoryMiB"] != float64(4096) || r.Input["applyMode"] != "now" || r.Input["vcpus"] != nil {
		t.Fatal(r, err)
	}
	g.Form.Fields[0].Value = "5"
	if r, err = g.liveRequest(m.Connection); err != nil || r.Input["vcpus"] != float64(5) {
		t.Fatal(r, err)
	}
}

// What a running VM cannot change says why, in the words of the thing to change.
func TestResourceEditorSaysWhyALiveChangeIsNotOffered(t *testing.T) {
	m := resourceWorkspace(t, func(r *domain.VMResourceView) {
		r.State = "running"
		r.RequiresShutdown = true
		r.LiveCPUReason = "This VM is running all 2 of its CPUs: it was not started with spare CPU slots"
		r.LiveMemoryReason = "This VM has no memory balloon, so its memory can only change at its next boot"
		r.Live = &domain.ResourceValues{VCPUs: resourceNumber(2), MemoryBytes: resourceNumber(2048 << 20)}
	})
	view := m.View()
	for _, text := range []string{"running all 2 of its CPUs", "no memory balloon"} {
		if !strings.Contains(view, text) {
			t.Fatal(text, view)
		}
	}
	if strings.Contains(view, "Change while it runs") {
		t.Fatal("a refused live change was offered", view)
	}
	if _, err := m.Resources.liveRequest(m.Connection); err == nil {
		t.Fatal("a refused live change built a request")
	}
}
