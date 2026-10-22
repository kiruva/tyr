package editor

import (
	"strings"
	"testing"

	"github.com/kiruva/tyr/internal/syntax"
)

func TestMoveLines(t *testing.T) {
	m := open("a\nb\nc\nd")
	keys(m, "down", "right", "alt+down")
	if got := m.Text(); got != "a\nc\nb\nd" || m.cur != (Pos{2, 1}) {
		t.Fatalf("alt+down: %q, cursor %v", got, m.cur)
	}
	keys(m, "alt+up", "alt+up")
	if got := m.Text(); got != "b\na\nc\nd" || m.cur != (Pos{0, 1}) {
		t.Fatalf("alt+up twice: %q, cursor %v", got, m.cur)
	}
	keys(m, "alt+up") // already at the top
	if got := m.Text(); got != "b\na\nc\nd" {
		t.Errorf("alt+up on the first line changed it: %q", got)
	}
	keys(m, "ctrl+z", "ctrl+z")
	if got := m.Text(); got != "a\nc\nb\nd" {
		t.Errorf("two undos: %q, want two moves taken back", got)
	}

	// A selection moves every line it touches, and stays selected.
	m = open("1\n2\n3\n4")
	keys(m, "shift+down", "shift+right", "alt+down")
	if got := m.Text(); got != "3\n1\n2\n4" {
		t.Fatalf("moving two lines: %q", got)
	}
	if !m.sel || m.selection() != "1\n2" {
		t.Errorf("selection after the move: %q (sel %v)", m.selection(), m.sel)
	}
	keys(m, "alt+down", "alt+down") // the second one is at the bottom
	if got := m.Text(); got != "3\n4\n1\n2" {
		t.Errorf("moving to the bottom: %q", got)
	}
}

func TestDuplicateLines(t *testing.T) {
	m := open("a\nb")
	keys(m, "right", "alt+shift+down")
	if got := m.Text(); got != "a\na\nb" || m.cur != (Pos{1, 1}) {
		t.Fatalf("alt+shift+down: %q, cursor %v", got, m.cur)
	}
	keys(m, "alt+shift+up")
	if got := m.Text(); got != "a\na\na\nb" || m.cur != (Pos{1, 1}) {
		t.Fatalf("alt+shift+up: %q, cursor %v", got, m.cur)
	}
	keys(m, "ctrl+z", "ctrl+z")
	if got := m.Text(); got != "a\nb" {
		t.Errorf("two undos: %q", got)
	}

	// The last line, with no line break after it, duplicates cleanly.
	m = open("x\nlast")
	keys(m, "down", "alt+shift+down")
	if got := m.Text(); got != "x\nlast\nlast" {
		t.Errorf("duplicating the last line: %q", got)
	}
}

func openLang(text string, lang syntax.Lang) *Model {
	m := open("")
	m.SetText(text, lang)
	return m
}

func TestToggleComment(t *testing.T) {
	m := openLang("func f() {\n\tx := 1\n\n\ty := 2\n}", syntax.Go)
	keys(m, "down", "shift+down", "shift+down", "shift+right")
	if !m.ToggleComment() {
		t.Fatal("Go has no comment marker?")
	}
	want := "func f() {\n\t// x := 1\n\n\t// y := 2\n}"
	if got := m.Text(); got != want {
		t.Fatalf("comment: %q, want %q", got, want)
	}
	if !m.sel {
		t.Error("the lines are no longer selected")
	}
	m.ToggleComment()
	if got := m.Text(); got != "func f() {\n\tx := 1\n\n\ty := 2\n}" {
		t.Errorf("uncomment: %q", got)
	}
	keys(m, "ctrl+z")
	if got := m.Text(); got != want {
		t.Errorf("undo of the uncomment: %q", got)
	}

	// Lines not all commented get commented, at their common indent.
	m = openLang("  # a\n    b", syntax.Python)
	keys(m, "shift+down", "shift+right")
	m.ToggleComment()
	if got := m.Text(); got != "  # # a\n  #   b" {
		t.Errorf("mixed lines: %q", got)
	}

	// The cursor's own line, with no selection; the cursor stays on its text.
	m = openLang("select 1", syntax.SQL)
	keys(m, "end")
	m.ToggleComment()
	if got := m.Text(); got != "-- select 1" || m.cur != (Pos{0, 11}) {
		t.Errorf("SQL line: %q, cursor %v", got, m.cur)
	}
	m.ToggleComment()
	if got := m.Text(); got != "select 1" || m.cur != (Pos{0, 8}) {
		t.Errorf("SQL uncomment: %q, cursor %v", got, m.cur)
	}

	// Markup wraps each line.
	m = openLang("  <a/>", syntax.HTML)
	m.ToggleComment()
	if got := m.Text(); got != "  <!-- <a/> -->" {
		t.Errorf("HTML: %q", got)
	}
	m.ToggleComment()
	if got := m.Text(); got != "  <a/>" {
		t.Errorf("HTML uncomment: %q", got)
	}

	// No way to comment JSON: nothing changes and the caller is told.
	m = openLang(`{"a": 1}`, syntax.JSON)
	if m.ToggleComment() || m.Dirty() {
		t.Error("JSON was commented")
	}
}

func TestBracketPartner(t *testing.T) {
	m := openLang("f(a, \")\", g[1]) {\n\t// }\n\tx\n}", syntax.Go)
	cases := []struct {
		at, want Pos
		found    bool
	}{
		{Pos{0, 1}, Pos{0, 14}, true},  // on (
		{Pos{0, 2}, Pos{0, 14}, true},  // just after (
		{Pos{0, 14}, Pos{0, 1}, true},  // on ), skipping the ) in the string
		{Pos{0, 16}, Pos{3, 0}, true},  // on {, skipping the } in the comment
		{Pos{3, 0}, Pos{0, 16}, true},  // on }
		{Pos{0, 11}, Pos{0, 13}, true}, // on [
		{Pos{2, 1}, Pos{}, false},      // nothing
		{Pos{0, 6}, Pos{}, false},      // the ) inside the string
	}
	for _, c := range cases {
		m.cur, m.sel = c.at, false
		from, to, ok := m.bracketPair()
		if ok != c.found || (ok && to != c.want) {
			t.Errorf("cursor %v: partner %v→%v found %v, want %v found %v", c.at, from, to, ok, c.want, c.found)
		}
	}

	// An unmatched bracket far from its partner gives up rather than scanning
	// the whole file on every keystroke.
	m = openLang("("+strings.Repeat("\nx", 100_000), syntax.Go)
	if _, _, ok := m.bracketPair(); ok {
		t.Error("found a partner for an unmatched (")
	}
}
