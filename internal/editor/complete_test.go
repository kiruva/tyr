package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/syntax"
)

// typeText types s a key at a time, as a person does.
func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func openAs(text string, lang syntax.Lang) *Model {
	m := open("")
	m.SetText(text, lang)
	return m
}

func labels(m *Model) []string {
	var out []string
	for _, it := range m.comp.items {
		out = append(out, it.label)
	}
	return out
}

// Two characters of a word open the list; one does not.
func TestCompletionOpensAfterTwoChars(t *testing.T) {
	m := openAs("", syntax.Shell)
	typeText(m, "e")
	if m.Completing() {
		t.Fatal("open after one character")
	}
	typeText(m, "c")
	if !m.Completing() || !strings.Contains(strings.Join(labels(m), " "), "echo") {
		t.Fatalf("after ec: open %v, %v", m.Completing(), labels(m))
	}
	keys(m, "tab")
	if got := m.Text(); got != "echo" {
		t.Errorf("tab took it: %q", got)
	}
	if m.Completing() {
		t.Error("still open after taking an entry")
	}
}

// Words already in the file are offered, most used first.
func TestCompletionOffersWordsFromTheFile(t *testing.T) {
	m := openAs("deployTarget=1\ndeployTarget=2\ndeployMode=x\n", syntax.Shell)
	keys(m, "ctrl+end")
	typeText(m, "depl")
	if got := labels(m); len(got) < 2 || got[0] != "deployTarget" || got[1] != "deployMode" {
		t.Fatalf("labels = %v", got)
	}
	keys(m, "down", "tab")
	if !strings.HasSuffix(m.Text(), "deployMode") {
		t.Errorf("text = %q", m.Text())
	}
}

// A snippet lands indented under its line, with the cursor where it says.
func TestCompletionSnippet(t *testing.T) {
	m := openAs("\t", syntax.Shell)
	keys(m, "end")
	typeText(m, "for")
	if got := labels(m); len(got) == 0 || got[0] != "for" || m.comp.items[0].kind != itemSnippet {
		t.Fatalf("labels = %v", got)
	}
	keys(m, "tab")
	if got := m.Text(); got != "\tfor  in ; do\n\t\t\n\tdone" {
		t.Errorf("snippet = %q", got)
	}
	typeText(m, "f")
	if got := m.Text(); !strings.HasPrefix(got, "\tfor f in") {
		t.Errorf("cursor not at the mark: %q", got)
	}
}

// Esc and Enter put the list away; Enter is still a line break.
func TestCompletionStaysOutOfTheWay(t *testing.T) {
	m := openAs("", syntax.Shell)
	typeText(m, "ec")
	keys(m, "esc")
	if m.Completing() || m.Text() != "ec" {
		t.Fatalf("esc: open %v, %q", m.Completing(), m.Text())
	}
	typeText(m, "h")
	keys(m, "enter")
	if m.Completing() || m.Text() != "ech\n" {
		t.Fatalf("enter: open %v, %q", m.Completing(), m.Text())
	}
	// Nothing in a comment.
	typeText(m, "# ec")
	if m.Completing() {
		t.Error("opened in a comment")
	}
}

// Ctrl+Space asks without a prefix, and takes a single match outright.
func TestCompletionOnRequest(t *testing.T) {
	m := openAs("uniqueName\n", syntax.Go)
	keys(m, "ctrl+end")
	typeText(m, "uniq")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
	if got := m.Text(); got != "uniqueName\nuniqueName" {
		t.Errorf("text = %q", got)
	}
}

// YAML gets spaces, whatever the snippet says.
func TestCompletionSnippetYAMLSpaces(t *testing.T) {
	m := openAs("", syntax.YAML)
	typeText(m, "servi")
	keys(m, "tab")
	if got := m.Text(); got != ":\n  image: \n  restart: unless-stopped" {
		t.Errorf("snippet = %q", got)
	}
}

// The list is drawn over the lines, the chosen entry marked.
func TestCompletionIsDrawn(t *testing.T) {
	m := openAs("", syntax.Dockerfile)
	typeText(m, "WORK")
	if view := m.View(); !strings.Contains(view, "WORKDIR") {
		t.Errorf("no list:\n%s", view)
	}
}
