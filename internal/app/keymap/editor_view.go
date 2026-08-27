package keymap

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/ui"
)

// The editor is full-screen: it is one row per action, and there are fifty of
// them. Chrome is a fixed number of lines so the list can be sized from the
// terminal height.
const chromeRows = 4 // header, rule, status, footer

// View draws the keymap, one action per row.
func (e Editor) View(keys Map, width, height int) string {
	rows := max(height-chromeRows, 1)

	start := 0
	if e.cursor >= rows {
		start = e.cursor - rows + 1
	}
	end := min(start+rows, len(specs))

	lines := []string{e.header(keys, width), ui.Faint.Render(strings.Repeat("─", max(width, 1)))}
	for i := start; i < end; i++ {
		lines = append(lines, e.row(keys, width, i, i == e.cursor))
	}
	for i := end - start; i < rows; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, e.footer(width))

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// keyEditHeader says what is being edited, and how much of it has been changed.
func (e Editor) header(keys Map, width int) string {
	changed := len(keys.Overrides())

	left := " keys · " + itemCount(len(specs), "action")
	if changed > 0 {
		left += " · " + itemCount(changed, "change")
	}
	right := "saved to the config file "
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return ui.StatusBar.Width(width).Render(left + strings.Repeat(" ", gap) + right)
}

// keyEditRow is one action: its group, what it does, and what reaches it.
func (e Editor) row(keys Map, width, index int, selected bool) string {
	spec := specs[index]
	bound := keys.boundKeys(spec)

	groupW := 11
	descW := max(width/3, 16)
	keysW := max(width-groupW-descW-6, 10)

	group := ""
	if index == 0 || specs[index-1].group != spec.group {
		group = spec.group
	}

	marker := "  "
	if selected {
		marker = "▸ "
		if e.stage != stageList {
			marker = "◆ "
		}
	}

	label := keyList(bound)
	if e.stage != stageList && selected {
		label = "press a key…"
		if e.stage == stageAdd {
			label = "press a key to add…"
		}
	}

	line := marker + ui.PadRight(ui.TruncTail(group, groupW), groupW) +
		ui.PadRight(ui.TruncTail(spec.desc, descW), descW) + ui.TruncTail(label, keysW)

	switch {
	case selected:
		return ui.Cursor.Width(width).Render(line)
	case sameKeys(bound, spec.def):
		return line
	default:
		// A rebound action is worth spotting at a glance.
		return ui.Selected.Render(line)
	}
}

// keyEditFooter is the legend, or whatever the editor has to say.
func (e Editor) footer(width int) string {
	if e.status != "" {
		bar := ui.NoticeBar
		if e.fault {
			bar = ui.ErrorBar
		}
		return bar.Width(width).Render(" " + ui.TruncTail(e.status, width-1))
	}
	if e.stage != stageList {
		return ui.NoticeBar.Width(width).Render(" press the key you want · esc to think again")
	}

	left := " enter rebind · a add a key · d default · D all defaults"
	right := "esc close "
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return ui.StatusBar.Width(width).Render(left + strings.Repeat(" ", gap) + right)
}

// itemCount is "3 changes" / "1 change".
func itemCount(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
