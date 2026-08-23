package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/config"
	"github.com/kiruva/tyr/internal/ui"
)

// openThemePicker enters the picker, remembering the theme to restore on cancel.
func (m *Model) openThemePicker() {
	m.themeOrig = ui.Current().Name
	m.themeCursor = ui.ThemeIndex(m.themeOrig)
	m.mode = modeTheme
}

// onThemeKey drives the picker. Moving the cursor applies the theme live, so
// the surrounding UI is the preview; Enter keeps it, Esc puts the old one back.
func (m Model) onThemeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	themes := ui.Themes()

	switch msg.String() {
	case "up", "k":
		m.themeCursor = max(m.themeCursor-1, 0)
		ui.Apply(themes[m.themeCursor])
	case "down", "j":
		m.themeCursor = min(m.themeCursor+1, len(themes)-1)
		ui.Apply(themes[m.themeCursor])
	case "home", "g":
		m.themeCursor = 0
		ui.Apply(themes[m.themeCursor])
	case "end", "G":
		m.themeCursor = len(themes) - 1
		ui.Apply(themes[m.themeCursor])
	case "enter":
		ui.Apply(themes[m.themeCursor])
		m.mode = modeNormal
		if err := config.Save(config.Config{Theme: ui.Current().Name}); err != nil {
			m.errText = "theme applied but not saved: " + err.Error()
		}
	case "esc", "q", "t":
		if t, ok := ui.ThemeByName(m.themeOrig); ok {
			ui.Apply(t)
		}
		m.mode = modeNormal
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}
