//go:build !windows

package app

import (
	"os"
	"strings"
)

// The shell tyr reaches for, and how a value is quoted for it, are the two
// things that differ between platforms. Everything else about `!` and `x` is
// the same code.

// interactiveShell is what `!` hands the terminal to.
func interactiveShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/sh"
}

// shellCommand is how one command line is run non-interactively.
func shellCommand(line string) (name string, args []string) {
	return "sh", []string{"-c", line}
}

// shellQuote wraps a value so the shell reads it as one word, whatever is in
// it. Single quotes protect everything; an embedded one closes, escapes and
// reopens.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
