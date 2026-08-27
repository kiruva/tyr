package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/app/keymap"
)

// onKeyEditKey hands the keypress to the editor and translates what it reports
// back into the root model's terms. The editor takes the bindings in and gives
// them back, so a rebind reaches the rest of the app without keymap needing to
// know the app exists — the same shape the theme picker uses.
func (m Model) onKeyEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	editor, keys, res := m.keyEdit.Update(msg, m.keys)
	m.keyEdit, m.keys = editor, keys

	if res.Outcome == keymap.Close {
		m.mode = modeNormal
	}
	return m, res.Cmd
}
