package editor

import (
	"strings"
	"testing"
)

func TestGoToLine(t *testing.T) {
	m := open(strings.Repeat("line\n", 50) + "\tlast")
	keys(m, "ctrl+l", "2", "0", "enter")
	if m.cur != (Pos{19, 0}) || m.Prompting() {
		t.Fatalf("ctrl+l 20: cursor %v, prompt open %v", m.cur, m.Prompting())
	}
	if m.top > 19 || m.top+m.textHeight() <= 19 {
		t.Errorf("line 20 is off screen: top %d", m.top)
	}

	// Line and column; the column is the one the footer shows, so a tab
	// counts as the columns it covers.
	keys(m, "ctrl+l", "51:6", "enter")
	if m.cur != (Pos{50, 2}) {
		t.Errorf("51:6 put the cursor at %v, want {50 2}", m.cur)
	}

	// Past the end is the end; not a number keeps the prompt open and says so.
	keys(m, "ctrl+l", "999", "enter")
	if m.cur.Row != 50 {
		t.Errorf("999 put the cursor on row %d, want the last", m.cur.Row)
	}
	keys(m, "ctrl+l", "x", "enter")
	if !m.Prompting() || m.jump.status == "" {
		t.Errorf("a non-number closed the prompt (open %v, status %q)", m.Prompting(), m.jump.status)
	}
	keys(m, "esc")
	if m.Prompting() || m.cur.Row != 50 {
		t.Errorf("esc: prompt open %v, cursor %v", m.Prompting(), m.cur)
	}
}

func TestReplaceOneAtATime(t *testing.T) {
	m := open("cat Cat dog\ncat")
	keys(m, "ctrl+r", "cat", "tab", "fox")
	if m.selection() != "cat" || m.cur != (Pos{0, 3}) {
		t.Fatalf("typing the query selected %q at %v", m.selection(), m.cur)
	}
	keys(m, "enter") // replaces the first, selects the next
	if got := m.Text(); got != "fox Cat dog\ncat" {
		t.Fatalf("after one replace: %q", got)
	}
	if m.selection() != "Cat" {
		t.Errorf("the next match is not selected: %q", m.selection())
	}
	keys(m, "down", "enter") // skip "Cat", replace the last
	if got := m.Text(); got != "fox Cat dog\nfox" {
		t.Errorf("after skip and replace: %q", got)
	}
	keys(m, "esc", "ctrl+z")
	if got := m.Text(); got != "fox Cat dog\ncat" {
		t.Errorf("undo took back %q, want one replace at a time", got)
	}
}

func TestReplaceAll(t *testing.T) {
	m := open("a.b a.b\nnone\nA.B end")
	keys(m, "ctrl+f", "a.b", "ctrl+r", "x", "ctrl+a")
	if got := m.Text(); got != "x x\nnone\nx end" {
		t.Fatalf("replace all: %q", got)
	}
	if !strings.Contains(m.find.status, "3") {
		t.Errorf("status = %q, want the count", m.find.status)
	}
	keys(m, "esc", "ctrl+z")
	if got := m.Text(); got != "a.b a.b\nnone\nA.B end" {
		t.Errorf("one undo left %q", got)
	}

	// A replacement holding the query is not found again.
	m = open("aa")
	keys(m, "ctrl+r", "a", "tab", "aa", "ctrl+a")
	if got := m.Text(); got != "aaaa" {
		t.Errorf("a → aa: %q", got)
	}
	// Nothing to replace changes nothing.
	m = open("text")
	keys(m, "ctrl+r", "zzz", "tab", "y", "ctrl+a")
	if m.Dirty() || !strings.Contains(m.find.status, "no match") {
		t.Errorf("no match: dirty %v, status %q", m.Dirty(), m.find.status)
	}
}

// The replace bar is drawn on the last row and names both fields.
func TestReplaceBarView(t *testing.T) {
	m := open("x")
	keys(m, "ctrl+r", "q", "tab", "r")
	v := m.View()
	last := v[strings.LastIndexByte(v, '\n')+1:]
	if !strings.Contains(last, "q") || !strings.Contains(last, "replace") {
		t.Errorf("last row = %q", last)
	}
	if n := strings.Count(v, "\n") + 1; n != 10 {
		t.Errorf("view is %d rows, want 10", n)
	}
}
