package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// See shell_unix.go: the shell, how a line is started and how a value reaches
// it are the platform-dependent part of `!` and `x`.

// interactiveShell is what `!` hands the terminal to. $SHELL wins when it is
// set, because someone who exports it on Windows means it; otherwise it is
// whatever %COMSPEC% points at, which is cmd.exe on every stock install.
func interactiveShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return comspec()
}

func comspec() string {
	if c := os.Getenv("COMSPEC"); c != "" {
		return c
	}
	return "cmd.exe"
}

// shellCmd runs one command line through cmd /S /C. The command line is
// written out by hand: os/exec would quote the line as one argument and escape
// its quotes as \", which is the C runtime's convention and not cmd's — cmd
// would see the backslashes. With /S, cmd takes everything between the first
// and the last quote after /C as the command, exactly as written.
func shellCmd(ctx context.Context, line string) *exec.Cmd {
	shell := comspec()
	c := exec.CommandContext(ctx, shell)
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdLine(shell, line)}
	// Cancelling kills the tree, not just cmd: what cmd started would
	// otherwise run on, holding the output pipe open.
	c.Cancel = func() error {
		if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid)).Run(); err != nil {
			return c.Process.Kill()
		}
		return nil
	}
	return c
}

// cmdLine is the whole command line shellCmd starts.
func cmdLine(shell, line string) string {
	return syscall.EscapeArg(shell) + ` /S /C "` + line + `"`
}

// shellWord puts a value in front of cmd without letting cmd read it.
//
// Quoting alone cannot do that: cmd expands %VAR% inside double quotes, so a
// file called %COMSPEC%.txt would arrive as C:\Windows\system32\cmd.exe.txt,
// and there is no escape for % that works inside quotes. So the value goes
// into the environment as TYR_ARG_n, and the line gets "%TYR_ARG_n%" in its
// place. cmd expands a variable once and never expands what it expanded to,
// and a Windows path cannot contain the double quote that would end the
// quoting around it — so whatever the name holds, it is one argument and
// nothing more.
//
// The program on the other end splits its own command line, and there a run
// of backslashes just before a closing quote escapes that quote: C:\dir\ is
// passed as C:\dir\\ to arrive intact.
func shellWord(value string, n int) (run, env, shown string) {
	name := fmt.Sprintf("TYR_ARG_%d", n)
	trailing := len(value) - len(strings.TrimRight(value, `\`))
	return `"%` + name + `%"`, name + "=" + value + strings.Repeat(`\`, trailing), `"` + value + `"`
}
