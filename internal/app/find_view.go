package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/pane"
	"github.com/kiruva/tyr/internal/ui"
)

// findLabelW is the label column in the find form, wide enough for "contains".
const findLabelW = 10

// renderFind draws whichever stage of the tool is current.
func (m Model) renderFind() string {
	switch m.find.stage {
	case findStageRunning:
		return m.renderFindRunning()
	case findStageResults:
		return m.renderFindResults()
	default:
		return m.renderFindForm()
	}
}

// renderFindForm draws the query: two fields and two switches.
func (m Model) renderFindForm() string {
	lines := []string{
		ui.DialogTitle.Render("Find"),
		"",
		ui.Faint.Render("in " + truncTail(m.find.root, contentWidth-3)),
		"",
		findRow("name", m.find.name.View(), m.find.focus == findFieldName),
		findRow("contains", m.find.content.View(), m.find.focus == findFieldContent),
		"",
		findSwitch("match case", m.find.caseSense, m.find.focus == findFieldCase),
		findSwitch("hidden files", m.find.hidden, m.find.focus == findFieldHidden),
	}
	if m.find.status != "" {
		lines = append(lines, "", ui.Danger.Render(truncTail(m.find.status, contentWidth)))
	}
	lines = append(lines, "",
		ui.Faint.Render("* ? [] globs · plain text matches anywhere"),
		ui.DialogHint.Render("enter")+" search    "+
			ui.DialogHint.Render("tab")+" field    "+
			ui.DialogHint.Render("esc")+" close")

	return ui.Dialog.Width(dialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// findRow lays out one labelled text field.
func findRow(label, field string, focused bool) string {
	name := padRight(label, findLabelW)
	if focused {
		return ui.HelpKey.Render(name) + ui.AddrEdit.Width(contentWidth-findLabelW).Render(field)
	}
	return ui.Faint.Render(name) + field
}

// findSwitch lays out one on/off row.
func findSwitch(label string, on, focused bool) string {
	box := "[ ]"
	if on {
		box = "[×]"
	}
	row := box + " " + label
	if focused {
		return ui.Cursor.Render("▸ " + row)
	}
	return "  " + row
}

// renderFindRunning is what is on screen while the walk runs.
func (m Model) renderFindRunning() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		ui.DialogTitle.Render("Find"),
		"",
		"searching "+truncTail(m.find.root, contentWidth-10)+"…",
		"",
		ui.DialogHint.Render("esc")+" cancel",
	)
	return ui.Dialog.Width(dialogWidth).Render(content)
}

// renderFindResults lists the hits, scrolled to keep the cursor in view.
func (m Model) renderFindResults() string {
	const rowW = dialogWidth + 20 // the hits are paths; give them more room

	start := 0
	if m.find.cursor >= findRows {
		start = m.find.cursor - findRows + 1
	}
	end := min(start+findRows, len(m.find.results))

	lines := []string{
		ui.DialogTitle.Render("Find — " + m.findSummary()),
		"",
	}
	for i := start; i < end; i++ {
		lines = append(lines, findResultRow(m, i, rowW))
	}
	lines = append(lines, "",
		ui.DialogHint.Render("enter")+" go there    "+
			ui.DialogHint.Render("/")+" narrow    "+
			ui.DialogHint.Render("esc")+" close")

	return ui.Dialog.Width(rowW).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// findResultRow renders one hit: its path, then its size or the line it matched.
func findResultRow(m Model, i, width int) string {
	r := m.find.results[i]

	name := r.Rel
	if r.IsDir {
		name += "/"
	}
	detail := pane.HumanSize(r.Size)
	if r.Line > 0 {
		detail = fmt.Sprintf("%d: %s", r.Line, r.Text)
	}

	inner := width - 2*dialogPadX - 2
	name = truncTail(name, max(inner/2, 8))
	detail = truncate(detail, max(inner-lipgloss.Width(name)-1, 0))
	gap := strings.Repeat(" ", max(inner-lipgloss.Width(name)-lipgloss.Width(detail), 1))

	if i == m.find.cursor {
		return ui.Cursor.Render("▸ " + name + gap + detail)
	}
	if r.IsDir {
		name = ui.DirName.Render(name)
	}
	return "  " + name + gap + ui.Faint.Render(detail)
}

// truncate keeps the head of a string, marking what it cut.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	r := []rune(s)
	return string(r[:w-1]) + "…"
}
