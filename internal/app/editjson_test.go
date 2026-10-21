package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ctrl+t opens the JSON menu; p prettifies the buffer in one undo step, and
// nothing is written until the file is saved.
func TestEditorJSONPrettify(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.json")
	writeBytes(t, p, []byte(`{"a":[1,2],"b":1.50e+10}`+"\n"))
	m := editIn(t, dir, "a.json")

	m = press(t, m, "ctrl+t")
	if app := m.(Model); !strings.Contains(app.View(), "p prettify") {
		t.Fatalf("no JSON menu in the footer:\n%s", app.View())
	}
	m = press(t, m, "p")
	app := m.(Model)
	want := "{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": 1.50e+10\n}\n"
	if got := app.editor.Text(); got != want {
		t.Fatalf("buffer = %q, want %q", got, want)
	}
	if !app.editor.Dirty() || !strings.Contains(app.editStatus, "prettified") {
		t.Errorf("dirty %v, status %q", app.editor.Dirty(), app.editStatus)
	}
	if data, _ := os.ReadFile(p); string(data) != `{"a":[1,2],"b":1.50e+10}`+"\n" {
		t.Error("the file changed before a save")
	}
	m = press(t, m, "ctrl+z")
	if got := m.(Model).editor.Text(); got != `{"a":[1,2],"b":1.50e+10}`+"\n" {
		t.Errorf("one undo left %q", got)
	}
}

// Text that is not JSON is left alone; the footer says where, and the cursor
// goes there.
func TestEditorJSONErrorGoesThere(t *testing.T) {
	dir := t.TempDir()
	text := "{\n  \"a\": 1\n  \"b\": 2\n}"
	writeBytes(t, filepath.Join(dir, "a.json"), []byte(text))
	m := press(t, press(t, editIn(t, dir, "a.json"), "ctrl+t"), "m")
	app := m.(Model)
	if app.editor.Text() != text || app.editor.Dirty() {
		t.Error("invalid JSON was changed")
	}
	if !strings.Contains(app.editStatus, "line 3, col 3") || !strings.Contains(app.editStatus, "comma missing") {
		t.Errorf("status %q", app.editStatus)
	}
	if line, col := app.editor.Cursor(); line != 3 || col != 3 {
		t.Errorf("cursor %d:%d, want 3:3", line, col)
	}
}

// JSONC fails to prettify, and says the way out; c strips the comments.
func TestEditorJSONComments(t *testing.T) {
	dir := t.TempDir()
	writeBytes(t, filepath.Join(dir, "a.jsonc"), []byte("{\n  // note\n  \"a\": [1,],\n}\n"))
	m := press(t, press(t, editIn(t, dir, "a.jsonc"), "ctrl+t"), "p")
	if st := m.(Model).editStatus; !strings.Contains(st, "comments") {
		t.Errorf("status %q, want a hint about the comments", st)
	}
	m = press(t, press(t, m, "ctrl+t"), "c")
	if got := m.(Model).editor.Text(); got != "{\n  \n  \"a\": [1]\n}\n" {
		t.Errorf("buffer = %q", got)
	}
}

// esc closes the menu without doing anything, and leaves the editor open.
func TestEditorJSONMenuEsc(t *testing.T) {
	dir := t.TempDir()
	writeBytes(t, filepath.Join(dir, "a.json"), []byte(`[1]`))
	m := press(t, press(t, editIn(t, dir, "a.json"), "ctrl+t"), "esc")
	app := m.(Model)
	if app.mode != modeEdit || app.editJSON || app.editor.Text() != "[1]" {
		t.Errorf("mode %v, menu %v, text %q", app.mode, app.editJSON, app.editor.Text())
	}
}
