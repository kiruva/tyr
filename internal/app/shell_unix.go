//go:build !windows

package app

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// The shell tyr reaches for, how a command line is started, and how a value is
// put in front of the shell are what differ between platforms. Everything else
// about `!` and `x` is the same code.

// interactiveShell is what `!` hands the terminal to.
func interactiveShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/sh"
}

// shellCmd runs one command line non-interactively, in a process group of its
// own. Cancelling ctx kills the whole group: sh is only the first process of
// `make | tee log`, and killing it alone would leave the rest running, holding
// the output pipe open.
func shellCmd(ctx context.Context, line string) *exec.Cmd {
	c := exec.CommandContext(ctx, "sh", "-c", line)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	return c
}

// shellWord puts a value into the line as one word, quoted: sh reads nothing
// inside single quotes as syntax. Nothing goes through the environment, and
// what runs is what is shown.
func shellWord(value string, _ int) (run, env, shown string) {
	q := shellQuote(value)
	return q, "", q
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
