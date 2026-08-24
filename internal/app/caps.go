package app

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/ui"
)

// renderCaps draws the capabilities overlay: what tyr can do on this machine,
// and which tool is missing where it cannot. It is the companion to the help
// overlay — help lists the keys, this lists what those keys can reach.
func (m Model) renderCaps() string {
	caps := fileops.Capabilities()
	nameW, detailW := capWidths(m.width, caps)

	blocks := make([]string, 0, 8)
	var lines []string
	group := ""
	for _, c := range caps {
		if c.Group != group {
			if len(lines) > 0 {
				blocks = append(blocks, lipgloss.JoinVertical(lipgloss.Left, lines...))
			}
			group = c.Group
			lines = []string{ui.HelpKey.Render(group)}
		}
		lines = append(lines, capLine(c, nameW, detailW))
	}
	if len(lines) > 0 {
		blocks = append(blocks, lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	// Split the groups across two columns by line count, not by group count:
	// the groups are very different sizes, and balancing on count alone makes
	// one column twice as tall as the other.
	mid := balancePoint(blocks)
	cols := lipgloss.JoinHorizontal(lipgloss.Top,
		joinBlocks(blocks[:mid]), "     ", joinBlocks(blocks[mid:]))

	header := ui.DialogTitle.Render("tyr — capabilities")
	if s := capSummary(caps); s != "" {
		header += "  " + ui.Faint.Render(s)
	}
	footer := ui.Faint.Render("? keys · any key to close")
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", cols, "", footer)
	return ui.Dialog.Render(content)
}

// balancePoint is the group to break the two columns at, chosen so the taller
// column is as short as it can be.
func balancePoint(blocks []string) int {
	total := 0
	for _, b := range blocks {
		total += lipgloss.Height(b) + 1 // + the blank line between groups
	}

	best, bestDiff, run := 1, total, 0
	for i, b := range blocks {
		run += lipgloss.Height(b) + 1
		diff := 2*run - total // left minus right
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			best, bestDiff = i+1, diff
		}
	}
	return best
}

// capWidths splits the terminal into the two columns the overlay renders in, so
// a long tool hint is cut rather than pushed through the dialog border. The
// budget is the window minus the dialog's border and padding (8), the gap
// between the columns (5), and each row's marker (4). Names get only the room
// the longest one asks for; whatever is left goes to the detail column.
func capWidths(width int, caps []fileops.Capability) (nameW, detailW int) {
	col := (width - 13) / 2
	for _, c := range caps {
		if w := lipgloss.Width(c.Name) + 1; w > nameW {
			nameW = w
		}
	}
	if room := col - 16; nameW > room {
		nameW = room
	}
	if nameW < 12 {
		nameW = 12
	}
	detailW = col - nameW - 4
	if detailW < 8 { // a narrow window pays for the detail out of the name
		detailW = 8
		nameW = col - 12
	}
	if nameW < 6 {
		nameW = 6
	}
	if over := 4 + nameW + detailW - col; over > 0 { // nothing may leave the column
		detailW -= over
	}
	if detailW < 1 {
		detailW = 1
	}
	return nameW, detailW
}

// capLine renders one capability row: marker, name, and what it depends on.
func capLine(c fileops.Capability, nameW, detailW int) string {
	mark, style := "✓", ui.HelpKey
	switch c.Status() {
	case fileops.StatusPartial:
		mark, style = "~", ui.Faint
	case fileops.StatusMissing:
		mark, style = "✗", ui.Danger
	}
	// The hint about what to install only fits on a wide enough window.
	detail := c.Detail()
	if full := c.DetailFull(); lipgloss.Width(full) <= detailW {
		detail = full
	}
	// nameW-1 keeps a gap between a truncated name and the detail beside it.
	name := padRight(truncHead(c.Name, nameW-1), nameW)
	return "  " + style.Render(mark) + " " + name + ui.Faint.Render(truncHead(detail, detailW))
}

// capSummary counts what is unavailable, so the state of the machine is one
// glance rather than a scan down both columns.
func capSummary(caps []fileops.Capability) string {
	var missing, partial int
	for _, c := range caps {
		switch c.Status() {
		case fileops.StatusMissing:
			missing++
		case fileops.StatusPartial:
			partial++
		}
	}
	switch {
	case missing == 0 && partial == 0:
		return "all available"
	case missing == 0:
		return fmt.Sprintf("%d optional %s missing", partial, tools(partial))
	case partial == 0:
		return fmt.Sprintf("%d unavailable", missing)
	default:
		return fmt.Sprintf("%d unavailable · %d optional %s missing", missing, partial, tools(partial))
	}
}

func tools(n int) string {
	if n == 1 {
		return "tool"
	}
	return "tools"
}
