package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/config"
)

// B saves the directory the pane is in; b lists what is saved and Enter goes
// there.
func TestBookmarkAddAndJump(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	home := t.TempDir()
	target := filepath.Join(home, "project")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	m := newInDir(t, target)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})

	app := m.(Model)
	if app.mode != modeBookmarkAdd {
		t.Fatalf("mode = %v, want modeBookmarkAdd", app.mode)
	}
	if got := app.bookmarkAdd.input.Value(); got != "project" {
		t.Errorf("suggested name = %q, want the directory's own name", got)
	}
	m = press(t, m, "enter")

	marks, err := config.Bookmarks()
	if err != nil {
		t.Fatalf("read bookmarks: %v", err)
	}
	if len(marks) != 1 || marks[0].Name != "project" || marks[0].Path != target {
		t.Fatalf("saved %+v, want project → %s", marks, target)
	}

	// Walk away, then come back through the list.
	app = m.(Model)
	if err := app.panes[0].GoTo(home); err != nil {
		t.Fatal(err)
	}
	var back tea.Model = app
	back, _ = back.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})

	listed := back.(Model)
	if listed.mode != modeBookmarks {
		t.Fatalf("mode = %v, want modeBookmarks", listed.mode)
	}
	if view := listed.View(); !strings.Contains(view, "project") {
		t.Errorf("the list does not show the bookmark:\n%s", view)
	}

	back = press(t, back, "enter")
	if got := back.(Model).panes[0].Path; got != target {
		t.Fatalf("pane at %q, want %q", got, target)
	}
}

// d asks before removing one, and y removes it.
func TestBookmarkDelete(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	if err := config.SaveBookmark(config.Bookmark{Name: "docs", Path: dir}); err != nil {
		t.Fatal(err)
	}

	m := newInDir(t, dir)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})

	if !m.(Model).bookmarks.deleting {
		t.Fatal("d did not ask first")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	marks, _ := config.Bookmarks()
	if len(marks) != 0 {
		t.Fatalf("bookmarks = %+v, want none left", marks)
	}
}

// A bookmark pointing somewhere that has gone says so rather than moving the
// pane nowhere.
func TestBookmarkToMissingDirectory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	if err := config.SaveBookmark(config.Bookmark{Name: "gone", Path: filepath.Join(dir, "nope")}); err != nil {
		t.Fatal(err)
	}

	m := newInDir(t, dir)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.mode != modeBookmarks {
		t.Fatalf("mode = %v, want the list to stay open", app.mode)
	}
	if app.bookmarks.status == "" {
		t.Error("nothing explained why the jump did not happen")
	}
	if app.panes[0].Path != dir {
		t.Errorf("the pane moved to %q anyway", app.panes[0].Path)
	}
}

// An empty list says how to fill it.
func TestBookmarksEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newInDir(t, t.TempDir())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})

	if status := m.(Model).bookmarks.status; !strings.Contains(status, "ctrl+d") && !strings.Contains(status, "no bookmarks") {
		t.Errorf("status = %q, want it to say the list is empty", status)
	}
}
