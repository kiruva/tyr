package app

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
)

// taggedJPEG is a tiny JPEG carrying one comment, which is metadata enough.
func taggedJPEG(t *testing.T, comment string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	clean := buf.Bytes()
	n := len(comment) + 2
	d := []byte{0xFF, 0xD8, 0xFF, 0xFE, byte(n >> 8), byte(n)}
	d = append(d, comment...)
	return append(d, clean[2:]...)
}

func writeBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// v on an image shows what it carries; x swaps to the bytes and back.
func TestViewImageShowsMetadata(t *testing.T) {
	dir := t.TempDir()
	writeBytes(t, filepath.Join(dir, "a.jpg"), taggedJPEG(t, "Made by DALL-E"))
	m := focusEntry(t, newInDir(t, dir), "a.jpg")
	m = press(t, m, "v")

	app := m.(Model)
	if app.mode != modeView || !app.viewer.meta || app.viewer.hex {
		t.Fatalf("mode %v, meta %v, hex %v", app.mode, app.viewer.meta, app.viewer.hex)
	}
	text := strings.Join(app.viewerLines(), "\n")
	for _, want := range []string{"JPEG · 1 item", "Comment", "Made by DALL-E", `mentions "dall-e"`} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}

	m = press(t, m, "x")
	if app = m.(Model); !app.viewer.hex {
		t.Fatal("x did not switch to hex")
	}
	m = press(t, m, "x")
	if app = m.(Model); app.viewer.hex || app.viewer.status != "metadata" {
		t.Errorf("x back: hex %v, status %q", app.viewer.hex, app.viewer.status)
	}
}

// A text file is still a text file.
func TestViewTextIsNotAnImage(t *testing.T) {
	dir := t.TempDir()
	writeBytes(t, filepath.Join(dir, "a.txt"), []byte("hello\n"))
	m := press(t, focusEntry(t, newInDir(t, dir), "a.txt"), "v")
	if app := m.(Model); app.viewer.meta || app.viewer.lines[0] != "hello" {
		t.Errorf("meta %v, lines %q", app.viewer.meta, app.viewer.lines)
	}
}

// An image with nothing in it says so rather than showing an empty report.
func TestViewCleanImage(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(dir, "c.jpg"), buf.Bytes())
	m := press(t, focusEntry(t, newInDir(t, dir), "c.jpg"), "v")
	if lines := m.(Model).viewerLines(); len(lines) != 1 || lines[0] != "JPEG · no metadata" {
		t.Errorf("lines %q", lines)
	}
}

// Images alone open the dialog with only the image rows, and the job written
// from it strips into the other pane, which undo then takes away.
func TestConvertImageRunsAndUndoes(t *testing.T) {
	dir, dest := t.TempDir(), t.TempDir()
	writeBytes(t, filepath.Join(dir, "p.jpg"), taggedJPEG(t, "secret"))
	m := selectForConvert(t, twoPanes(t, dir, dest), "p.jpg")

	app := m.(Model)
	if app.convert.audio || !app.convert.images || app.convert.focus != convertFieldOrient {
		t.Fatalf("audio %v images %v focus %v", app.convert.audio, app.convert.images, app.convert.focus)
	}
	m = press(t, m, "down")
	m = press(t, m, "right") // keep ICC
	m = press(t, m, "enter")
	app = m.(Model)
	if app.mode != modeConfirm || app.convertInPlace {
		t.Fatalf("mode %v, in place %v", app.mode, app.convertInPlace)
	}
	if o := app.pending.Convert; !o.Images || !o.Strip.KeepOrientation || !o.Strip.KeepICC || o.Encoder != "" {
		t.Errorf("opts %+v", o)
	}

	m = press(t, m, "y")
	out := filepath.Join(dest, "p.jpg")
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("secret")) {
		t.Error("the copy kept the comment")
	}
	m = press(t, m, "ctrl+z")
	_ = press(t, m, "y")
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("undo left the copy: %v", err)
	}
}

// Same folder both sides: the confirm prompt says the originals go.
func TestConvertImageInPlaceWarns(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "p.jpg")
	writeBytes(t, in, taggedJPEG(t, "secret"))
	m := selectForConvert(t, twoPanes(t, dir, dir), "p.jpg")
	m = press(t, m, "enter")
	app := m.(Model)
	if !app.convertInPlace || !strings.Contains(m.View(), "stripped in place") {
		t.Fatalf("in place %v; view:\n%s", app.convertInPlace, m.View())
	}
	m = press(t, m, "y")
	if got, _ := os.ReadFile(in); bytes.Contains(got, []byte("secret")) {
		t.Error("the original kept the comment")
	}
	if n := m.(Model).noticeText; !strings.Contains(n, "stripped 1 image in place") {
		t.Errorf("notice %q", n)
	}
	if app := m.(Model); len(app.undoStack) != 0 {
		t.Errorf("an in-place strip went into the undo history: %d entries", len(app.undoStack))
	}
}

// The convert key is C now; a does nothing to a selection.
func TestConvertKeyIsShiftC(t *testing.T) {
	dir := t.TempDir()
	writeBytes(t, filepath.Join(dir, "p.jpg"), taggedJPEG(t, "x"))
	m := press(t, focusEntry(t, twoPanes(t, dir, t.TempDir()), "p.jpg"), "space")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if m.(Model).mode == modeConvert {
		t.Error("a still opens convert")
	}
}

func TestConvertExampleKeepsImageNames(t *testing.T) {
	got := convertExample([]string{"/x/p.jpg"}, fileops.AudioFormatInfo{}, false)
	if got != "p.jpg → p.jpg" {
		t.Errorf("example = %q", got)
	}
}
