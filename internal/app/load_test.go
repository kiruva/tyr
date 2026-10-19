package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file bigger than the viewer holds opens at its start, saying so; the
// editor refuses it, since saving what it held would cut the file short.
func TestOpenLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxViewBytes + 1); err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteAt([]byte("first line\n"), 0)
	f.Close()

	m := press(t, focusEntry(t, newInDir(t, dir), "big.log"), "v")
	app := m.(Model)
	if app.mode != modeView {
		t.Fatalf("mode = %v, want the viewer", app.mode)
	}
	if len(app.viewer.raw) != maxViewBytes || !strings.Contains(app.viewer.status, "first") {
		t.Errorf("held %d bytes, status %q; want the first %d and a note", len(app.viewer.raw), app.viewer.status, maxViewBytes)
	}

	m = press(t, focusEntry(t, newInDir(t, dir), "big.log"), "e")
	app = m.(Model)
	if app.mode == modeEdit || !strings.Contains(app.errText, "too big") {
		t.Errorf("mode = %v errText = %q, want the editor to refuse", app.mode, app.errText)
	}
}

// A reply for an open that has been superseded is dropped.
func TestStaleLoadIsDropped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := focusEntry(t, newInDir(t, dir), "a.txt")
	m, first := keyPress(m, "v")
	m, second := keyPress(m, "v")
	m, _ = m.Update(first())
	if m.(Model).mode == modeView {
		t.Error("the first, superseded, reply opened the viewer")
	}
	m, _ = m.Update(second())
	if m.(Model).mode != modeView {
		t.Error("the current reply did not open the viewer")
	}
}

// The hex dump and the wrapped lines are built once per layout, not once per
// call: a search calls for them twice a keystroke.
func TestViewerLinesAreCached(t *testing.T) {
	var m Model
	m.width, m.height = 80, 24
	m.viewer = newViewer("bin", []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, 0)
	a, b := m.viewerLines(), m.viewerLines()
	if &a[0] != &b[0] {
		t.Error("the hex dump was rebuilt for the same layout")
	}
	m.viewer.hex, m.viewer.wrap = false, true
	if c := m.viewerLines(); len(c) > 0 && &c[0] == &a[0] {
		t.Error("a different layout got the old lines")
	}
}
