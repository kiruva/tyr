package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// seedTree makes dir/sub with one file of the given size in it.
func seedTree(t *testing.T, dir, sub string, size int) {
	t.Helper()
	full := filepath.Join(dir, sub)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, "payload.bin"), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Space on a directory marks it and measures what is inside it.
func TestSpaceMeasuresDirectory(t *testing.T) {
	dir := t.TempDir()
	seedTree(t, dir, "sub", 3072)
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // off "..", onto sub
	moved := m.(Model)
	if cur, ok := moved.panes[0].Current(); !ok || cur.Name != "sub" {
		t.Fatalf("cursor on %+v, want sub", cur)
	}

	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("Space on a directory measured nothing")
	}
	m = runCmd(t, m, cmd)

	app := m.(Model)
	if !app.panes[0].DirSizeKnown("sub") {
		t.Fatal("the total never arrived")
	}
	if !strings.Contains(app.View(), "3.0KB") {
		t.Error("the measured size is not on screen")
	}
	if names := app.panes[0].SelectedNames(); len(names) != 1 || names[0] != "sub" {
		t.Errorf("selected = %v, want [sub] — Space still marks", names)
	}
}

// "=" measures every directory in the pane at once.
func TestSizeAllDirectories(t *testing.T) {
	dir := t.TempDir()
	seedTree(t, dir, "one", 1024)
	seedTree(t, dir, "two", 2048)
	m := newInDir(t, dir)

	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'='}})
	if cmd == nil {
		t.Fatal("= measured nothing")
	}

	// A batch hands back one message per directory.
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want tea.BatchMsg", msg)
	}
	for _, c := range batch {
		m = runCmd(t, m, c)
	}

	app := m.(Model)
	for _, name := range []string{"one", "two"} {
		if !app.panes[0].DirSizeKnown(name) {
			t.Errorf("%s was not measured", name)
		}
	}
}

// A total that lands after the pane has moved on is dropped.
func TestStaleDirSizeIgnored(t *testing.T) {
	dir := t.TempDir()
	seedTree(t, dir, "sub", 512)
	m := newInDir(t, dir)

	m, _ = m.Update(dirSizeMsg{pane: 0, dir: filepath.Join(dir, "elsewhere"), name: "sub", size: 999})
	app := m.(Model)
	if app.panes[0].DirSizeKnown("sub") {
		t.Error("a total measured in another directory was applied here")
	}
}
