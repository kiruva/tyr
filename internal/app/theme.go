package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/app/theme"
)

// onThemeKey hands the keypress to the picker and translates what it reports
// back into the root model's terms. Owning the mode transition here — rather
// than letting the overlay write to m.mode — is what keeps every overlay's
// effect on the app readable from one place.
func (m Model) onThemeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	next, res := m.theme.Update(msg)
	m.theme = next

	if res.Err != nil {
		m.errText = res.Err.Error()
	}
	if res.Outcome == theme.Close {
		m.mode = modeNormal
	}
	return m, res.Cmd
}
