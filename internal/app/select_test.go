package app

import (
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// selected is the active pane's marked set, ordered for comparison.
func selected(m Model) []string {
	names := m.panes[m.active].SelectedNames()
	sort.Strings(names)
	return names
}

func TestSelectAllAndInvert(t *testing.T) {
	dir := t.TempDir()
	seedFiles(t, dir, "a.txt", "b.txt", "c.txt")
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if got := selected(m.(Model)); len(got) != 3 {
		t.Fatalf("after ctrl+a: %v, want all three", got)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'*'}})
	inverted := m.(Model)
	if got := inverted.panes[0].SelectedCount(); got != 0 {
		t.Fatalf("after inverting a full selection: %d marked, want 0", got)
	}
}

// The mask prompt marks what matches and leaves the rest alone.
func TestSelectByMask(t *testing.T) {
	dir := t.TempDir()
	seedFiles(t, dir, "one.go", "two.go", "three.md")
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	opened := m.(Model)
	if opened.mode != modeSelectMask {
		t.Fatalf("mode = %v, want modeSelectMask", opened.mode)
	}
	if !strings.Contains(opened.View(), "Select by mask") {
		t.Error("the mask prompt is not on screen")
	}

	// The field is prefilled with "*"; replace it.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = typeKeys(t, m, "*.go")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", app.mode)
	}
	got := selected(app)
	if len(got) != 2 || got[0] != "one.go" || got[1] != "two.go" {
		t.Fatalf("selected = %v, want the two .go files", got)
	}

	// And "-" takes them back off.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = typeKeys(t, m, "one*")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := selected(m.(Model)); len(got) != 1 || got[0] != "two.go" {
		t.Fatalf("selected = %v, want [two.go]", got)
	}
}

// Esc closes the prompt without touching the selection.
func TestSelectMaskCancel(t *testing.T) {
	dir := t.TempDir()
	seedFiles(t, dir, "a.txt")
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	app := m.(Model)
	if app.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", app.mode)
	}
	if app.panes[0].SelectedCount() != 0 {
		t.Error("cancelling the prompt still marked something")
	}
}
