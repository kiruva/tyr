package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/app/keymap"
	"github.com/kiruva/tyr/internal/config"
)

// focusAction puts the editor's cursor on a named action.
func focusAction(t *testing.T, m tea.Model, id string) tea.Model {
	t.Helper()
	app := m.(Model)
	index, ok := keymap.ActionIndex(id)
	if !ok {
		t.Fatalf("no such action: %s", id)
	}
	app.keyEdit = app.keyEdit.MoveTo(index)
	return app
}

// openKeys starts the app with an isolated config and opens the key editor.
func openKeys(t *testing.T) tea.Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	m := newInDir(t, t.TempDir())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	if got := m.(Model).mode; got != modeKeys {
		t.Fatalf("mode = %v, want modeKeys", got)
	}
	return m
}

// Rebinding an action takes effect at once and is written to the config.
func TestRebindKey(t *testing.T) {
	m := focusAction(t, openKeys(t), "sort")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // "press a key…"
	if !m.(Model).keyEdit.Capturing() {
		t.Fatal("enter should put the editor into capture")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})

	app := m.(Model)
	if status, refused := app.keyEdit.Status(); refused {
		t.Fatalf("refused: %s", status)
	}
	if got := app.keys.Sort.Keys(); len(got) != 1 || got[0] != "z" {
		t.Fatalf("sort is bound to %v, want [z]", got)
	}

	cfg, err := config.Keys()
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if got := cfg["sort"]; len(got) != 1 || got[0] != "z" {
		t.Fatalf("config has sort = %v, want [z]", got)
	}

	// And the new key works in the pane.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	cycled := m.(Model)
	if got := cycled.panes[0].SortModeLabel(); got != "size" {
		t.Errorf("sort mode = %q, want the rebound key to have cycled it", got)
	}
}

// A key that already belongs to something else is refused, not stolen.
func TestRebindRefusesADuplicate(t *testing.T) {
	m := focusAction(t, openKeys(t), "sort")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}}) // already "view"

	app := m.(Model)
	if _, refused := app.keyEdit.Status(); !refused {
		t.Fatal("binding a key that was taken was allowed")
	}
	if status, _ := app.keyEdit.Status(); !strings.Contains(status, "view") {
		t.Errorf("status = %q, want it to name what has the key", status)
	}
	if got := app.keys.Sort.Keys()[0]; got != "s" {
		t.Errorf("sort moved to %q anyway", got)
	}
}

// The keys that get you out of things cannot be given away.
func TestRebindRefusesReservedKeys(t *testing.T) {
	m := focusAction(t, openKeys(t), "sort")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	app := m.(Model)
	if _, refused := app.keyEdit.Status(); !refused {
		t.Fatal("ctrl+c was accepted as a binding")
	}
	if app.mode != modeKeys {
		t.Errorf("mode = %v — the editor should still be open", app.mode)
	}
}

// a adds a second key to an action rather than replacing what is there.
func TestAddSecondKey(t *testing.T) {
	m := focusAction(t, openKeys(t), "sort")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})

	got := m.(Model).keys.Sort.Keys()
	if len(got) != 2 || got[0] != "s" || got[1] != "z" {
		t.Fatalf("sort is bound to %v, want [s z]", got)
	}
}

// d puts one action back, D puts everything back.
func TestResetBindings(t *testing.T) {
	m := focusAction(t, openKeys(t), "sort")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if got := m.(Model).keys.Sort.Keys(); len(got) != 1 || got[0] != "s" {
		t.Fatalf("sort = %v after reset, want [s]", got)
	}
	if cfg, _ := config.Keys(); len(cfg) != 0 {
		t.Errorf("config still holds %v", cfg)
	}

	// Rebind two things, then throw the lot away.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = focusAction(t, m, "view")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	if got := len(m.(Model).keys.Overrides()); got != 2 {
		t.Fatalf("%d actions rebound, want 2", got)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	if got := len(m.(Model).keys.Overrides()); got != 0 {
		t.Fatalf("%d actions still rebound after D", got)
	}
}

// The config's bindings are in force from the first keypress of the session.
func TestConfigBindingsApplyAtStartup(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cfg := config.Config{Keys: map[string][]string{"sort": {"z"}, "select": {"space"}}}
	var m tea.Model = New().WithSession(cfg)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	started := m.(Model)
	if got := started.panes[0].SortModeLabel(); got != "size" {
		t.Errorf("sort = %q, want the config's key to have worked", got)
	}

	// "space" in the file means the space bar.
	app := m.(Model)
	if got := app.keys.Select.Keys(); len(got) != 1 || got[0] != " " {
		t.Errorf("select is bound to %q, want the space bar", got)
	}
}

// An action name the app has never heard of is said out loud rather than
// silently doing nothing.
func TestUnknownActionIsReported(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := New().WithSession(config.Config{Keys: map[string][]string{"teleport": {"z"}}})
	if !strings.Contains(m.noticeText, "teleport") {
		t.Errorf("noticeText = %q, want it to name the unknown action", m.noticeText)
	}
}

// The help overlay shows whatever the keys currently are, not what they were.
func TestHelpFollowsRebinding(t *testing.T) {
	m := focusAction(t, openKeys(t), "sort")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	view := m.(Model).View()
	if !strings.Contains(view, "z") {
		t.Errorf("the help does not show the new key:\n%s", view)
	}
}
