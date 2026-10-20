package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/ui"
)

// The modal dialogs keep one size across every stage, so moving between them
// does not make the box jump around. dialogWidth is the outer box; contentWidth
// is what is left after the dialog style's horizontal padding, and is what the
// rows inside must fit into.
const (
	dialogWidth  = 58
	dialogPadX   = 3
	contentWidth = dialogWidth - 2*dialogPadX
)

// formRow lays out one field of a form dialog: label, value, and a faint note
// about it pushed to the right edge. The focused field's label carries the
// cursor.
func formRow(focused bool, label, value, note string) string {
	name := "  " + ui.PadRight(label, packLabelW-2)
	if focused {
		name = ui.Cursor.Render(ui.PadRight("▸ "+label, packLabelW))
	}
	row := name + value
	if note == "" {
		return row
	}
	gap := max(contentWidth-lipgloss.Width(row)-lipgloss.Width(note), 1)
	return row + strings.Repeat(" ", gap) + ui.Faint.Render(note)
}

// moveCursor applies the keys every scrolling list shares — ↑↓ and jk, page
// up and down, home and end — to a cursor over rows 0..last, page rows at a
// time. It reports whether key was one of them.
func moveCursor(cursor *int, key string, last, page int) bool {
	c := *cursor
	switch key {
	case "up", "k":
		c--
	case "down", "j":
		c++
	case "pgup", "ctrl+u":
		c -= page
	case "pgdown", "ctrl+d":
		c += page
	case "home", "g":
		c = 0
	case "end", "G":
		c = last
	default:
		return false
	}
	*cursor = max(min(c, last), 0)
	return true
}
