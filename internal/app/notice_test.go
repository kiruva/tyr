package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A startup notice occupies the status bar until the user presses something,
// then gets out of the way for good.
func TestNoticeShowsThenClearsOnAnyKey(t *testing.T) {
	var m tea.Model = New().WithNotice("config moved from /old to /new")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	if got := m.View(); !strings.Contains(got, "config moved from /old to /new") {
		t.Fatalf("notice not rendered:\n%s", got)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if app := m.(Model); app.noticeText != "" {
		t.Fatalf("noticeText = %q, want cleared", app.noticeText)
	}
	if got := m.View(); strings.Contains(got, "config moved") {
		t.Fatalf("notice still rendered after a keypress:\n%s", got)
	}
}

// An error outranks a notice: a failure the user needs to see must not be
// hidden behind a housekeeping message.
func TestErrorOutranksNotice(t *testing.T) {
	m := New().WithNotice("config moved from /old to /new")
	m.errText = "copy failed: permission denied"
	m.width, m.height = 100, 24

	got := m.statusBar()
	if !strings.Contains(got, "permission denied") {
		t.Fatalf("error not rendered:\n%s", got)
	}
	if strings.Contains(got, "config moved") {
		t.Fatalf("notice rendered over an error:\n%s", got)
	}
}
