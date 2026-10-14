package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// tab in the help overlay is the way to the capabilities overlay; it has no
// key of its own out of the box.
func openCaps(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.(Model).mode != modeCaps {
		t.Fatal("tab in help should open the capabilities overlay")
	}
	return m
}

func TestCapsOverlayOpensAndCloses(t *testing.T) {
	m := openCaps(t, newSized(t))

	view := m.View()
	for _, want := range []string{"capabilities", ".7z", ".rar", "Remote (SSH)", "strip image metadata"} {
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
	if !strings.Contains(m.View(), "tab capabilities") {
		t.Error("help overlay does not point at the capabilities overlay")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.(Model).mode != modeCaps {
		t.Fatal("tab in help should switch to capabilities")
	}
	if !strings.Contains(m.View(), "tab keys") {
		t.Error("capabilities overlay does not point back at help")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.(Model).mode != modeHelp {
		t.Fatal("tab in capabilities should switch back to help")
	}
}

// 'C' is convert now, and 'c' is still copy: neither opens capabilities.
func TestCapsHasNoDefaultKey(t *testing.T) {
	for _, r := range []rune{'c', 'C'} {
		m, _ := newSized(t).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if m.(Model).mode == modeCaps {
			t.Fatalf("%q opened capabilities", r)
		}
	}
}
