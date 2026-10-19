package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/trash"
)

// Undoing a move puts the files back where they were.
func TestUndoMove(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "notes.txt"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, twoPanes(t, src, dst), "notes.txt")
	m = press(t, m, "m") // move to the other pane
	m = press(t, m, "y")

	if _, err := os.Stat(filepath.Join(dst, "notes.txt")); err != nil {
		t.Fatalf("the move did not land: %v", err)
	}
	app := m.(Model)
	if len(app.undoStack) != 1 || app.undoStack[0].kind != undoMove {
		t.Fatalf("undo stack = %+v, want one move entry", app.undoStack)
	}

	m = press(t, m, "ctrl+z")
	press(t, m, "y")

	if _, err := os.Stat(filepath.Join(src, "notes.txt")); err != nil {
		t.Fatalf("the file did not come back: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "notes.txt")); !os.IsNotExist(err) {
		t.Error("the moved copy is still at the destination")
	}
}

// Undoing a copy removes what the copy created, and nothing else.
func TestUndoCopyRemovesOnlyTheCopies(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "notes.txt"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dst, "keep.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, twoPanes(t, src, dst), "notes.txt")
	m = press(t, m, "c")
	m = press(t, m, "y")

	copied := filepath.Join(dst, "notes.txt")
	if _, err := os.Stat(copied); err != nil {
		t.Fatalf("the copy did not land: %v", err)
	}

	m = press(t, m, "ctrl+z")
	app := m.(Model)
	if view := app.View(); !strings.Contains(view, "Remove") {
		t.Errorf("the prompt does not say what undoing a copy does:\n%s", view)
	}
	press(t, m, "y")

	if _, err := os.Stat(copied); !os.IsNotExist(err) {
		t.Error("the copy is still there")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("undoing the copy took an unrelated file with it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "notes.txt")); err != nil {
		t.Errorf("undoing the copy removed the original: %v", err)
	}
}

// A copy that overwrote something is not undone by deleting what is there —
// that would take the original with it, so nothing is recorded.
func TestUndoIgnoresOverwritingCopy(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "notes.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "notes.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, twoPanes(t, src, dst), "notes.txt")
	m = press(t, m, "c")
	m = press(t, m, "y")
	m = press(t, m, "o") // overwrite

	if got := len(m.(Model).undoStack); got != 0 {
		t.Fatalf("undo stack has %d entries, want none — an overwrite created nothing", got)
	}
}

// With nothing to undo, Ctrl+Z says so instead of doing something surprising.
func TestUndoWithEmptyHistory(t *testing.T) {
	m := newInDir(t, t.TempDir())
	m = press(t, m, "ctrl+z")

	app := m.(Model)
	if app.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", app.mode)
	}
	if !strings.Contains(app.errText, "nothing to undo") {
		t.Errorf("errText = %q, want it to say there is nothing to undo", app.errText)
	}
}

// A restore that brought back only some of its items keeps the rest on the
// history: they are still in the trash, and Ctrl+Z still gets them out.
func TestPartialRestoreKeepsTheRest(t *testing.T) {
	m := New()
	a := trash.Item{Original: "/w/a.txt", Path: "/t/a.txt"}
	b := trash.Item{Original: "/w/b.txt", Path: "/t/b.txt"}
	m.pushUndo(undoEntry{kind: undoTrash, label: "delete of 2 items", trashed: []trash.Item{a, b}})

	m.spendUndo(fileops.Result{Op: fileops.OpRestore, Trashed: []trash.Item{a}, Err: errors.New("b.txt is no longer in the trash")})
	if len(m.undoStack) != 1 {
		t.Fatal("the entry was dropped with b.txt still in the trash")
	}
	if got := m.undoStack[0].trashed; len(got) != 1 || got[0] != b {
		t.Errorf("left on the history: %v, want only b.txt", got)
	}

	m.spendUndo(fileops.Result{Op: fileops.OpRestore, Trashed: []trash.Item{b}})
	if len(m.undoStack) != 0 {
		t.Error("a restore that brought everything back left the entry behind")
	}
}
