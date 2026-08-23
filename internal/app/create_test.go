package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/remote"
)

// newInDir starts the app with both panes rooted at dir.
func newInDir(t *testing.T, dir string) tea.Model {
	t.Helper()
	t.Chdir(dir)

	var m tea.Model = New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return m
}

// TestNewFileCreatesAndHighlights drives the whole flow: press n, type a name,
// Enter — the file lands in the active pane's directory and the cursor moves to
// it so the next keypress (v, e, F5) acts on what was just made.
func TestNewFileCreatesAndHighlights(t *testing.T) {
	dir := t.TempDir()
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if m.(Model).mode != modeCreate {
		t.Fatalf("mode = %v, want modeCreate", m.(Model).mode)
	}
	m = typeKeys(t, m, "notes.txt")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", app.mode)
	}
	if app.errText != "" {
		t.Errorf("errText = %q, want empty", app.errText)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if cur, ok := app.panes[0].Current(); !ok || cur.Name != "notes.txt" {
		t.Errorf("cursor on %+v, want notes.txt", cur)
	}
}

// A nested name creates the missing directories on the way, and the pane
// highlights the top-level entry — the only part of it the pane can show.
func TestNewFolderNested(t *testing.T) {
	dir := t.TempDir()
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	m = typeKeys(t, m, "src/internal")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	fi, err := os.Stat(filepath.Join(dir, "src", "internal"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !fi.IsDir() {
		t.Error("created a file, want a directory")
	}
	if cur, ok := app.panes[0].Current(); !ok || cur.Name != "src" {
		t.Errorf("cursor on %+v, want src", cur)
	}
}

// A name that is already taken keeps the prompt open with the reason in it, so
// it can be corrected without retyping from scratch.
func TestCreateRejectsExistingName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = typeKeys(t, m, "keep.txt")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.mode != modeCreate {
		t.Fatalf("mode = %v, want the prompt to stay open", app.mode)
	}
	if !strings.Contains(app.create.status, "already exists") {
		t.Errorf("status = %q, want it to mention the clash", app.create.status)
	}
	if !strings.Contains(app.View(), "already exists") {
		t.Error("the render does not show why the name was refused")
	}
	data, err := os.ReadFile(filepath.Join(dir, "keep.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "data" {
		t.Errorf("contents = %q, want the file untouched", data)
	}
}

func TestCreateEscapeCancels(t *testing.T) {
	dir := t.TempDir()
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = typeKeys(t, m, "gone.txt")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.(Model).mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", m.(Model).mode)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.txt")); err == nil {
		t.Error("esc created the file anyway")
	}
}

func TestCleanNewName(t *testing.T) {
	tests := []struct {
		in   string
		want string // "" = must be rejected
	}{
		{"notes.txt", "notes.txt"},
		{"  notes.txt  ", "notes.txt"},
		{"src/main.go", "src/main.go"},
		{"build/", "build"},
		{"", ""},
		{"   ", ""},
		{"/etc/passwd", ""},
		{"~/notes.txt", ""},
		{"..", ""},
		{"../escape", ""},
		{"a/../../escape", ""},
		{".", ""},
	}
	for _, tc := range tests {
		got, err := cleanNewName(tc.in)
		switch {
		case tc.want == "" && err == nil:
			t.Errorf("cleanNewName(%q) = %q, want an error", tc.in, got)
		case tc.want != "" && err != nil:
			t.Errorf("cleanNewName(%q): %v", tc.in, err)
		case tc.want != "" && got != tc.want:
			t.Errorf("cleanNewName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Dotfiles are the one name the pane may legitimately hide, so a create with
// hidden files off must still succeed — it just can't move the cursor.
func TestCreateHiddenFile(t *testing.T) {
	dir := t.TempDir()
	m := newInDir(t, dir)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = typeKeys(t, m, ".env")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if app := m.(Model); app.errText != "" {
		t.Errorf("errText = %q, want empty", app.errText)
	}
}

// On a remote pane the create is a round trip, so it must leave the prompt and
// hand back a command rather than touching the local filesystem.
func TestCreateOnRemotePaneDefersToCommand(t *testing.T) {
	dir := t.TempDir()
	m := newInDir(t, dir)

	app := m.(Model)
	remotePane(t, &app, 0, remote.Host{Name: "box", User: "kim"}, "/srv")
	m = app

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	m = typeKeys(t, m, "logs")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("no command returned for a remote create")
	}
	if got := m.(Model).mode; got != modeNormal {
		t.Errorf("mode = %v, want modeNormal", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs")); err == nil {
		t.Error("a remote create touched the local filesystem")
	}
}

// A failed remote create reports why, and does not leave the pane thinking
// something new is there.
func TestRemoteCreateFailureReported(t *testing.T) {
	m := newInDir(t, t.TempDir())

	app := m.(Model)
	remotePane(t, &app, 0, remote.Host{Name: "box"}, "/srv")

	next, _ := app.Update(createdMsg{pane: 0, name: "logs", dir: true, err: os.ErrPermission})
	if got := next.(Model).errText; !strings.Contains(got, "New folder failed") {
		t.Errorf("errText = %q, want it to name the failure", got)
	}
}
