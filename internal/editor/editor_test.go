package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/syntax"
)

// open is an editor on text, sized like a small terminal, with a clipboard of
// its own so tests never touch the desktop's.
func open(text string) *Model {
	m := New()
	m.Clipboard = &MemoryClipboard{}
	m.SetSize(60, 10)
	m.SetText(text, syntax.None)
	return &m
}

// keys sends each key in turn: a name like "ctrl+z", or text to type.
func keys(m *Model, ks ...string) {
	for _, k := range ks {
		m.Update(keyMsg(k))
	}
}

func keyMsg(k string) tea.KeyMsg {
	named := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab,
		"backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
		"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
		"shift+left": tea.KeyShiftLeft, "shift+right": tea.KeyShiftRight,
		"shift+down": tea.KeyShiftDown, "shift+up": tea.KeyShiftUp,
		"home": tea.KeyHome, "end": tea.KeyEnd, "ctrl+home": tea.KeyCtrlHome, "ctrl+end": tea.KeyCtrlEnd,
		"ctrl+z": tea.KeyCtrlZ, "ctrl+y": tea.KeyCtrlY, "ctrl+c": tea.KeyCtrlC, "ctrl+x": tea.KeyCtrlX,
		"ctrl+v": tea.KeyCtrlV, "ctrl+a": tea.KeyCtrlA, "ctrl+f": tea.KeyCtrlF, "ctrl+k": tea.KeyCtrlK,
		"esc": tea.KeyEsc, "ctrl+right": tea.KeyCtrlRight, "ctrl+l": tea.KeyCtrlL,
		"ctrl+r": tea.KeyCtrlR, "ctrl+_": tea.KeyCtrlUnderscore, "ctrl+g": tea.KeyCtrlG,
	}
	// "alt+…" is the key with Alt held, as bubbletea reports it.
	if rest, ok := strings.CutPrefix(k, "alt+"); ok {
		msg := keyMsg(rest)
		msg.Alt = true
		return msg
	}
	if t, ok := named[k]; ok {
		return tea.KeyMsg{Type: t}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// Whatever a file holds comes back unchanged when nothing is edited: tabs,
// CRLF, a lone \r, control characters, no final newline.
func TestRoundTrip(t *testing.T) {
	for _, text := range []string{
		"", "x", "a\n", "all:\n\techo hi\n", "one\r\ntwo\r\n", "one\r\ntwo",
		"mixed\r\nends\n", "lone\rcr\n", "a\x01b\x7fc\x1b[0m\n", "é漢字\n\n\n",
	} {
		m := open(text)
		if got := m.Text(); got != text {
			t.Errorf("%q came back as %q", text, got)
		}
		if m.Dirty() {
			t.Errorf("%q is dirty straight after opening", text)
		}
	}
}

// A line typed into a CRLF file is saved with CRLF like the rest.
func TestCRLFKeptForNewLines(t *testing.T) {
	m := open("one\r\ntwo\r\n")
	keys(m, "end", "enter", "new")
	if got := m.Text(); got != "one\r\nnew\r\ntwo\r\n" {
		t.Errorf("text = %q", got)
	}
}

func TestTypingAndDeleting(t *testing.T) {
	m := open("hello\nworld")
	keys(m, "end", " there", "down", "backspace", "backspace", "home", "delete")
	if got := m.Text(); got != "hello there\nor" {
		t.Errorf("text = %q", got)
	}
	// Backspace at a line's start joins it to the line above.
	keys(m, "home", "home", "backspace")
	if got := m.Text(); got != "hello thereor" {
		t.Errorf("after joining: %q", got)
	}
}

// Enter keeps the indentation of the line it splits; Tab types a tab.
func TestEnterIndentsAndTabIsATab(t *testing.T) {
	m := open("\tif x {")
	keys(m, "end", "enter", "tab", "y")
	if got := m.Text(); got != "\tif x {\n\t\ty" {
		t.Errorf("text = %q", got)
	}
}

// A word typed is one undo step; undo and redo walk back and forth, and the
// buffer is clean again once it is back where it was saved.
func TestUndoRedo(t *testing.T) {
	m := open("a")
	keys(m, "end", "b", "c", "enter", "d")
	if got := m.Text(); got != "abc\nd" {
		t.Fatalf("text = %q", got)
	}
	keys(m, "ctrl+z")
	if got := m.Text(); got != "abc\n" {
		t.Errorf("one undo: %q", got)
	}
	keys(m, "ctrl+z", "ctrl+z")
	if got := m.Text(); got != "a" || m.Dirty() {
		t.Errorf("all undone: %q dirty=%v", got, m.Dirty())
	}
	keys(m, "ctrl+y", "ctrl+y", "ctrl+y")
	if got := m.Text(); got != "abc\nd" || !m.Dirty() {
		t.Errorf("all redone: %q dirty=%v", got, m.Dirty())
	}

	// Saved, then typed on: the new letters are not merged into what was saved.
	m.MarkSaved()
	keys(m, "e", "ctrl+z")
	if got := m.Text(); got != "abc\nd" || m.Dirty() {
		t.Errorf("undo after save: %q dirty=%v", got, m.Dirty())
	}
}

// Backspacing a run of letters is one undo step too.
func TestBackspaceRunIsOneUndo(t *testing.T) {
	m := open("hello")
	keys(m, "end", "backspace", "backspace", "backspace", "ctrl+z")
	if got := m.Text(); got != "hello" {
		t.Errorf("text = %q", got)
	}
}

func TestSelectCopyCutPaste(t *testing.T) {
	m := open("one two\nthree")
	keys(m, "shift+right", "shift+right", "shift+right", "ctrl+c")
	if got := m.Clipboard.(*MemoryClipboard).Text; got != "one" {
		t.Fatalf("copied %q", got)
	}
	keys(m, "ctrl+end", "ctrl+v")
	if got := m.Text(); got != "one two\nthreeone" {
		t.Errorf("after paste: %q", got)
	}
	// Typing over a selection replaces it; cut takes it away.
	keys(m, "ctrl+home", "shift+down", "X")
	if got := m.Text(); got != "Xthreeone" {
		t.Errorf("typed over a selection: %q", got)
	}
	keys(m, "ctrl+a", "ctrl+x")
	if got := m.Text(); got != "" || m.Clipboard.(*MemoryClipboard).Text != "Xthreeone" {
		t.Errorf("cut all: %q, clipboard %q", got, m.Clipboard.(*MemoryClipboard).Text)
	}
	keys(m, "ctrl+z")
	if got := m.Text(); got != "Xthreeone" {
		t.Errorf("undo the cut: %q", got)
	}
}

// A paste from the terminal brings \r line breaks; they become the buffer's.
func TestPasteNormalisesLineBreaks(t *testing.T) {
	m := open("")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\rb\r\nc"), Paste: true})
	if got := m.Text(); got != "a\nb\nc" {
		t.Errorf("text = %q", got)
	}
}

// brokenClipboard is a desktop with no clipboard tool to call.
type brokenClipboard struct{}

func (brokenClipboard) Read() (string, error) { return "", errNoClipboard }
func (brokenClipboard) Write(string) error    { return nil }

// Ctrl+V with no clipboard to read says why once, rather than doing nothing.
func TestPasteWithoutClipboardSaysWhy(t *testing.T) {
	m := open("x")
	m.Clipboard = brokenClipboard{}
	keys(m, "ctrl+v")
	if got := m.TakeNote(); got != errNoClipboard.Error() {
		t.Errorf("note = %q", got)
	}
	if got := m.TakeNote(); got != "" {
		t.Errorf("note shown twice: %q", got)
	}
	if got := m.Text(); got != "x" {
		t.Errorf("text = %q", got)
	}
}

// Tab with several lines selected indents them all; shift+tab takes it back.
func TestIndentSelection(t *testing.T) {
	m := open("a\nb\n    c")
	keys(m, "ctrl+a", "tab")
	if got := m.Text(); got != "\ta\n\tb\n\t    c" {
		t.Errorf("indented: %q", got)
	}
	keys(m, "shift+tab", "shift+tab")
	if got := m.Text(); got != "a\nb\nc" {
		t.Errorf("outdented twice: %q", got)
	}
}

// Ctrl+K cuts the line.
func TestCutLine(t *testing.T) {
	m := open("one\ntwo\nthree")
	keys(m, "down", "ctrl+k")
	if got := m.Text(); got != "one\nthree" {
		t.Errorf("text = %q", got)
	}
	if got := m.Clipboard.(*MemoryClipboard).Text; got != "two\n" {
		t.Errorf("clipboard = %q", got)
	}
}

// Up and down keep aiming for the column they started in, across tabs.
func TestVerticalMovesKeepTheColumn(t *testing.T) {
	m := open("\tabcdef\nx\n\tabcdef")
	keys(m, "end") // column 4+6 = 10
	keys(m, "down", "down")
	if _, col := m.Cursor(); col != 11 {
		t.Errorf("column = %d, want 11", col)
	}
}

func TestFind(t *testing.T) {
	m := open("alpha\nBeta beta\ngamma beta")
	keys(m, "ctrl+f", "b", "e", "t", "a")
	if m.cur != (Pos{1, 4}) || m.selection() != "Beta" {
		t.Errorf("first match at %v selecting %q", m.cur, m.selection())
	}
	keys(m, "enter")
	if m.cur != (Pos{1, 9}) {
		t.Errorf("next match at %v", m.cur)
	}
	keys(m, "enter", "enter")
	if m.cur != (Pos{1, 4}) {
		t.Errorf("after wrapping round: %v", m.cur)
	}
	keys(m, "up")
	if m.cur != (Pos{2, 10}) {
		t.Errorf("previous, wrapping back: %v", m.cur)
	}
	keys(m, "esc")
	if m.Finding() {
		t.Error("esc left the prompt open")
	}
	keys(m, "ctrl+f", "zzz")
	if !strings.Contains(m.find.status, "no match") {
		t.Errorf("status = %q", m.find.status)
	}
}

// A tab is drawn out to the next tab stop, a control character as ^X, and
// every line is exactly as wide as the editor.
func TestViewDrawsTabsAndControls(t *testing.T) {
	m := open("\tx\na\x01b")
	m.color = false
	lines := strings.Split(stripANSI(m.View()), "\n")
	if len(lines) != 10 {
		t.Fatalf("%d lines, want the height of 10", len(lines))
	}
	if !strings.HasPrefix(lines[0], "1 │     x") { // cursor cell + 3 spaces of tab, then x
		t.Errorf("line 1 = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "2 │ a^Ab") {
		t.Errorf("line 2 = %q", lines[1])
	}
}

// A file far bigger than the old 10000-line limit loads and saves whole.
func TestLongFile(t *testing.T) {
	text := strings.Repeat("line\n", 50000)
	m := open(text)
	keys(m, "ctrl+end", "x")
	if got := m.Text(); got != text+"x" {
		t.Errorf("lost lines: %d bytes, want %d", len(got), len(text)+1)
	}
}

// stripANSI removes colour codes, for asserting on what is drawn.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// A whole-text tool works on the selection, or on everything when there is
// none, and its result is one undo step.
func TestReplaceTarget(t *testing.T) {
	m := open("a\nb\nc")
	keys(m, "down")
	if text, whole := m.Target(); text != "a\nb\nc" || !whole {
		t.Fatalf("Target = %q, %v; want the whole text", text, whole)
	}
	m.ReplaceTarget("x\ny\nz\nw")
	if m.Text() != "x\ny\nz\nw" {
		t.Fatalf("Text = %q", m.Text())
	}
	if line, _ := m.Cursor(); line != 2 {
		t.Errorf("cursor on line %d, want it left on line 2", line)
	}
	keys(m, "ctrl+z")
	if m.Text() != "a\nb\nc" {
		t.Errorf("one undo left %q", m.Text())
	}
	keys(m, "ctrl+y")
	if m.Text() != "x\ny\nz\nw" {
		t.Errorf("redo left %q", m.Text())
	}

	// With a selection, only that is the target, and the result stays selected.
	m = open("keep [1,2] keep")
	keys(m, "right", "right", "right", "right", "right",
		"shift+right", "shift+right", "shift+right", "shift+right", "shift+right")
	if text, whole := m.Target(); text != "[1,2]" || whole {
		t.Fatalf("Target = %q, %v", text, whole)
	}
	m.ReplaceTarget("[\n  1,\n  2\n]")
	if m.Text() != "keep [\n  1,\n  2\n] keep" {
		t.Errorf("Text = %q", m.Text())
	}
	if text, _ := m.Target(); text != "[\n  1,\n  2\n]" {
		t.Errorf("the result is not selected: Target = %q", text)
	}
}

// A CRLF file stays CRLF after a whole-text tool rewrote it.
func TestReplaceTargetKeepsCRLF(t *testing.T) {
	m := open("{\"a\":1}\r\n")
	m.ReplaceTarget("{\n  \"a\": 1\n}\n")
	if m.Text() != "{\r\n  \"a\": 1\r\n}\r\n" {
		t.Errorf("Text = %q", m.Text())
	}
}

// GoToTarget puts the cursor at a line and column counted inside the target.
func TestGoToTarget(t *testing.T) {
	m := open("x [1,\n 2 3] y")
	m.GoToTarget(2, 4)
	if line, col := m.Cursor(); line != 2 || col != 4 {
		t.Errorf("cursor %d:%d, want 2:4", line, col)
	}
	keys(m, "ctrl+home", "right", "right", "shift+right", "shift+right", "shift+right", "shift+down", "shift+left")
	if text, _ := m.Target(); text != "[1,\n 2 3" {
		t.Fatalf("Target = %q", text)
	}
	m.GoToTarget(2, 4) // the "3": line 2 of the selection is line 2 of the file
	if line, col := m.Cursor(); line != 2 || col != 4 {
		t.Errorf("cursor %d:%d, want 2:4", line, col)
	}
	keys(m, "ctrl+home", "right", "right", "shift+right", "shift+right", "shift+right", "shift+down", "shift+left")
	m.GoToTarget(1, 2) // the "1": the selection starts at column 3
	if line, col := m.Cursor(); line != 1 || col != 4 {
		t.Errorf("cursor %d:%d, want 1:4", line, col)
	}
}

// Every kind the highlighter can hand back has a style; a kind left out of
// the map is drawn plain without anyone noticing.
func TestEverySyntaxKindIsStyled(t *testing.T) {
	for k := syntax.Keyword; k <= syntax.Removed; k++ {
		if _, ok := syntaxStyle[k]; !ok {
			t.Errorf("syntax kind %d has no style", k)
		}
	}
}

// Alt+Shift+arrow selects a word at a time. bubbletea calls the key
// "alt+shift+left"; the editor listened for "shift+alt+left", so it never
// selected.
func TestAltShiftSelectsWords(t *testing.T) {
	m := open("one two three")
	keys(m, "end", "alt+shift+left")
	if !m.sel || m.selection() != "three" {
		t.Fatalf("alt+shift+left selected %q (sel %v), want three", m.selection(), m.sel)
	}
	keys(m, "home", "alt+shift+right")
	if m.selection() != "one" {
		t.Errorf("alt+shift+right selected %q, want one", m.selection())
	}
}
