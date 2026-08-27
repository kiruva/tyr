// Package caps is the capabilities overlay: what the tools installed on this
// machine let tyr actually do.
//
// It is a pure renderer — no state, no key handling. The root model keeps the
// scroll offset (shared with the help overlay) and passes it in. Splitting a
// view out with nothing but a function signature is the cheapest kind of
// extraction there is, and it still takes 160 lines of layout arithmetic out of
// the root package.
package caps

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/ui"
)

// View draws the overlay: what tyr can do on this machine, and which tool is
// missing where it cannot. It is the companion to the help overlay — help lists
// the keys, this lists what those keys can reach.
//
// The scroll offset is a parameter rather than state here, because it is shared
// with the help overlay: the two swap into each other with C and ?, and a
// scroll position that survived the swap would be confusing. The root model
// owns it for exactly that reason.
func View(width, rows, scroll int) string {
	caps := fileops.Capabilities()
	nameW, detailW := capWidths(width, caps)

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

	cols := ui.TwoColumns(blocks)

	header := ui.DialogTitle.Render("tyr — capabilities")
	if s := capSummary(caps); s != "" {
		header += "  " + ui.Faint.Render(s)
	}
	body, more := ui.ScrollBlock(cols, rows, scroll)

	footer := ui.Faint.Render("? keys · any key to close")
	if more {
		footer = ui.Faint.Render("↑/↓ more · ? keys · any key to close")
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	return ui.Dialog.Render(content)
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
	name := ui.PadRight(ui.TruncHead(c.Name, nameW-1), nameW)
	return "  " + style.Render(mark) + " " + name + ui.Faint.Render(ui.TruncHead(detail, detailW))
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
