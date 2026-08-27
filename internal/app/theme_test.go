package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/config"
	"github.com/kiruva/tyr/internal/ui"
)

// newSized builds an app with a usable window, restoring the default theme
// afterwards — ui styles are process-global.
func newSized(t *testing.T) tea.Model {
	t.Helper()
	t.Cleanup(func() { ui.Apply(ui.Themes()[0]) })
	ui.Apply(ui.Themes()[0])

	var m tea.Model = New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestThemePickerAppliesAndPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newSized(t)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if m.(Model).mode != modeTheme {
		t.Fatal("'t' should open the theme picker")
	}
	if !strings.Contains(m.View(), "nord") {
		t.Fatal("picker does not list themes")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if ui.Current().Name != ui.Themes()[1].Name {
		t.Fatalf("moving the cursor should preview: theme = %q", ui.Current().Name)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app := m.(Model)
	if app.mode != modeNormal {
		t.Fatal("enter should close the picker")
	}
	if app.errText != "" {
		t.Fatalf("errText = %q", app.errText)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != ui.Themes()[1].Name {
		t.Fatalf("saved theme = %q, want %q", cfg.Theme, ui.Themes()[1].Name)
	}
}

func TestThemePickerCancelReverts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newSized(t)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.(Model).mode != modeNormal {
		t.Fatal("esc should close the picker")
	}
	if ui.Current().Name != "default" {
		t.Fatalf("theme = %q after cancel, want default", ui.Current().Name)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "" {
		t.Fatalf("cancel wrote %q to the config", cfg.Theme)
	}
}

func TestThemePickerCursorStaysInRange(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newSized(t)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	for range len(ui.Themes()) + 5 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	if got := m.(Model).theme.Cursor(); got != len(ui.Themes())-1 {
		t.Fatalf("theme cursor = %d, want %d", got, len(ui.Themes())-1)
	}
	for range len(ui.Themes()) + 5 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	}
	if got := m.(Model).theme.Cursor(); got != 0 {
		t.Fatalf("theme cursor = %d, want 0", got)
	}
}
