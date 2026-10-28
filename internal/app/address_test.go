package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeKeys(t *testing.T, m tea.Model, s string) tea.Model {
	t.Helper()
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// TestAddressModeJump drives the whole app: open the address bar, clear it,
// type a directory, and confirm both the pane and the render followed.
func TestAddressModeJump(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "target")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	var m tea.Model = New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	if m.(Model).mode != modeAddress {
		t.Fatal("':' should open the address bar")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU}) // clear the seeded path
	m = typeKeys(t, m, sub)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.mode != modeNormal {
		t.Fatalf("mode = %v after a successful jump, want normal", app.mode)
	}
	if app.panes[0].Path != sub {
		t.Fatalf("left pane at %q, want %q", app.panes[0].Path, sub)
	}
	if app.panes[1].Path == sub {
		t.Fatal("right pane should not have moved")
	}
	if !strings.Contains(m.View(), filepath.Base(sub)) {
		t.Fatal("render does not show the new location")
	}
}

// TestAddressModeBadPathStaysOpen keeps the user in the bar with an error.
func TestAddressModeBadPathStaysOpen(t *testing.T) {
	var m tea.Model = New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = typeKeys(t, m, filepath.Join(t.TempDir(), "missing"))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.mode != modeAddress {
		t.Fatal("a bad path should keep the address bar open")
	}
	if !strings.Contains(app.errText, "no such directory") {
		t.Fatalf("errText = %q", app.errText)
	}

	// Esc restores the previous location and leaves address mode.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = m.(Model)
	if app.mode != modeNormal || app.panes[0].EditingPath() {
		t.Fatal("esc should cancel the edit")
	}
}

// ~ in the listing goes straight to the home folder, and ~ typed over the
// address bar's path does the same in two keys.
func TestTildeGoesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // where os.UserHomeDir looks on Windows
	dir := t.TempDir()

	m := typeKeys(t, newInDir(t, dir), "~")
	if got := m.(Model).panes[m.(Model).active].Path; got != home {
		t.Fatalf("~ went to %q, want %q", got, home)
	}

	m = typeKeys(t, newInDir(t, dir), ":~")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.(Model).panes[m.(Model).active].Path; got != home {
		t.Fatalf(": ~ enter went to %q, want %q", got, home)
	}
}
