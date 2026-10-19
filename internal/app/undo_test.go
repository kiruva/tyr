package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	isolateTrash(t)
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

// changeFile gives path new contents and a new modification time, the way an
// editor saving over it would.
func changeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

// A copy that has been worked on since is no longer just a copy: undoing the
// copy leaves it alone, says so, and sends the untouched ones to the trash
// rather than unlinking them.
func TestUndoCopyLeavesEditedCopiesAlone(t *testing.T) {
	isolateTrash(t)
	src, dst := t.TempDir(), t.TempDir()
	seedFiles(t, src, "a.txt", "b.txt")

	m := twoPanes(t, src, dst)
	m = focusEntry(t, m, "a.txt")
	m = press(t, m, "space")
	m = press(t, m, "space")
	m = press(t, m, "c")
	m = press(t, m, "y")

	edited := filepath.Join(dst, "a.txt")
	changeFile(t, edited, "my work")

	m = press(t, m, "ctrl+z")
	if view := m.(Model).View(); !strings.Contains(view, "changed since") {
		t.Errorf("the prompt does not say an edited copy is left alone:\n%s", view)
	}
	m = press(t, m, "y")

	if got, err := os.ReadFile(edited); err != nil || string(got) != "my work" {
		t.Fatalf("the edited copy = %q, %v; want it kept", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "b.txt")); !os.IsNotExist(err) {
		t.Error("the untouched copy is still there")
	}
	trashed, err := trash.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(trashed, "b.txt")); err != nil {
		t.Errorf("the untouched copy did not go to the trash: %v", err)
	}
	if n := len(m.(Model).undoStack); n != 0 {
		t.Errorf("undo stack has %d entries, want the copy spent", n)
	}
}

// When every copy has changed there is nothing to undo, and the entry goes.
func TestUndoCopyRefusedWhenEveryCopyChanged(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	seedFiles(t, src, "a.txt")

	m := focusEntry(t, twoPanes(t, src, dst), "a.txt")
	m = press(t, m, "c")
	m = press(t, m, "y")
	changeFile(t, filepath.Join(dst, "a.txt"), "my work")

	m = press(t, m, "ctrl+z")
	app := m.(Model)
	if app.mode == modeConfirm {
		t.Fatal("asked to remove a copy that has been edited since")
	}
	if !strings.Contains(app.errText, "changed since") {
		t.Errorf("errText = %q, want it to say why", app.errText)
	}
}

// A file put where a moved one landed is somebody else's: undoing the move
// does not carry it off to where the original came from.
func TestUndoMoveLeavesAReplacedFileAlone(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	seedFiles(t, src, "notes.txt")

	m := focusEntry(t, twoPanes(t, src, dst), "notes.txt")
	m = press(t, m, "m")
	m = press(t, m, "y")

	landed := filepath.Join(dst, "notes.txt")
	if err := os.Remove(landed); err != nil {
		t.Fatal(err)
	}
	changeFile(t, landed, "a different file")

	m = press(t, m, "ctrl+z")
	if m.(Model).mode == modeConfirm {
		t.Fatal("offered to move back a file that is not the one moved")
	}
	if _, err := os.Stat(filepath.Join(src, "notes.txt")); !os.IsNotExist(err) {
		t.Error("the replacement was moved to where the original came from")
	}
}

// An undo that stopped part-way — cancelled, or failed on one item — keeps
// what it did not get to, so Ctrl+Z can finish the job.
func TestUnfinishedUndoKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	seedFiles(t, dir, "a.txt", "b.txt")
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")

	m := New()
	m.recordUndo(fileops.Result{Op: fileops.OpCopy, Created: []string{a, b}})
	if err := os.Remove(a); err != nil { // the undo got this far
		t.Fatal(err)
	}
	m.spendUndo(fileops.Result{Op: fileops.OpTrash, Err: fileops.ErrCancelled})
	if len(m.undoStack) != 1 {
		t.Fatal("a cancelled undo dropped the entry")
	}
	if got := m.undoStack[0].created; len(got) != 1 || got[0] != b {
		t.Errorf("left on the history: %v, want only b.txt", got)
	}

	m.recordUndo(fileops.Result{Op: fileops.OpMove, Moved: []fileops.Pair{{Src: "/elsewhere/b.txt", Dst: b}}})
	m.spendUndo(fileops.Result{Op: fileops.OpMovePairs, Err: fileops.ErrCancelled})
	if len(m.undoStack) != 2 {
		t.Fatal("a cancelled move back dropped the entry")
	}

	// A plain failure would fail the same way next time; kept, it would block
	// the history under it.
	m.spendUndo(fileops.Result{Op: fileops.OpMovePairs, Err: os.ErrPermission})
	if len(m.undoStack) != 1 {
		t.Error("a failed move back was kept on top of the history")
	}
}

// The trash cannot take a copy on another volume. The undo says so, and the
// next Ctrl+Z offers to delete the copies for good instead — asking first.
func TestUndoCopyFallsBackFromAnotherVolume(t *testing.T) {
	isolateTrash(t)
	dir := t.TempDir()
	seedFiles(t, dir, "a.txt")
	a := filepath.Join(dir, "a.txt")

	m := New()
	m.recordUndo(fileops.Result{Op: fileops.OpCopy, Created: []string{a}})
	m.openUndo()
	if m.pending.Op != fileops.OpTrash {
		t.Fatalf("op = %v, want the copy undone into the trash", m.pending.Op)
	}

	m.spendUndo(fileops.Result{Op: fileops.OpTrash, Err: fmt.Errorf("a.txt: %w", trash.ErrOtherFilesystem)})
	m.openUndo()
	if m.pending.Op != fileops.OpDelete {
		t.Fatalf("op = %v, want a permanent delete offered", m.pending.Op)
	}
	if view := m.renderConfirm(); !strings.Contains(view, "for good") {
		t.Errorf("the prompt does not say the delete is permanent:\n%s", view)
	}
}
