package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kiruva/tyr/internal/metastrip"
)

func orientationIn(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := metastrip.Strip(data, metastrip.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r.Orientation
}

// o opens the dialog, r turns the image at once, and ctrl+z turns it back.
func TestRotateAndUndo(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jpg")
	writeBytes(t, p, taggedJPEG(t, "kept"))

	m := press(t, focusEntry(t, newInDir(t, dir), "a.jpg"), "o")
	app := m.(Model)
	if app.mode != modeRotate {
		t.Fatalf("mode %v, want the rotate dialog", app.mode)
	}
	if !strings.Contains(app.View(), "shown now: normal") {
		t.Errorf("the dialog does not say how the image is shown:\n%s", app.View())
	}

	m = press(t, m, "r")
	app = m.(Model)
	if app.mode != modeNormal {
		t.Fatalf("mode %v after the turn", app.mode)
	}
	if got := orientationIn(t, p); got != 6 {
		t.Fatalf("orientation %d, want 6", got)
	}
	if !strings.Contains(app.noticeText, "rotate right: 1 image") || !strings.Contains(app.noticeText, "ctrl+z") {
		t.Errorf("notice %q", app.noticeText)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), "kept") {
		t.Error("the comment is gone: rotating stripped the image")
	}

	m = press(t, m, "ctrl+z")
	if app = m.(Model); !strings.Contains(app.View(), "Turn 1 image back") {
		t.Errorf("undo prompt:\n%s", app.View())
	}
	press(t, m, "y")
	if got := orientationIn(t, p); got != 1 {
		t.Errorf("orientation %d after the undo, want 1 (normal)", got)
	}
}

func TestRotateRefusesANonImage(t *testing.T) {
	dir := t.TempDir()
	writeBytes(t, filepath.Join(dir, "a.txt"), []byte("hello"))
	m := press(t, focusEntry(t, newInDir(t, dir), "a.txt"), "o")
	if app := m.(Model); app.mode != modeNormal || !strings.Contains(app.errText, "not an image") {
		t.Errorf("mode %v, error %q", app.mode, app.errText)
	}
}

func TestRotateEscCancels(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jpg")
	writeBytes(t, p, taggedJPEG(t, "x"))
	m := press(t, press(t, focusEntry(t, newInDir(t, dir), "a.jpg"), "o"), "esc")
	if app := m.(Model); app.mode != modeNormal {
		t.Errorf("mode %v", app.mode)
	}
	if got := orientationIn(t, p); got != 0 {
		t.Errorf("orientation %d after esc", got)
	}
}
