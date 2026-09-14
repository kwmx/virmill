package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

type jobRefreshPulse struct{}

func jobRefreshTick() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return jobRefreshPulse{} })
}
func (m *Workspace) refreshJobs() tea.Cmd {
	if m.Quit {
		return nil
	}
	cmds := []tea.Cmd{jobRefreshTick()}
	if m.activityMatches() && len(m.Activity.Events) < 1000 && !domain.Terminal(field(m.Detail, "state")) && m.Activity.Error == "" {
		cmds = append(cmds, m.refreshJobActivity())
	}
	if m.Pending["jobs"] == 0 {
		cmds = append(cmds, m.request("jobs", "operation.list", app.Request{}))
	}
	if m.Section == 8 && m.Detail != nil && field(m.Detail, "state") != "" && m.Pending["job-update"] == 0 && m.Pending["detail"] == 0 {
		cmds = append(cmds, m.request("job-update", "operation.get", app.Request{ID: resourceID(m.Detail)}))
	}
	return tea.Batch(cmds...)
}
func (m Workspace) jobButtons() []workspaceButton {
	state := field(m.Detail, "state")
	out := []workspaceButton{}
	if m.creationNetworkJobMatches() {
		out = append(out, workspaceButton{"Back to VM setup", "creation-network-return"})
	}
	if o := m.currentJobOutcome(); o != nil {
		if o.Error != "" {
			out = append(out, workspaceButton{"Refresh result", "job-refresh-result"})
		} else if !o.Loading && o.VMID != "" {
			out = append(out, workspaceButton{"Open VM", "job-open-vm"})
		}
	}
	out = append(out, workspaceButton{"Activity", "job-activity"})
	if !domain.Terminal(state) && state != "interrupted" && state != "reconciling" {
		out = append(out, workspaceButton{"Cancel job", "action:operation cancel"})
	}
	if state == "interrupted" || state == "recovery-required" {
		out = append(out, workspaceButton{"Check recovery", "action:operation reconcile"})
	}
	if _, prepared := m.PreparedBasics[resourceID(m.Detail)]; state == "succeeded" && prepared {
		out = append(out, workspaceButton{"Create VM", "action:vm create"})
	}
	return append(out, workspaceButton{"More", "a"}, workspaceButton{"Back", "esc"})
}
func (m Workspace) jobDetails(width int) []string {
	j := m.Detail
	state := field(j, "state")
	status, next := "Working", "You can leave this page; the job continues in the background."
	switch state {
	case "queued", "validating":
		status = "Preparing"
	case "verifying":
		status = "Checking the result"
	case "succeeded":
		status = "Completed"
		next = "The requested operation completed. Guest boot and readiness are separate checks."
	case "failed":
		status = "Failed"
		next = "Read the error below before trying again. Open More for available recovery tools."
	case "partial":
		status = "Partly completed"
		next = "Some changes remain. Inspect the result and recovery options before starting another attempt."
	case "recovery-required", "interrupted":
		status = "Needs attention"
		next = "Use Check recovery to inspect what happened. It does not blindly repeat the operation."
	case "cancel-requested", "canceling":
		status = "Cancel requested"
		next = "Waiting for a safe stopping point. Keep existing files and resources until the result is known."
	case "canceled":
		status = "Canceled"
		next = "The job stopped. Review any retained files or partial resources before removing them."
	default:
		if state != "running" {
			status = "Job state: " + state
		}
	}
	if o := m.currentJobOutcome(); o != nil {
		switch {
		case o.Loading:
			next = "Checking the completed result..."
		case o.Error != "":
			next = "The job completed, but its result could not be checked. Choose Refresh result."
		case o.Title != "":
			status, next = o.Title, o.Summary
		}
	}
	lines := []string{"Job / " + status, "", next, ""}
	if label := operationLabel(field(j, "operation")); label != "" {
		lines = append(lines, "Task: "+label)
		if target := m.jobTarget(j); target != "" {
			lines = append(lines, "For: "+target)
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, field(j, "createdAt")); err == nil {
		lines = append(lines, "Started: "+t.Local().Format("Jan 2 15:04")+" ("+ago(field(j, "createdAt"), clock())+")")
	}
	lines = append(lines, "Operation ID: "+resourceID(j), "Status: "+state)

	if o := m.currentJobOutcome(); o != nil && o.Error != "" {
		lines = append(lines, "", "Result issue: "+o.Error)
	}
	if field(j, "cancelRequested") == "true" {
		lines = append(lines, "Cancellation has been requested.")
	}
	if e := object(j)["error"]; e != nil {
		lines = append(lines, "", "What happened", field(e, "message"), "Error code: "+field(e, "code"))
		for _, v := range array(object(e)["safeNextActions"]) {
			if x, ok := v.(string); ok {
				lines = append(lines, "Next: "+x)
			}
		}
	}
	if m.Errors["job-update"] != "" {
		lines = append(lines, "", "Could not refresh this job. Showing its last known state.", m.Errors["job-update"])
	} else {
		lines = append(lines, "", "Updates automatically. Activity shows recorded progress and errors.")
	}
	out := []string{}
	for _, line := range lines {
		out = append(out, wrap(strings.TrimSpace(line), width)...)
	}
	return out
}
