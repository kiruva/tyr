package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// editIn opens name in dir in the editor.
func editIn(t *testing.T, dir, name string) tea.Model {
	t.Helper()
	m := focusEntry(t, newInDir(t, dir), name)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if got := m.(Model).mode; got != modeEdit {
		t.Fatalf("mode = %v, want modeEdit", got)
	}
	return m
}

func typeAndSave(m tea.Model, text string) tea.Model {
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	return m
}

// A file changed by something else while it was open is not overwritten by
// the first Ctrl+S: the editor says so, and a second Ctrl+S means it.
func TestEditorSaveRefusesAnOutsideChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	writeAged(t, path, "original\n", time.Hour)
	m := editIn(t, dir, "notes.txt")

	writeAged(t, path, "changed elsewhere\n", 0)
	m = typeAndSave(m, "X")
	if got := readBody(t, path); got != "changed elsewhere\n" {
		t.Fatalf("file = %q, want the outside change kept", got)
	}
	if status := m.(Model).editStatus; !strings.Contains(status, "changed") {
		t.Errorf("status = %q, want it to say the file changed", status)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if got := readBody(t, path); got != "original\nX" {
		t.Errorf("file = %q after the second Ctrl+S, want the buffer", got)
	}
	if status := m.(Model).editStatus; status != "saved" {
		t.Errorf("status = %q, want saved", status)
	}

	// Saved, the file is the editor's again: the next save goes straight through.
	typeAndSave(m, "Y")
	if got := readBody(t, path); got != "original\nXY" {
		t.Errorf("file = %q, want the next save to go through", got)
	}
}

// Saving replaces the file whole, keeps its mode, and through a link writes
// the file the link points at and leaves the link a link.
func TestEditorSaveKeepsModeAndLink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	writeAged(t, real, "body\n", time.Hour)
	if err := os.Chmod(real, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "link.txt")); err != nil {
		t.Skip("no symlinks here:", err)
	}

	m := editIn(t, dir, "link.txt")
	m = typeAndSave(m, "X")
	if status := m.(Model).editStatus; status != "saved" {
		t.Fatalf("status = %q, want saved", status)
	}
	if got := readBody(t, real); got != "body\nX" {
		t.Errorf("target = %q", got)
	}
	if info, _ := os.Lstat(filepath.Join(dir, "link.txt")); info.Mode()&os.ModeSymlink == 0 {
		t.Error("the link was replaced by a file")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(real); info.Mode().Perm() != 0o640 {
			t.Errorf("mode = %v, want 0640 kept", info.Mode().Perm())
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("folder holds %d entries, want no temporary file left", len(entries))
	}
}
