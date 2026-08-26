package app

import (
	"os"
	"strings"
	"testing"
)

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":           `"plain"`,
		"":                `""`,
		"a b":             `"a b"`,
		`say "hi"`:        `"say "^"hi"^""`,
		"& del C:\\":      `"& del C:\"`,
		"it's a file.txt": `"it's a file.txt"`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

// Whatever is quoted, cmd must read it as one argument — so the metacharacters
// that would otherwise run a second command stay inside the quotes.
func TestShellQuoteContainsMetacharacters(t *testing.T) {
	for _, in := range []string{"a&b", "a|b", "a>b", "a^b", "a%PATH%b"} {
		got := shellQuote(in)
		if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
			t.Errorf("shellQuote(%q) = %s, want it wrapped in quotes", in, got)
		}
	}
}

func TestShellCommand(t *testing.T) {
	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)
	name, args := shellCommand("echo hi")
	if name != os.Getenv("COMSPEC") {
		t.Errorf("shell = %q, want %%COMSPEC%%", name)
	}
	if len(args) != 2 || args[0] != "/C" || args[1] != "echo hi" {
		t.Errorf("args = %q, want /C and the line", args)
	}
}

func TestInteractiveShellPrefersSHELL(t *testing.T) {
	t.Setenv("SHELL", `C:\Program Files\PowerShell\7\pwsh.exe`)
	if got := interactiveShell(); got != os.Getenv("SHELL") {
		t.Errorf("interactiveShell = %q, want $SHELL", got)
	}
}
