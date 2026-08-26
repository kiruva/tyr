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

// A recursive change reaches everything inside a directory.
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
	press(t, m, "enter")

	info, err := os.Stat(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("nested file mode = %o, want 700", got)
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
		got, err := parseMode(in)
		if err != nil {
			t.Errorf("parseMode(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseMode(%q) = %o, want %o", in, got, want)
		}
	}
	for _, in := range []string{"", "999", "rwx", "77777"} {
		if _, err := parseMode(in); err == nil {
			t.Errorf("parseMode(%q) was accepted", in)
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
