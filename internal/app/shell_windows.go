package app

import (
	"os"
	"strings"
)

// See shell_unix.go: the shell and its quoting are the platform-dependent part
// of `!` and `x`.

// interactiveShell is what `!` hands the terminal to. $SHELL wins when it is
// set, because someone who exports it on Windows means it; otherwise it is
// whatever %COMSPEC% points at, which is cmd.exe on every stock install.
func interactiveShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		return comspec
	}
	return "cmd.exe"
}

// shellCommand is how one command line is run non-interactively. /C runs it and
// exits; /S with the whole line quoted is what keeps cmd from re-parsing the
// quotes inside it.
func shellCommand(line string) (name string, args []string) {
	shell := "cmd.exe"
	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		shell = comspec
	}
	return shell, []string{"/C", line}
}

// shellQuote wraps a value so cmd.exe reads it as one word. cmd has no escape
// for a double quote inside a quoted string, so a value containing one is
// quoted around it: `a"b` becomes `"a"^"b"`. A caret escapes outside quotes.
func shellQuote(s string) string {
	if s == "" {
		return `""`
	}
	parts := strings.Split(s, `"`)
	for i, part := range parts {
		parts[i] = `"` + part + `"`
	}
	return strings.Join(parts, `^"`)
}
