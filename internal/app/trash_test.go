package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/config"
)

// isolateTrash points the trash at a temp directory, on either platform, so a
// test deletes into somewhere it owns.
func isolateTrash(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("HOME", dir)
}

// F8 moves the entry to the trash rather than unlinking it, and Ctrl+Z brings
// it back.
func TestDeleteToTrashAndUndo(t *testing.T) {
	isolateTrash(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, newInDir(t, dir), "notes.txt")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})

	app := m.(Model)
	if app.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", app.mode)
	}
	if view := app.View(); !strings.Contains(view, "trash") {
		t.Errorf("the prompt does not mention the trash:\n%s", view)
	}

	m = press(t, m, "y")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("the file is still where it was")
	}
	app = m.(Model)
	if len(app.undoStack) != 1 || app.undoStack[0].kind != undoTrash {
		t.Fatalf("undo stack = %+v, want one trash entry", app.undoStack)
	}
	if !strings.Contains(app.noticeText, "ctrl+z") {
		t.Errorf("noticeText = %q, want it to offer the undo", app.noticeText)
	}

	m = press(t, m, "ctrl+z")
	m = press(t, m, "y")

	if body, err := os.ReadFile(file); err != nil {
		t.Fatalf("the file did not come back: %v", err)
	} else if string(body) != "keep me" {
		t.Errorf("restored contents = %q", body)
	}
	if got := len(m.(Model).undoStack); got != 0 {
		t.Errorf("undo stack has %d entries left, want it spent", got)
	}
}

// D deletes for good: no trash, no undo entry, and the prompt says so.
func TestDeletePermanently(t *testing.T) {
	isolateTrash(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, newInDir(t, dir), "gone.txt")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})

	app := m.(Model)
	if view := app.View(); !strings.Contains(view, "cannot be undone") {
		t.Errorf("the prompt does not warn that it is permanent:\n%s", view)
	}

	m = press(t, m, "y")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("the file survived a permanent delete")
	}
	if got := len(m.(Model).undoStack); got != 0 {
		t.Errorf("undo stack has %d entries, want none — this cannot be undone", got)
	}
}

// The config can turn the trash off, and then F8 is the permanent delete.
func TestConfigTurnsTrashOff(t *testing.T) {
	isolateTrash(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	var m tea.Model = New().WithSession(config.Config{Delete: "remove"})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = focusEntry(t, m, "gone.txt")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if got := m.(Model).pending.Op.String(); got != "Delete" {
		t.Fatalf("pending op = %q, want Delete", got)
	}

	m = press(t, m, "y")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("the file survived")
	}
	if got := len(m.(Model).undoStack); got != 0 {
		t.Errorf("undo stack = %d entries, want none", got)
	}
}
