//go:build !windows

package app

import "testing"

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

func TestShellCommand(t *testing.T) {
	name, args := shellCommand("echo hi")
	if name != "sh" || len(args) != 2 || args[0] != "-c" || args[1] != "echo hi" {
		t.Errorf("shellCommand = %q %q, want sh -c", name, args)
	}
}
