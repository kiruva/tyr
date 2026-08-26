package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/ui"
)

// The editor is full-screen: it is one row per action, and there are fifty of
// them. Chrome is a fixed number of lines so the list can be sized from the
// terminal height.
const keyEditChrome = 4 // header, rule, status, footer

// renderKeyEditor draws the keymap, one action per row.
func (m Model) renderKeyEditor() string {
	rows := max(m.height-keyEditChrome, 1)

	start := 0
	if m.keyEdit.cursor >= rows {
		start = m.keyEdit.cursor - rows + 1
	}
	end := min(start+rows, len(keySpecs))

	lines := []string{m.keyEditHeader(), ui.Faint.Render(strings.Repeat("─", max(m.width, 1)))}
	for i := start; i < end; i++ {
		lines = append(lines, m.keyEditRow(i, i == m.keyEdit.cursor))
	}
	for i := end - start; i < rows; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, m.keyEditFooter())

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// keyEditHeader says what is being edited, and how much of it has been changed.
func (m Model) keyEditHeader() string {
	changed := len(m.keys.overrides())

	left := " keys · " + itemCount(len(keySpecs), "action")
	if changed > 0 {
		left += " · " + itemCount(changed, "change")
	}
	right := "saved to the config file "
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return ui.StatusBar.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

// keyEditRow is one action: its group, what it does, and what reaches it.
func (m Model) keyEditRow(index int, selected bool) string {
	spec := keySpecs[index]
	bound := m.keys.boundKeys(spec)

	groupW := 11
	descW := max(m.width/3, 16)
	keysW := max(m.width-groupW-descW-6, 10)

	group := ""
	if index == 0 || keySpecs[index-1].group != spec.group {
		group = spec.group
	}

	marker := "  "
	if selected {
		marker = "▸ "
		if m.keyEdit.stage != keyEditList {
			marker = "◆ "
		}
	}

	keys := keyList(bound)
	if m.keyEdit.stage != keyEditList && selected {
		keys = "press a key…"
		if m.keyEdit.stage == keyEditAdd {
			keys = "press a key to add…"
		}
	}

	line := marker + padRight(truncTail(group, groupW), groupW) +
		padRight(truncTail(spec.desc, descW), descW) + truncTail(keys, keysW)

	switch {
	case selected:
		return ui.Cursor.Width(m.width).Render(line)
	case sameKeys(bound, spec.def):
		return line
	default:
		// A rebound action is worth spotting at a glance.
		return ui.Selected.Render(line)
	}
}

// keyEditFooter is the legend, or whatever the editor has to say.
func (m Model) keyEditFooter() string {
	if m.keyEdit.status != "" {
		bar := ui.NoticeBar
		if m.keyEdit.fault {
			bar = ui.ErrorBar
		}
		return bar.Width(m.width).Render(" " + truncTail(m.keyEdit.status, m.width-1))
	}
	if m.keyEdit.stage != keyEditList {
		return ui.NoticeBar.Width(m.width).Render(" press the key you want · esc to think again")
	}

	left := " enter rebind · a add a key · d default · D all defaults"
	right := "esc close "
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return ui.StatusBar.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

// itemCount is "3 changes" / "1 change".
func itemCount(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
