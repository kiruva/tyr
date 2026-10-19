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
	body, more := ui.ScrollBlock(helpColumns(k), rows, scroll)

	header := ui.DialogTitle.Render("tyr — keys")
	footer := ui.Faint.Render("tab capabilities · any key to close")
	if more {
		footer = ui.Faint.Render("↑/↓ more · tab capabilities · any key to close")
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	return ui.Dialog.Render(content)
}

// HelpHeight is how many lines the overlay's body runs to unscrolled, which is
// what bounds how far it can scroll.
func HelpHeight(k Map) int { return lipgloss.Height(helpColumns(k)) }

// helpColumns is every bound action, grouped, in two balanced columns.
func helpColumns(k Map) string {
	groups := k.groups()
	blocks := make([]string, 0, len(groups))
	for _, g := range groups {
		lines := []string{ui.HelpKey.Render(g.title)}
		for _, b := range g.binds {
			h := b.Help()
			if h.Key == "" {
				continue // unbound: nothing to press
			}
			lines = append(lines, "  "+ui.HelpKey.Render(ui.PadRight(h.Key, 9))+ui.Faint.Render(h.Desc))
		}
		blocks = append(blocks, lipgloss.JoinVertical(lipgloss.Left, lines...))
	}
	return ui.TwoColumns(blocks)
}
