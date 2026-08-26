package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// writeAged writes a file with a known age, so "newer" means something.
func writeAged(t *testing.T, path, body string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

// copyOnto sets up a collision and runs the copy up to the question.
func copyOnto(t *testing.T) (tea.Model, string, string) {
	t.Helper()
	src, dst := t.TempDir(), t.TempDir()
	writeAged(t, filepath.Join(src, "notes.txt"), "new", 0)
	writeAged(t, filepath.Join(dst, "notes.txt"), "old", time.Hour)

	m := focusEntry(t, twoPanes(t, src, dst), "notes.txt")
	m = press(t, m, "c") // copy what the cursor is on to the other pane
	m = press(t, m, "y") // confirm; the job runs and hits the collision
	return m, src, dst
}

func TestCopyCollisionAsks(t *testing.T) {
	m, _, dst := copyOnto(t)

	app := m.(Model)
	if app.mode != modeConflict {
		t.Fatalf("mode = %v, want modeConflict", app.mode)
	}
	view := app.View()
	if !strings.Contains(view, "Already there") || !strings.Contains(view, "notes.txt") {
		t.Errorf("the dialog does not name the collision:\n%s", view)
	}
	if !strings.Contains(view, "newer") {
		t.Error("the dialog does not say which side is newer")
	}

	m = press(t, m, "o") // overwrite
	if got := readBody(t, filepath.Join(dst, "notes.txt")); got != "new" {
		t.Fatalf("destination holds %q, want the copied contents", got)
	}
	if mode := m.(Model).mode; mode != modeNormal {
		t.Errorf("mode = %v after answering, want modeNormal", mode)
	}
}

func TestCopyCollisionSkip(t *testing.T) {
	m, _, dst := copyOnto(t)
	m = press(t, m, "s")

	if got := readBody(t, filepath.Join(dst, "notes.txt")); got != "old" {
		t.Fatalf("destination holds %q, want it untouched", got)
	}
	if err := m.(Model).errText; err != "" {
		t.Errorf("errText = %q, want none — skipping is not a failure", err)
	}
}

func TestCopyCollisionKeepBoth(t *testing.T) {
	m, _, dst := copyOnto(t)
	press(t, m, "b")

	if got := readBody(t, filepath.Join(dst, "notes.txt")); got != "old" {
		t.Errorf("the original holds %q, want it untouched", got)
	}
	if got := readBody(t, filepath.Join(dst, "notes (2).txt")); got != "new" {
		t.Errorf("the kept copy holds %q, want the new contents", got)
	}
}

// Cancelling says so in the status bar rather than reporting a failure.
func TestCopyCollisionCancel(t *testing.T) {
	m, _, dst := copyOnto(t)
	m = press(t, m, "esc")

	app := m.(Model)
	if got := readBody(t, filepath.Join(dst, "notes.txt")); got != "old" {
		t.Errorf("destination holds %q, want it untouched", got)
	}
	if app.errText != "" {
		t.Errorf("errText = %q, want the cancel reported as a notice", app.errText)
	}
	if !strings.Contains(app.noticeText, "cancelled") {
		t.Errorf("noticeText = %q, want it to say cancelled", app.noticeText)
	}
}

// readBody is the contents of a file the test just acted on.
func readBody(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
