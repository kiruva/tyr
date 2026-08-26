package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// seedFiles writes empty files into dir.
func seedFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// entryNames is what the given pane is showing.
func entryNames(m Model, idx int) []string {
	out := make([]string, 0, len(m.panes[idx].Entries))
	for _, e := range m.panes[idx].Entries {
		out = append(out, e.Name)
	}
	return out
}

// Typing a filter narrows the pane as the keys land, and Enter keeps it.
func TestFilterNarrowsAsYouType(t *testing.T) {
	dir := t.TempDir()
	seedFiles(t, dir, "main.go", "util.go", "README.md")
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if m.(Model).mode != modeFilter {
		t.Fatalf("mode = %v, want modeFilter", m.(Model).mode)
	}

	m = typeKeys(t, m, ".go")
	if got := entryNames(m.(Model), 0); len(got) != 3 { // "..", main.go, util.go
		t.Fatalf("entries = %v, want the two .go files and ..", got)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app := m.(Model)
	if app.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", app.mode)
	}
	if app.panes[0].Filter() != ".go" {
		t.Errorf("Filter = %q, want .go", app.panes[0].Filter())
	}
	if !strings.Contains(app.View(), "filter:.go") {
		t.Error("the status bar does not say the pane is filtered")
	}
}

// Esc inside the prompt puts back the filter that was there before it opened.
func TestFilterEscRestoresPrevious(t *testing.T) {
	dir := t.TempDir()
	seedFiles(t, dir, "one.go", "two.md")
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeKeys(t, m, "one")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Reopen, type something else, then back out of it.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeKeys(t, m, "two")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	app := m.(Model)
	if app.panes[0].Filter() != "one" {
		t.Fatalf("Filter = %q, want the previous filter back", app.panes[0].Filter())
	}
}

// With nothing modal open, Esc clears the filter.
func TestEscClearsFilterInNormalMode(t *testing.T) {
	dir := t.TempDir()
	seedFiles(t, dir, "a.go", "b.md")
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeKeys(t, m, "a")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	app := m.(Model)
	if app.panes[0].Filter() != "" {
		t.Fatalf("Filter = %q, want empty", app.panes[0].Filter())
	}
	if got := entryNames(app, 0); len(got) != 3 {
		t.Fatalf("entries = %v, want everything back", got)
	}
}

// An operation started from a filtered pane acts on what is visible.
func TestFilterBoundsSelection(t *testing.T) {
	dir := t.TempDir()
	seedFiles(t, dir, "keep.go", "skip.md")
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeKeys(t, m, "*.go")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})

	app := m.(Model)
	names := app.panes[0].SelectedNames()
	if len(names) != 1 || names[0] != "keep.go" {
		t.Fatalf("selected = %v, want [keep.go]", names)
	}
}
