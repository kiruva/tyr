package app

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
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

// newInput is a text field as the dialogs use it: no prompt of its own, no
// length limit, a placeholder saying what goes in it.
func newInput(placeholder string, password bool, width int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.CharLimit = 0
	ti.Width = width
	if password {
		ti.EchoMode = textinput.EchoPassword
	}
	return ti
}

// prompt is a one-field dialog: a title, a faint line saying what it is
// about, the field, and what enter does.
type prompt struct {
	title  string
	alarm  bool   // the title is a warning ("Wrong password")
	about  string // shown faint above the field, already cut to fit
	field  textinput.Model
	note   string // shown faint under the field, "" for none
	status string // why the last enter was refused, "" for nothing
	action string // what enter does: "create", "rename", …
	more   string // shown faint after the key hints, "" for none
}

func (p prompt) render() string {
	title := ui.DialogTitle.Render(p.title)
	if p.alarm {
		title = ui.Danger.Render(p.title)
	}
	lines := []string{
		title,
		"",
		ui.Faint.Render(p.about),
		ui.AddrEdit.Width(contentWidth).Render(p.field.View()),
	}
	if p.note != "" {
		lines = append(lines, "", ui.Faint.Render(p.note))
	}
	if p.status != "" {
		lines = append(lines, "", ui.Danger.Render(ui.TruncTail(p.status, contentWidth)))
	}
	hints := ui.DialogHint.Render("enter") + " " + p.action + "    " + ui.DialogHint.Render("esc") + " cancel"
	if p.more != "" {
		hints += ui.Faint.Render("    " + p.more)
	}
	lines = append(lines, "", hints)
	return ui.Dialog.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
