package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// i shows what the entry is, and the permissions field can change it.
func TestPropsShowsAndChmods(t *testing.T) {
	skipWithoutUnixPerms(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, newInDir(t, dir), "script.sh")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})

	app := m.(Model)
	if app.mode != modeProps {
		t.Fatalf("mode = %v, want modeProps", app.mode)
	}
	view := app.View()
	for _, want := range []string{"script.sh", "file", "permissions"} {
		if !strings.Contains(view, want) {
			t.Errorf("the dialog does not mention %q:\n%s", want, view)
		}
	}
	if app.props.mode.Value() != "0644" {
		t.Errorf("mode field = %q, want 0644", app.props.mode.Value())
	}

	// Replace the value with 755 and apply.
	for range 4 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m = typeKeys(t, m, "755")
	m = press(t, m, "enter")

	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("mode = %o, want 755", got)
	}
	if mode := m.(Model).mode; mode != modeNormal {
		t.Errorf("mode = %v after applying, want modeNormal", mode)
	}
}

// +x is enough for someone told to make a script executable, and the dialog
// shows what it comes to before Enter.
func TestPropsSymbolicPlusX(t *testing.T) {
	skipWithoutUnixPerms(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := focusEntry(t, newInDir(t, dir), "script.sh")
	m = typeKeys(t, m, "i")
	for range 4 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m = typeKeys(t, m, "+x")
	if view := m.(Model).View(); !strings.Contains(view, "0755 rwxr-xr-x") {
		t.Errorf("no preview of the result:\n%s", view)
	}
	press(t, m, "enter")

	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("mode = %o, want 755", got)
	}
}

// A recursive change reaches everything inside a directory, once confirmed. A
// folder keeps execute so it can still be opened; a file that was no program
// does not become one.
func TestPropsChmodRecursive(t *testing.T) {
	skipWithoutUnixPerms(t)

	dir := t.TempDir()
	sub := filepath.Join(dir, "tree")
	if err := os.MkdirAll(filepath.Join(sub, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(sub, "inner", "file.txt")
	if err := os.WriteFile(nested, []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, newInDir(t, dir), "tree")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})

	// Move to the switch and turn it on.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = press(t, m, "space")
	if !m.(Model).props.recursive {
		t.Fatal("the recursive switch did not turn on")
	}

	// Back to the field, replace the value, apply.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	for range 4 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m = typeKeys(t, m, "700")
	m = press(t, m, "enter")

	app := m.(Model)
	if app.mode != modeConfirm {
		t.Fatalf("mode = %v, want the change confirmed first", app.mode)
	}
	if view := app.View(); !strings.Contains(view, "folders → 0700") || !strings.Contains(view, "files → 0600") {
		t.Errorf("the prompt does not say what folders and files get:\n%s", view)
	}
	press(t, m, "y")

	for path, want := range map[string]os.FileMode{nested: 0o600, filepath.Join(sub, "inner"): 0o700} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s = %o, want %o", filepath.Base(path), got, want)
		}
	}
}

// Something that is not octal is refused with the reason on screen.
func TestPropsRejectsBadMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, newInDir(t, dir), "a.txt")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	for range 4 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m = typeKeys(t, m, "999")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.mode != modeProps {
		t.Fatalf("mode = %v, want the dialog to stay open", app.mode)
	}
	if app.props.status == "" {
		t.Error("nothing explained why the value was refused")
	}
}

func TestParseMode(t *testing.T) {
	cases := map[string]os.FileMode{"644": 0o644, "0755": 0o755, "600": 0o600, "1777": 0o1777}
	for in, want := range cases {
		got, err := parseMode(in, 0o644, false)
		if err != nil {
			t.Errorf("parseMode(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseMode(%q) = %o, want %o", in, got, want)
		}
	}
	for _, in := range []string{"", "999", "rwx", "77777", "u", "+q", "u+x,"} {
		if _, err := parseMode(in, 0o644, false); err == nil {
			t.Errorf("parseMode(%q) was accepted", in)
		}
	}
}

// Symbolic modes work from what the entry has now, as chmod's do.
func TestParseSymbolicMode(t *testing.T) {
	cases := []struct {
		in    string
		cur   os.FileMode
		isDir bool
		want  os.FileMode
	}{
		{"+x", 0o644, false, 0o755},
		{"u+x", 0o644, false, 0o744},
		{"go-w", 0o666, false, 0o644},
		{"a=r", 0o755, false, 0o444},
		{"u=rwx,g=rx,o=", 0o600, false, 0o750},
		{"u+rw,g-w", 0o464, false, 0o644},
		{"+X", 0o644, false, 0o644},
		{"+X", 0o644, true, 0o755},
		{"+X", 0o744, false, 0o755},
		{"u+s", 0o755, false, 0o4755},
		{"+t", 0o777, true, 0o1777},
		{"-x+w", 0o555, false, 0o666},
	}
	for _, c := range cases {
		got, err := parseMode(c.in, c.cur, c.isDir)
		if err != nil || got != c.want {
			t.Errorf("parseMode(%q) on %04o = %04o, %v; want %04o", c.in, c.cur, got, err, c.want)
		}
	}
}

// skipWithoutUnixPerms skips a test that asserts on permission bits. Windows has
// none to assert on: os.Chmod there flips the read-only attribute and nothing
// else, so a mode of 0755 is not a thing a file can come back with.
func skipWithoutUnixPerms(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
}
