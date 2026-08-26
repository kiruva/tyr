package app

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/pane"
	"github.com/kiruva/tyr/internal/ui"
)

// propsLabelW is the label column in the properties dialog.
const propsLabelW = 12

// renderProps draws what the entry is, above the one field that can change it.
func (m Model) renderProps() string {
	s := m.props
	info := s.info

	lines := []string{
		ui.DialogTitle.Render(truncTail(s.name, contentWidth)),
		ui.Faint.Render(truncTail(s.path, contentWidth)),
		"",
		propsRow("type", entryKind(info)),
	}

	if s.link != "" {
		lines = append(lines, propsRow("links to", truncTail(s.link, contentWidth-propsLabelW)))
	}
	if info.IsDir() {
		lines = append(lines, propsRow("size", dirSizeLabel(m)))
	} else {
		lines = append(lines, propsRow("size", fmt.Sprintf("%s  (%d bytes)", pane.HumanSize(info.Size()), info.Size())))
	}

	lines = append(lines, propsRow("modified", info.ModTime().Format("2006-01-02 15:04:05")))
	if s.owner != "" {
		lines = append(lines, propsRow("owner", s.owner+":"+s.group))
	}
	if s.links > 1 {
		lines = append(lines, propsRow("hard links", fmt.Sprintf("%d", s.links)))
	}
	lines = append(lines, propsRow("mode", info.Mode().String()))

	lines = append(lines, "", propsModeRow(m))
	if info.IsDir() {
		lines = append(lines, propsRecursiveRow(m))
	}

	if s.status != "" {
		lines = append(lines, "", ui.Danger.Render(truncTail(s.status, contentWidth)))
	}
	lines = append(lines, "",
		ui.DialogHint.Render("enter")+" apply    "+ui.DialogHint.Render("esc")+" close")

	return ui.Dialog.Width(dialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// propsRow is one read-only fact.
func propsRow(label, value string) string {
	return ui.Faint.Render(padRight(label, propsLabelW)) + value
}

// propsModeRow is the editable octal field.
func propsModeRow(m Model) string {
	label := padRight("permissions", propsLabelW)
	if m.props.focus == propsFieldMode {
		label = ui.HelpKey.Render(label)
		return label + ui.AddrEdit.Width(8).Render(m.props.mode.View())
	}
	return ui.Faint.Render(label) + m.props.mode.View()
}

// propsRecursiveRow is the switch a directory gets.
func propsRecursiveRow(m Model) string {
	box := "[ ]"
	if m.props.recursive {
		box = "[×]"
	}
	row := box + " apply to everything inside"
	if m.props.focus == propsFieldRecursive {
		return ui.Cursor.Render("▸ " + row)
	}
	return "  " + row
}

// dirSizeLabel is the measured total when the pane has one, and how to get it
// when it does not.
func dirSizeLabel(m Model) string {
	p := &m.panes[m.props.paneIdx]
	if cur, ok := p.Current(); ok && p.DirSizeKnown(cur.Name) {
		for _, e := range p.Entries {
			if e.Name == cur.Name && e.HasSize {
				return pane.HumanSize(e.Size)
			}
		}
	}
	return ui.Faint.Render("not measured — space or = measures it")
}
