package tui

// helpLines is the keyboard guide, grouped by what you want to do. It fits an
// 80x24 terminal without scrolling.
func (m Workspace) helpLines() []string {
	arrows := "↑/↓"
	if m.ASCII {
		arrows = "Up/Down"
	}
	row := func(label, text string) string { return padCell(label, 11) + text }
	return []string{
		"Keyboard guide", "",
		row("Sections", "1 Overview  2 VMs  3 Networks  4 Storage  5 Templates  6 Labs"),
		row("", "7 Protection  8 Devices  9 Jobs  0 Plugins  , Settings"),
		row("Moving", "Tab moves between the list, buttons and sections"),
		row("", "Esc goes back; q or Ctrl+C quits (running jobs continue)"),
		row("Lists", arrows+" select, Enter opens details, / searches, r refreshes"),
		row("", "x in details shows every field and the libvirt XML"),
		row("Tasks", "a opens more tasks for this page; A there shows advanced tools"),
		row("", ": opens All tools, every task in every section"),
		row("VMs", "s start  t shut down  b restart  p pause  u resume"),
		row("", "e CPU/RAM  c capture  g guest tools  i import"),
		row("Safety", "Every change opens a review first; nothing applies until you confirm"),
	}
}
