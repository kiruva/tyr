package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// runCmd executes a command the model returned and feeds its message back in,
// which is what the Bubble Tea runtime does for real.
func runCmd(t *testing.T, m tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	m, _ = m.Update(msg)
	return m
}

// Find walks the pane's tree and its hits take the pane to the file.
func TestFindJumpsToHit(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "internal", "pkg")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "buried.go"), []byte("package pkg"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedFiles(t, dir, "top.md")

	m := newInDir(t, dir)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	opened := m.(Model)
	if opened.mode != modeFind {
		t.Fatalf("mode = %v, want modeFind", opened.mode)
	}
	if !strings.Contains(opened.View(), "Find") {
		t.Error("the find form is not on screen")
	}

	m = typeKeys(t, m, "*.go")
	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.(Model).find.stage != findStageRunning {
		t.Fatalf("stage = %v, want findStageRunning", m.(Model).find.stage)
	}

	m = runCmd(t, m, cmd)
	app := m.(Model)
	if app.find.stage != findStageResults {
		t.Fatalf("stage = %v, want findStageResults (status %q)", app.find.stage, app.find.status)
	}
	if len(app.find.results) != 1 {
		t.Fatalf("hits = %d, want 1", len(app.find.results))
	}
	if view := app.View(); !strings.Contains(view, "buried.go") {
		t.Errorf("the hit list does not show the file:\n%s", view)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = m.(Model)
	if app.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", app.mode)
	}
	if app.panes[0].Path != deep {
		t.Fatalf("pane went to %q, want %q", app.panes[0].Path, deep)
	}
	if cur, ok := app.panes[0].Current(); !ok || cur.Name != "buried.go" {
		t.Errorf("cursor on %+v, want buried.go", cur)
	}
}

// A search that matches nothing says so and leaves the query on screen.
func TestFindNoMatches(t *testing.T) {
	dir := t.TempDir()
	seedFiles(t, dir, "only.txt")
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = typeKeys(t, m, "*.rs")
	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(t, m, cmd)

	app := m.(Model)
	if app.find.stage != findStageForm {
		t.Fatalf("stage = %v, want the form back", app.find.stage)
	}
	if app.find.status != "nothing matched" {
		t.Errorf("status = %q, want \"nothing matched\"", app.find.status)
	}
	if app.find.name.Value() != "*.rs" {
		t.Errorf("the query was cleared: %q", app.find.name.Value())
	}
}

// An empty query is refused before any walking happens.
func TestFindEmptyQueryRefused(t *testing.T) {
	dir := t.TempDir()
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.find.stage != findStageForm {
		t.Fatalf("stage = %v, want the form", app.find.stage)
	}
	if app.find.status == "" {
		t.Error("an empty query ran without saying anything")
	}
}

// A content search reports the line it matched on.
func TestFindByContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("one\nTODO: later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // to the "contains" field
	m = typeKeys(t, m, "todo")
	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(t, m, cmd)

	app := m.(Model)
	if app.find.stage != findStageResults {
		t.Fatalf("stage = %v, want results (status %q)", app.find.stage, app.find.status)
	}
	hit := app.find.results[0]
	if hit.Line != 2 || hit.Text != "TODO: later" {
		t.Fatalf("hit = line %d %q, want line 2 \"TODO: later\"", hit.Line, hit.Text)
	}
}

// Esc closes the tool without moving anything.
func TestFindEscCloses(t *testing.T) {
	dir := t.TempDir()
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	app := m.(Model)
	if app.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal", app.mode)
	}
	if app.panes[0].Path != dir {
		t.Errorf("pane moved to %q", app.panes[0].Path)
	}
}
