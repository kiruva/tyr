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

// shellQuote wraps a value so cmd.exe reads it as one word. Two rules meet here:
//
//   - cmd has no escape for a double quote inside a quoted string, so a value
//     containing one is quoted around it. `a"b` becomes `"a"^"b"`: the quoted
//     `"a"`, then a caret-escaped quote, which works only outside quotes, then
//     the quoted `"b"`. The pieces are one argument because nothing separates
//     them.
//   - The program on the other end splits its own command line, and there a
//     backslash run immediately before a closing quote escapes that quote. So
//     `C:\dir\` has to be written `"C:\dir\"` to arrive intact.
func shellQuote(s string) string {
	if s == "" {
		return `""`
	}
	parts := strings.Split(s, `"`)
	for i, part := range parts {
		trailing := len(part) - len(strings.TrimRight(part, `\`))
		parts[i] = `"` + part + strings.Repeat(`\`, trailing) + `"`
	}
	return strings.Join(parts, `^"`)
}
