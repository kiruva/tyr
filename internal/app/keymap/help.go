package keymap

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/ui"
)

// Help draws the keybinding overlay, generated from the keymap — which is why
// it lives here rather than with the rest of the app's views: adding an action
// to specs is all it takes to get it listed.
//
// The scroll offset is a parameter because it is shared with the capabilities
// overlay; the two swap into each other and the root model owns the position.
func Help(k Map, width, rows, scroll int) string {
	groups := k.groups()
	blocks := make([]string, 0, len(groups))
	for _, g := range groups {
		lines := []string{ui.HelpKey.Render(g.title)}
		for _, b := range g.binds {
			h := b.Help()
			lines = append(lines, "  "+ui.HelpKey.Render(ui.PadRight(h.Key, 9))+ui.Faint.Render(h.Desc))
		}
		blocks = append(blocks, lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	cols := ui.TwoColumns(blocks)
	body, more := ui.ScrollBlock(cols, rows, scroll)

	header := ui.DialogTitle.Render("tyr — keys")
	footer := ui.Faint.Render("C capabilities · any key to close")
	if more {
		footer = ui.Faint.Render("↑/↓ more · C capabilities · any key to close")
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	return ui.Dialog.Render(content)
}
