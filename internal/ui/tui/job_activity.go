package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// jobActivity contains presentation state only. The workspace owns service
// reads, exact job/connection validation, cursor history and late-reply guards.
type jobActivity struct {
	OperationID, Connection string
	Events                  []domain.Event
	After                   int64
	History                 []int64
	Loading                 bool
	CheckedAt               time.Time
	Error                   string
	Offset, Focus           int
	width, height           int
}

type jobActivityButton struct{ label, intent string }

func (a jobActivity) buttons() []jobActivityButton {
	buttons := []jobActivityButton{{"Refresh", "refresh"}}
	if len(a.History) > 0 {
		buttons = append(buttons, jobActivityButton{"Older events", "older"})
	}
	if len(a.Events) == 1000 {
		buttons = append(buttons, jobActivityButton{"Newer events", "newer"})
	}
	return append(buttons, jobActivityButton{"Back to job", "back"})
}

func (a *jobActivity) SetViewport(width, height int) {
	a.width, a.height = width, height
}

func (a jobActivity) dimensions() (int, int) {
	width, height := a.width, a.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 17
	}
	return width, height
}

func jobActivityText(value string) string {
	return strings.ReplaceAll(validation.SafeText(value), "\t", "    ")
}

func (a jobActivity) content(width int) []string {
	var lines []string
	add := func(text string) { lines = append(lines, wrap(jobActivityText(text), width)...) }
	if a.Error != "" {
		add("Could not read activity. Choose Refresh to try again.")
		add("Issue: " + a.Error)
		if len(a.Events) > 0 {
			add("Previously recorded events are kept below.")
		}
		lines = append(lines, "")
	}
	if a.Loading {
		if len(a.Events) == 0 {
			add("Reading recorded events…")
		} else {
			add("Refreshing activity… Previously recorded events are kept below.")
		}
		lines = append(lines, "")
	}
	if len(a.Events) == 0 {
		if !a.Loading && a.Error == "" {
			if len(a.History) > 0 {
				add("No newer events recorded yet. Refresh, or choose Older events.")
			} else {
				add("No events recorded yet. Choose Refresh to check again.")
			}
		}
		return lines
	}
	events := slices.Clone(a.Events)
	slices.SortStableFunc(events, func(x, y domain.Event) int {
		if x.Seq < y.Seq {
			return -1
		}
		if x.Seq > y.Seq {
			return 1
		}
		return 0
	})
	for i, event := range events {
		if i > 0 {
			lines = append(lines, "")
		}
		stamp := "Time unavailable"
		if !event.At.IsZero() {
			stamp = event.At.UTC().Format("2006-01-02 15:04:05 UTC")
		}
		phase := strings.Join(strings.Fields(jobActivityText(event.Phase)), " ")
		severity := strings.Join(strings.Fields(jobActivityText(event.Severity)), " ")
		if phase == "" {
			phase = "Phase not recorded"
		}
		if severity == "" {
			severity = "Severity not recorded"
		}
		add(stamp + " · " + phase + " · " + severity)
		if event.Message == "" {
			add("No message recorded.")
		} else {
			add(event.Message)
		}
	}
	return lines
}

func (a jobActivity) buttonRows(width int) []string {
	buttons := a.buttons()
	focus := max(0, min(a.Focus, len(buttons)-1))
	var rows []string
	row := ""
	for i, button := range buttons {
		text := "[ " + button.label + " ]"
		if i == focus {
			text = ">" + text
		}
		if row != "" && ansi.StringWidth(row)+2+ansi.StringWidth(text) > width {
			rows = append(rows, row)
			row = ""
		}
		if row != "" {
			row += "  "
		}
		row += text
	}
	if row != "" {
		rows = append(rows, row)
	}
	return rows
}

func (a jobActivity) layout(width, height int) (header, content, footer []string, room int) {
	header = []string{"Job activity"}
	header = append(header, wrap("Job ID: "+jobActivityText(a.OperationID), width)...)
	header = append(header, fmt.Sprintf("Recorded events · Page %d · %d events", len(a.History)+1, len(a.Events)))
	if !a.CheckedAt.IsZero() {
		header[len(header)-1] += " · Checked " + a.CheckedAt.UTC().Format("15:04:05 UTC")
	}
	content = a.content(width)
	footer = a.buttonRows(width)
	footer = append(footer, "Tab/←/→ Buttons · Enter Choose · ↑/↓ Scroll · Esc Back")
	room = max(1, height-len(header)-len(footer)-1)
	return
}

func (a jobActivity) View(width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	if width < 40 || height < 9 {
		return pageLines([]string{"Resize to read job activity.", "The job continues. Esc returns to the job."}, width, height, 0)
	}
	header, content, footer, room := a.layout(width, height)
	start := max(0, min(a.Offset, max(0, len(content)-room)))
	end := min(len(content), start+room)
	lines := append(header, content[start:end]...)
	for len(lines) < height-len(footer)-1 {
		lines = append(lines, "")
	}
	first := 0
	if len(content) > 0 {
		first = start + 1
	}
	lines = append(lines, fmt.Sprintf("Lines %d–%d of %d · PgUp/PgDn · Home/End", first, end, len(content)))
	lines = append(lines, footer...)
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	return lines[:min(height, len(lines))]
}

func (a jobActivity) Update(key tea.KeyMsg) (jobActivity, string) {
	buttons := a.buttons()
	a.Focus = max(0, min(a.Focus, len(buttons)-1))
	width, height := a.dimensions()
	_, content, _, room := a.layout(width, height)
	last := max(0, len(content)-room)
	a.Offset = max(0, min(a.Offset, last))
	switch key.Type {
	case tea.KeyEsc:
		return a, "back"
	case tea.KeyTab, tea.KeyRight:
		a.Focus = (a.Focus + 1) % len(buttons)
	case tea.KeyShiftTab, tea.KeyLeft:
		a.Focus = (a.Focus + len(buttons) - 1) % len(buttons)
	case tea.KeyDown:
		a.Offset = min(last, a.Offset+1)
	case tea.KeyUp:
		a.Offset = max(0, a.Offset-1)
	case tea.KeyPgDown:
		a.Offset = min(last, a.Offset+room)
	case tea.KeyPgUp:
		a.Offset = max(0, a.Offset-room)
	case tea.KeyHome:
		a.Offset = 0
	case tea.KeyEnd:
		a.Offset = last
	case tea.KeyEnter, tea.KeySpace:
		intent := buttons[a.Focus].intent
		if !a.Loading || intent == "back" {
			return a, intent
		}
	}
	return a, ""
}
