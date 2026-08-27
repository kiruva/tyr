package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCapsOverlayOpensAndCloses(t *testing.T) {
	m := newSized(t)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	if m.(Model).mode != modeCaps {
		t.Fatal("'C' should open the capabilities overlay")
	}

	view := m.View()
	for _, want := range []string{"capabilities", ".7z", ".rar", "Remote (SSH)"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay does not mention %q", want)
		}
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.(Model).mode != modeNormal {
		t.Fatal("any key should close the overlay")
	}
}

// The two overlays are each other's escape hatch: the footers advertise it.
func TestHelpAndCapsSwap(t *testing.T) {
	m := newSized(t)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !strings.Contains(m.View(), "C capabilities") {
		t.Error("help overlay does not point at the capabilities overlay")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	if m.(Model).mode != modeCaps {
		t.Fatal("'C' in help should switch to capabilities")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if m.(Model).mode != modeHelp {
		t.Fatal("'?' in capabilities should switch back to help")
	}
}

// 'C' must not be swallowed by the copy binding on 'c'.
func TestCapsKeyIsNotCopy(t *testing.T) {
	m := newSized(t)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.(Model).mode == modeCaps {
		t.Fatal("'c' should be copy, not capabilities")
	}
}
