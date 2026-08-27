package theme

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/ui"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// The picker previews as it moves: the surrounding UI is the preview, so the
// theme is applied on every cursor move rather than only on Enter.
func TestMovingTheCursorAppliesThePreview(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	start := ui.Current().Name

	m, res := New().Update(key("j"))
	if res.Outcome != Stay {
		t.Fatalf("Outcome = %v, want Stay", res.Outcome)
	}
	if ui.Current().Name == start {
		t.Error("moving down did not preview the next theme")
	}
	if m.Cursor() != 1 {
		t.Errorf("cursor = %d, want 1", m.Cursor())
	}
}

// Esc puts back whatever was applied when the picker opened, however far the
// preview wandered.
func TestEscapeRestoresTheOriginal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	start := ui.Current().Name

	m := New()
	for range 3 {
		m, _ = m.Update(key("j"))
	}
	_, res := m.Update(key("esc"))

	if res.Outcome != Close {
		t.Fatalf("Outcome = %v, want Close", res.Outcome)
	}
	if got := ui.Current().Name; got != start {
		t.Errorf("theme = %q, want the original %q back", got, start)
	}
}

// The cursor never leaves the list, however long a key is held.
func TestCursorStaysInRange(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	last := len(ui.Themes()) - 1

	m := New()
	for range len(ui.Themes()) + 5 {
		m, _ = m.Update(key("j"))
	}
	if m.Cursor() != last {
		t.Errorf("cursor = %d, want %d", m.Cursor(), last)
	}
	for range len(ui.Themes()) + 5 {
		m, _ = m.Update(key("k"))
	}
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0", m.Cursor())
	}
}

// MoveTo is what a scroll wheel uses, and it clamps the same way.
func TestMoveToClamps(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := New().MoveTo(-10).Cursor(); got != 0 {
		t.Errorf("MoveTo(-10) = %d, want 0", got)
	}
	if got, want := New().MoveTo(9999).Cursor(), len(ui.Themes())-1; got != want {
		t.Errorf("MoveTo(9999) = %d, want %d", got, want)
	}
}

// Enter closes the picker and keeps what is applied.
func TestEnterKeepsTheTheme(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	m, _ := New().Update(key("j"))
	previewed := ui.Current().Name

	_, res := m.Update(key("enter"))
	if res.Outcome != Close {
		t.Fatalf("Outcome = %v, want Close", res.Outcome)
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if got := ui.Current().Name; got != previewed {
		t.Errorf("theme = %q, want the previewed %q kept", got, previewed)
	}
}

// Ctrl+C quits rather than closing the overlay: an overlay is not what the user
// is trying to leave.
func TestCtrlCQuits(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, res := New().Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if res.Outcome != Stay {
		t.Errorf("Outcome = %v, want Stay", res.Outcome)
	}
	if res.Cmd == nil {
		t.Fatal("Cmd = nil, want tea.Quit")
	}
	if _, isQuit := res.Cmd().(tea.QuitMsg); !isQuit {
		t.Error("Cmd is not tea.Quit")
	}
}

// The picker lists every theme by name, so the one being looked for is there to
// find.
func TestViewListsEveryTheme(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	view := New().View()
	for _, th := range ui.Themes() {
		if !strings.Contains(view, th.Name) {
			t.Errorf("the picker does not list %q", th.Name)
		}
	}
}
