package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// hostileName is a file name that, drawn raw, writes "hello" to the clipboard
// (OSC 52), retitles the window (OSC 0) and clears the screen (CSI 2J).
const hostileName = "a\x1b]52;c;aGVsbG8=\x07b\x1b]0;PWNED\x07c\x1b[2Jd"

// assertInert fails if a frame still holds anything that would make the
// terminal act rather than draw: an escape other than SGR, or a C0 control
// other than newline and tab.
func assertInert(t *testing.T, where, frame string) {
	t.Helper()
	for _, bad := range []string{"\x1b]", "\x1b[2J", "\x07", "\x1bP"} {
		if strings.Contains(frame, bad) {
			t.Errorf("%s: frame still contains %q", where, bad)
		}
	}
}

func hostileDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows file names cannot hold control characters")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, hostileName), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A name with escape sequences in it is drawn as a name: the panes show it
// escaped, and nothing in it reaches the terminal as a command.
func TestHostileNameIsShownEscaped(t *testing.T) {
	m := focusEntry(t, newInDir(t, hostileDir(t)), hostileName)

	frame := m.(Model).View()
	assertInert(t, "pane", frame)
	if !strings.Contains(frame, `\u001b]52;c;aGVsbG8=\u0007b`) {
		t.Errorf("the name is not shown escaped:\n%s", frame)
	}
}

// The dialogs that name the file in hand get the same treatment.
func TestHostileNameInDialogs(t *testing.T) {
	for _, key := range []string{"i", "r"} { // properties, rename
		m := focusEntry(t, newInDir(t, hostileDir(t)), hostileName)
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})

		frame := m.(Model).View()
		assertInert(t, key, frame)
		if !strings.Contains(frame, `\u001b]0;PWNED\u0007`) {
			t.Errorf("%s: the dialog does not show the name escaped:\n%s", key, frame)
		}
	}
}

// An error that carries outside text — a remote's stderr, a tool's complaint —
// is escaped, kept to one line, and cut to the width of the bar.
func TestErrorBarIsOneSafeLine(t *testing.T) {
	m := New()
	m.width, m.height = 40, 10
	m.errText = "remote: " + hostileName + "\nsecond line\r\nthird line that goes on and on and on"

	bar := m.statusBar()
	assertInert(t, "error bar", m.View())
	if strings.Contains(bar, "\n") {
		t.Errorf("error bar spans more than one line:\n%s", bar)
	}
	if !strings.Contains(bar, `\u001b`) {
		t.Errorf("error bar does not show the escape:\n%s", bar)
	}
}

// File contents are somebody else's too: the viewer shows an ESC in a text file
// as an escape, and keeps the tabs.
func TestViewerEscapesControlCharacters(t *testing.T) {
	m := openIn(t, "log.txt", "plain\tline\nred \x1b[31mtext\x1b]0;PWNED\x07\n")

	frame := m.(Model).View()
	assertInert(t, "viewer", frame)
	if !strings.Contains(frame, `\u001b[31mtext`) {
		t.Errorf("the escape in the file is not shown:\n%s", frame)
	}
	if got := m.(Model).viewer.lines[0]; got != "plain\tline" {
		t.Errorf("first line = %q, want the tab kept", got)
	}
}

// tyr's own frames are styling and text only, so the filter has nothing to
// take out of them: if it ever does, it is eating a sequence tyr meant to send.
// One frame from each kind of screen: panes, help, properties, viewer, editor,
// theme picker, key editor, connect, find, command, batch rename, new file.
func TestFrameLeavesOwnRenderingAlone(t *testing.T) {
	withColour(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "?", "i", "v", "e", "t", "K", "S", "F", "x", "M", "n"} {
		m := focusEntry(t, newInDir(t, dir), "notes.txt")
		if key != "" {
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		}
		app := m.(Model)
		frame := app.render()
		if !strings.Contains(frame, "\x1b[") {
			t.Fatalf("after %q: no styling rendered, nothing to check", key)
		}
		if app.View() != frame {
			t.Errorf("after %q: Frame changed tyr's own frame", key)
		}
	}
}

// withColour renders in true colour for the rest of the test. Tests run without
// a terminal, where lipgloss draws no colour at all — and a check that styling
// survives Frame would then pass without a single SGR sequence in the frame.
func withColour(t *testing.T) {
	t.Helper()
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(before) })
}
