package app

import (
	"context"
	"os"
	"strings"
	"testing"
)

// A value reaches cmd through the environment, never through the line: cmd
// expands %VAR% inside quotes, so no quoting of the name itself is safe.
func TestShellWord(t *testing.T) {
	for _, tt := range []struct {
		in, env, shown string
	}{
		{"plain", "TYR_ARG_3=plain", `"plain"`},
		{"%COMSPEC%.txt", "TYR_ARG_3=%COMSPEC%.txt", `"%COMSPEC%.txt"`},
		{"a & del C:\\", `TYR_ARG_3=a & del C:\\`, `"a & del C:\"`},
		// A trailing backslash is doubled so it escapes itself rather than the
		// quote that closes the argument.
		{`C:\dir\`, `TYR_ARG_3=C:\dir\\`, `"C:\dir\"`},
	} {
		run, env, shown := shellWord(tt.in, 3)
		if run != `"%TYR_ARG_3%"` || env != tt.env || shown != tt.shown {
			t.Errorf("shellWord(%q) = %s, %s, %s; want \"%%TYR_ARG_3%%\", %s, %s", tt.in, run, env, shown, tt.env, tt.shown)
		}
	}
}

// The line is handed to cmd as written, inside /S /C "…". os/exec would have
// quoted it as one argument and escaped its quotes as \", which cmd does not
// read as an escape.
func TestShellCmdLine(t *testing.T) {
	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)
	c := shellCmd(context.Background(), `type "%TYR_ARG_1%"`)
	want := `C:\Windows\System32\cmd.exe /S /C "type "%TYR_ARG_1%""`
	if got := c.SysProcAttr.CmdLine; got != want {
		t.Errorf("command line = %s, want %s", got, want)
	}

	t.Setenv("COMSPEC", `C:\Program Files\shell\cmd.exe`)
	if got := cmdLine(os.Getenv("COMSPEC"), "ver"); !strings.HasPrefix(got, `"C:\Program Files\shell\cmd.exe" /S /C`) {
		t.Errorf("command line = %s, want the shell's path quoted", got)
	}
}

func TestInteractiveShellPrefersSHELL(t *testing.T) {
	t.Setenv("SHELL", `C:\Program Files\PowerShell\7\pwsh.exe`)
	if got := interactiveShell(); got != os.Getenv("SHELL") {
		t.Errorf("interactiveShell = %q, want $SHELL", got)
	}
}
