package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// x runs a command in the pane's directory and shows what it printed.
func TestRunCommandShowsOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newInDir(t, dir)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.(Model).mode != modeCommand {
		t.Fatalf("mode = %v, want modeCommand", m.(Model).mode)
	}

	m = typeKeys(t, m, "ls")
	m = press(t, m, "enter")

	app := m.(Model)
	if app.mode != modeView {
		t.Fatalf("mode = %v, want the output in the pager", app.mode)
	}
	if !strings.Contains(app.viewTitle, "ls") {
		t.Errorf("title = %q, want the command in it", app.viewTitle)
	}
	if view := app.View(); !strings.Contains(view, "one.txt") {
		t.Errorf("the output does not show what ls printed:\n%s", view)
	}
}

// A command that fails shows its error alongside whatever it managed to print.
func TestRunCommandReportsFailure(t *testing.T) {
	m := newInDir(t, t.TempDir())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = typeKeys(t, m, "exit 3")
	m = press(t, m, "enter")

	app := m.(Model)
	if app.mode != modeView {
		t.Fatalf("mode = %v, want the pager", app.mode)
	}
	if view := app.View(); !strings.Contains(view, "exit status 3") {
		t.Errorf("the failure is not reported:\n%s", view)
	}
}

// An empty command line is refused rather than running a shell that does nothing.
func TestRunCommandNeedsSomething(t *testing.T) {
	m := newInDir(t, t.TempDir())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.mode != modeCommand {
		t.Fatalf("mode = %v, want the prompt to stay open", app.mode)
	}
	if app.command.status == "" {
		t.Error("nothing said why it did not run")
	}
}

// The placeholders carry the pane's state into the command line, quoted so a
// filename cannot be read as shell syntax.
func TestExpandCommand(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	awkward := "it's a file.txt"
	if err := os.WriteFile(filepath.Join(left, awkward), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, twoPanes(t, left, right), awkward).(Model)

	got := m.expandCommand("wc -c %f in %d then %D")
	want := "wc -c 'it'\\''s a file.txt' in '" + left + "' then '" + right + "'"
	if got != want {
		t.Fatalf("expanded to\n%s\nwant\n%s", got, want)
	}

	if got := m.expandCommand("echo 100%%"); got != "echo 100%" {
		t.Errorf("%%%% expanded to %q, want a literal percent", got)
	}
}

// A selection becomes a list of quoted words, and the full path is available too.
func TestExpandCommandSelection(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := focusEntry(t, newInDir(t, dir), "a.txt")
	m = press(t, m, "space") // marks a.txt and steps to b.txt
	m = press(t, m, "space") // marks b.txt

	app := m.(Model)
	got := app.expandCommand("cat %s")
	if !strings.Contains(got, "'a.txt'") || !strings.Contains(got, "'b.txt'") {
		t.Fatalf("expanded to %q, want both marked files", got)
	}

	full := app.expandCommand("stat %F")
	if !strings.Contains(full, dir) {
		t.Errorf("%%F expanded to %q, want the full path", full)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":    "'plain'",
		"":         "''",
		"it's":     `'it'\''s'`,
		"a b":      "'a b'",
		"$(rm -r)": "'$(rm -r)'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

// The directory tyr exits in is written for a shell wrapper to read.
func TestWriteCDFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "cd")

	if err := WriteCDFile(out, "/home/kim/src"); err != nil {
		t.Fatalf("write: %v", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "/home/kim/src\n" {
		t.Errorf("wrote %q", body)
	}

	// No path, or no directory, writes nothing rather than failing.
	if err := WriteCDFile("", "/home/kim"); err != nil {
		t.Errorf("empty path: %v", err)
	}
	if err := WriteCDFile(out, ""); err != nil {
		t.Errorf("empty directory: %v", err)
	}
}

// ActiveDir follows the active pane, and reports nothing for a remote one.
func TestActiveDir(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	m := twoPanes(t, left, right).(Model)

	if got := m.ActiveDir(); got != left {
		t.Errorf("ActiveDir = %q, want %q", got, left)
	}
	m.active = 1
	if got := m.ActiveDir(); got != right {
		t.Errorf("ActiveDir after switching = %q, want %q", got, right)
	}
}
