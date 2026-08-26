package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// openIn writes a file, opens it in the viewer, and hands back the model.
func openIn(t *testing.T, name, body string) tea.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, newInDir(t, dir), name)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if got := m.(Model).mode; got != modeView {
		t.Fatalf("mode = %v, want modeView", got)
	}
	return m
}

// The viewer opens with numbered lines and the file's contents in it.
func TestViewerShowsNumberedLines(t *testing.T) {
	m := openIn(t, "notes.txt", "first line\nsecond line\nthird line\n")

	view := m.(Model).View()
	for _, want := range []string{"notes.txt", "first line", "third line", "1 │", "3 │"} {
		if !strings.Contains(view, want) {
			t.Errorf("the viewer does not show %q:\n%s", want, view)
		}
	}

	// # turns the numbers off, and says so.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'#'}})
	app := m.(Model)
	if app.viewer.numbers {
		t.Error("# did not turn the numbers off")
	}
	if strings.Contains(app.View(), "1 │") {
		t.Error("the numbers are still drawn")
	}
}

// x switches to the hex dump, which shows the bytes and what they stand for.
func TestViewerHexMode(t *testing.T) {
	m := openIn(t, "notes.txt", "hello world\n")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	app := m.(Model)
	if !app.viewer.hex {
		t.Fatal("x did not switch to hex")
	}
	view := app.View()
	for _, want := range []string{"00000000", "68 65 6c 6c 6f", "|hello world.|", "hex"} {
		if !strings.Contains(view, want) {
			t.Errorf("the dump does not show %q:\n%s", want, view)
		}
	}
}

// A file that is not text opens as hex rather than being refused.
func TestViewerOpensBinaryAsHex(t *testing.T) {
	dir := t.TempDir()
	data := []byte{0x7f, 'E', 'L', 'F', 0x00, 0x01, 0x02, 0x03}
	if err := os.WriteFile(filepath.Join(dir, "a.out"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, newInDir(t, dir), "a.out")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})

	app := m.(Model)
	if app.mode != modeView {
		t.Fatalf("mode = %v — a binary file should still open", app.mode)
	}
	if !app.viewer.hex {
		t.Error("a binary file did not open in hex")
	}
	if app.errText != "" {
		t.Errorf("errText = %q, want none", app.errText)
	}
	if !strings.Contains(app.View(), "7f 45 4c 46") {
		t.Errorf("the dump does not show the file's bytes:\n%s", app.View())
	}
}

// w wraps long lines instead of cutting them off at the edge.
func TestViewerWrap(t *testing.T) {
	long := strings.Repeat("abcdefghij", 30) // 300 characters
	m := openIn(t, "long.txt", long+"\n")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	app := m.(Model)
	if !app.viewer.wrap {
		t.Fatal("w did not turn wrapping on")
	}
	if got := len(app.viewerLines()); got < 3 {
		t.Fatalf("%d display lines, want the long line broken up", got)
	}
	for _, line := range app.viewerLines() {
		if len([]rune(line)) > app.textWidth() {
			t.Fatalf("a wrapped line is %d wide, over the %d available", len([]rune(line)), app.textWidth())
		}
	}
}

// / finds text, n walks the hits, and the footer counts them.
func TestViewerSearch(t *testing.T) {
	m := openIn(t, "notes.txt", "alpha\nbeta\nneedle one\ngamma\nneedle two\n")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !m.(Model).viewer.prompting {
		t.Fatal("/ did not open the search field")
	}
	m = typeKeys(t, m, "needle")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if len(app.viewer.matches) != 2 {
		t.Fatalf("matches = %v, want the two lines with the word on", app.viewer.matches)
	}
	if !strings.Contains(app.View(), "match 1 of 2") {
		t.Errorf("the footer does not count the matches:\n%s", app.View())
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if got := m.(Model).viewer.current; got != 1 {
		t.Errorf("n moved to match %d, want the second", got)
	}
	// n past the end comes back to the first.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if got := m.(Model).viewer.current; got != 0 {
		t.Errorf("n at the last match went to %d, want it to wrap to 0", got)
	}

	// Esc drops the search but stays in the viewer.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = m.(Model)
	if app.mode != modeView {
		t.Fatalf("mode = %v, want the viewer still open", app.mode)
	}
	if app.viewer.query != "" {
		t.Errorf("query = %q, want it cleared", app.viewer.query)
	}
}

func TestViewerSearchWithNoMatch(t *testing.T) {
	m := openIn(t, "notes.txt", "alpha\nbeta\n")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeKeys(t, m, "zzz")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if len(app.viewer.matches) != 0 {
		t.Fatalf("matches = %v, want none", app.viewer.matches)
	}
	if !strings.Contains(app.viewer.status, "no match") {
		t.Errorf("status = %q, want it to say so", app.viewer.status)
	}
}

// A Go file is highlighted; s turns that off.
func TestViewerSyntaxToggle(t *testing.T) {
	m := openIn(t, "main.go", "package main\n\nfunc main() {} // hi\n")

	app := m.(Model)
	if app.viewer.lang.String() != "code" {
		t.Fatalf("language = %q, want code", app.viewer.lang.String())
	}
	if !app.viewer.colour {
		t.Error("a known language opened with highlighting off")
	}
	// What the spans are is the highlighter's business (and its tests); what
	// matters here is that the text survives being coloured.
	if !strings.Contains(app.View(), "package main") {
		t.Errorf("the highlighted view lost the text:\n%s", app.View())
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	app = m.(Model)
	if app.viewer.colour {
		t.Fatal("s did not turn highlighting off")
	}
	if !strings.Contains(app.View(), "func main() {} // hi") {
		t.Error("the plain text is not shown once highlighting is off")
	}
}

// q closes the viewer, and e hands the same file to the editor.
func TestViewerCloseAndEdit(t *testing.T) {
	m := openIn(t, "notes.txt", "text\n")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if got := m.(Model).mode; got != modeEdit {
		t.Fatalf("mode = %v, want modeEdit", got)
	}

	m = openIn(t, "notes.txt", "text\n")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if got := m.(Model).mode; got != modeNormal {
		t.Fatalf("mode = %v, want modeNormal", got)
	}
}

func TestHexDump(t *testing.T) {
	lines := hexDump([]byte("hi"))
	if len(lines) != 1 {
		t.Fatalf("%d lines for two bytes", len(lines))
	}
	if !strings.HasPrefix(lines[0], "00000000  68 69 ") {
		t.Errorf("line = %q", lines[0])
	}
	if !strings.HasSuffix(lines[0], "|hi|") {
		t.Errorf("line = %q, want the printable column at the end", lines[0])
	}
	if got := hexDump(nil); len(got) != 1 || got[0] != "(empty)" {
		t.Errorf("an empty file dumps as %v", got)
	}
}

func TestSplitLines(t *testing.T) {
	if got := splitLines([]byte("a\nb\n")); len(got) != 2 {
		t.Errorf("splitLines = %v, want two lines and no trailing blank", got)
	}
	if got := splitLines([]byte("a\r\nb")); len(got) != 2 || got[0] != "a" {
		t.Errorf("splitLines with CRLF = %q", got)
	}
	if got := splitLines(nil); len(got) != 1 || got[0] != "" {
		t.Errorf("an empty file split into %q", got)
	}
}
