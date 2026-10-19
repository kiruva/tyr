package app

import (
	"os"
	"path/filepath"
	"runtime"
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

	got := m.expandCommand("wc -c %f in %d then %D").shown
	want := "wc -c " + shownWord(awkward) + " in " + shownWord(left) + " then " + shownWord(right)
	if got != want {
		t.Fatalf("expanded to\n%s\nwant\n%s", got, want)
	}

	for _, line := range []string{"echo 100%%", "echo 50%", "echo %x"} {
		want := strings.ReplaceAll(line, "%%", "%")
		if got := m.expandCommand(line); got.run != want || got.shown != want {
			t.Errorf("%q expanded to %+v, want %q", line, got, want)
		}
	}
}

// shownWord is how a value appears in the line the user is shown.
func shownWord(value string) string {
	_, _, shown := shellWord(value, 1)
	return shown
}

// A name that starts with a dash reaches the command as a path, not an
// option: rm %f on a file called -rf removes that file.
func TestExpandCommandNamesAreNotOptions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "-rf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := focusEntry(t, newInDir(t, dir), "-rf").(Model)
	want := shownWord("." + string(filepath.Separator) + "-rf")
	if got := m.expandCommand("rm %f").shown; got != "rm "+want {
		t.Errorf("expanded to %q, want rm %s", got, want)
	}
}

// End to end: a name full of what a shell reads as syntax is handed to the
// command as that name, and nothing in it runs.
func TestRunCommandWithHostileName(t *testing.T) {
	dir := t.TempDir()
	name := "it's $(touch pwned) & echo %PATH% ;x.txt"
	if runtime.GOOS == "windows" {
		name = "it's & echo %PATH% ^ ;x.txt" // no $( ) meaning, and no " allowed
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("contents-ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	show := "cat %f"
	if runtime.GOOS == "windows" {
		show = "type %f"
	}

	m := focusEntry(t, newInDir(t, dir), name)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = typeKeys(t, m, show)
	m = press(t, m, "enter")

	view := m.(Model).View()
	if !strings.Contains(view, "contents-ok") {
		t.Fatalf("the file was not read by its name:\n%s", view)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Fatal("part of the name ran as a command")
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
	got := app.expandCommand("cat %s").shown
	if !strings.Contains(got, shownWord("a.txt")) || !strings.Contains(got, shownWord("b.txt")) {
		t.Fatalf("expanded to %q, want both marked files", got)
	}

	full := app.expandCommand("stat %F").shown
	if !strings.Contains(full, dir) {
		t.Errorf("%%F expanded to %q, want the full path", full)
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
