package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// openEditorOn writes body to a file and opens it in the editor.
func openEditorOn(t *testing.T, name, body string) (tea.Model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m := press(t, focusEntry(t, newInDir(t, dir), name), "e")
	return m, path
}

// A file opened and saved unchanged comes back byte for byte. The old editor
// widget turned tabs into spaces, \r into a line break and dropped other
// control characters; a Makefile saved from it no longer worked.
func TestEditorRoundTripsUnchanged(t *testing.T) {
	bodies := map[string]string{
		"Makefile":   "all:\n\techo hi\n",
		"crlf.txt":   "one\r\ntwo\r\n",
		"ctrl.txt":   "a\x01b\x7fc\x1b[0m\n",
		"no-eol.txt": "last line has no newline",
		"symbol.txt": "a tab symbol ␉ is just a character\n",
		"long.txt":   strings.Repeat("x\n", 20000),
		"plain.go":   "package main\n\nfunc main() {\n\tprintln(\"é\")\n}\n",
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			m, path := openEditorOn(t, name, body)
			app := m.(Model)
			if app.mode != modeEdit {
				t.Fatalf("mode = %v (%q), want the editor", app.mode, app.errText)
			}
			if app.editor.Dirty() {
				t.Error("the buffer counts as changed straight after opening")
			}
			m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
			got, _ := os.ReadFile(path)
			if string(got) != body {
				t.Errorf("saved %q, want %q back", got, body)
			}
		})
	}
}

// Tab types a tab; the cursor starts at the top of the file.
func TestEditorTabKeyTypesATab(t *testing.T) {
	m, path := openEditorOn(t, "Makefile", "all:\n")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if got, _ := os.ReadFile(path); string(got) != "\tall:\n" {
		t.Errorf("saved %q, want a tab at the start", got)
	}
}

// Bytes that are not UTF-8 would not survive the editor, which holds text as
// characters: such a file is refused, untouched.
func TestEditorRefusesNonUTF8(t *testing.T) {
	m, path := openEditorOn(t, "latin1.txt", "caf\xe9\n")
	app := m.(Model)
	if app.mode == modeEdit {
		t.Fatal("opened for editing")
	}
	if !strings.Contains(app.errText, "UTF-8") {
		t.Errorf("errText = %q", app.errText)
	}
	if got, _ := os.ReadFile(path); string(got) != "caf\xe9\n" {
		t.Error("the file changed")
	}
}

// Esc with the find prompt open closes the prompt, not the editor.
func TestEditorEscClosesFindFirst(t *testing.T) {
	m, _ := openEditorOn(t, "a.txt", "hello\n")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.(Model).mode != modeEdit {
		t.Fatal("esc in the find prompt closed the editor")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.(Model).mode != modeNormal {
		t.Error("esc on a clean buffer did not close the editor")
	}
}

// Enter adds a line anywhere in a long file: past 99 lines it did nothing.
func TestEditorEnterPastLine99(t *testing.T) {
	m, _ := openEditorOn(t, "a.txt", strings.Repeat("l\n", 120))
	before := m.(Model).editor.Text()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.(Model).editor.Text() == before {
		t.Error("Enter did nothing")
	}
}

// e in the viewer edits what the viewer shows. Command output has no file
// behind it, and is not handed to the editor as whatever the cursor is on.
func TestViewerEditsWhatItShows(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := press(t, focusEntry(t, newInDir(t, dir), "a.txt"), "v")
	app := m.(Model)
	app.panes[app.active].Focus("b.txt") // the cursor moves on behind the viewer
	m = press(t, app, "e")
	if got := m.(Model).edit.realPath; filepath.Base(got) != "a.txt" {
		t.Errorf("editing %q, want the viewed a.txt", got)
	}

	app = focusEntry(t, newInDir(t, dir), "a.txt").(Model)
	app.showBytes("$ ls", []byte("a.txt\nb.txt\n"), 0)
	m = press(t, app, "e")
	if mode := m.(Model).mode; mode != modeView {
		t.Errorf("mode = %v, want the viewer to stay on the command output", mode)
	}
	if m.(Model).viewer.status == "" {
		t.Error("e on command output said nothing")
	}
}
