package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ctrl+o writes the buffer to a new file — a bare name lands beside the one
// being edited — and the editor goes on editing the new one.
func TestEditorSaveAs(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "a.txt")
	writeBytes(t, orig, []byte("one\n"))
	m := editIn(t, dir, "a.txt")
	m = press(t, m, "x")

	m = press(t, m, "ctrl+o")
	app := m.(Model)
	if !app.editAs.on || app.editAs.field.Value() != orig {
		t.Fatalf("save-as prompt open %v, prefilled %q", app.editAs.on, app.editAs.field.Value())
	}
	if !strings.Contains(app.View(), "save as") {
		t.Errorf("no save-as prompt in the footer:\n%s", app.View())
	}
	m = press(t, m, "ctrl+u")
	m = press(t, m, "b.json")
	m = press(t, m, "enter")
	app = m.(Model)

	copyPath := filepath.Join(dir, "b.json")
	if data, err := os.ReadFile(copyPath); err != nil || string(data) != "xone\n" {
		t.Fatalf("b.json = %q, %v", data, err)
	}
	if data, _ := os.ReadFile(orig); string(data) != "one\n" {
		t.Errorf("a.txt changed: %q", data)
	}
	if app.editAs.on || app.editor.Dirty() || app.edit.realPath != copyPath || app.edit.title != "b.json" {
		t.Errorf("after save as: prompt %v, dirty %v, editing %q (%q)", app.editAs.on, app.editor.Dirty(), app.edit.realPath, app.edit.title)
	}

	// A plain save now goes to the new file.
	m = press(t, m, "y")
	press(t, m, "ctrl+s")
	if data, _ := os.ReadFile(copyPath); string(data) != "xyone\n" {
		t.Errorf("ctrl+s after save as wrote %q to b.json", data)
	}
	if data, _ := os.ReadFile(orig); string(data) != "one\n" {
		t.Errorf("ctrl+s after save as changed a.txt: %q", data)
	}
}

// Saving over a file that exists takes a second enter; esc leaves everything
// as it was.
func TestEditorSaveAsOverwrite(t *testing.T) {
	dir := t.TempDir()
	writeBytes(t, filepath.Join(dir, "a.txt"), []byte("new\n"))
	other := filepath.Join(dir, "b.txt")
	writeBytes(t, other, []byte("old\n"))
	m := editIn(t, dir, "a.txt")

	m = press(t, press(t, press(t, m, "ctrl+o"), "ctrl+u"), "b.txt")
	m = press(t, m, "enter")
	if app := m.(Model); !app.editAs.on || !strings.Contains(app.editStatus, "exists") {
		t.Fatalf("first enter: prompt %v, status %q", app.editAs.on, app.editStatus)
	}
	if data, _ := os.ReadFile(other); string(data) != "old\n" {
		t.Fatalf("overwritten without asking: %q", data)
	}
	m = press(t, m, "enter")
	if data, _ := os.ReadFile(other); string(data) != "new\n" {
		t.Errorf("second enter did not overwrite: %q", data)
	}

	m = press(t, press(t, press(t, m, "ctrl+o"), "ctrl+u"), "c.txt")
	m = press(t, m, "esc")
	if app := m.(Model); app.editAs.on || app.mode != modeEdit {
		t.Errorf("esc: prompt %v, mode %v", app.editAs.on, app.mode)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.txt")); err == nil {
		t.Error("esc saved anyway")
	}
}

// Esc on the go-to-line prompt closes the prompt, not the editor; the footer
// with all its hints still fits a narrow terminal on one line.
func TestEditorPromptsAndFooter(t *testing.T) {
	dir := t.TempDir()
	writeBytes(t, filepath.Join(dir, "a.go"), []byte("package a\n\nfunc f() {}\n"))
	m := editIn(t, dir, "a.go")
	m = press(t, press(t, m, "ctrl+l"), "esc")
	if app := m.(Model); app.mode != modeEdit || app.editor.Prompting() {
		t.Fatalf("esc on ctrl+l: mode %v, prompt %v", app.mode, app.editor.Prompting())
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = press(t, m, "ctrl+_")
	app := m.(Model)
	if got := app.editor.Text(); got != "// package a\n\nfunc f() {}\n" {
		t.Errorf("ctrl+/ on line 1: %q", got)
	}
	for i, line := range strings.Split(app.View(), "\n") {
		if w := lipgloss.Width(line); w > 50 {
			t.Errorf("row %d is %d wide: %q", i, w, line)
		}
	}
	if n := strings.Count(app.View(), "\n") + 1; n != 12 {
		t.Errorf("view is %d rows, want 12", n)
	}
}
