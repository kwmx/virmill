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
