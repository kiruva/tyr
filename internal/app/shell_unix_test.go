//go:build !windows

package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

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

func TestShellCmd(t *testing.T) {
	c := shellCmd(context.Background(), "echo hi")
	if len(c.Args) != 3 || c.Args[1] != "-c" || c.Args[2] != "echo hi" {
		t.Errorf("args = %q, want sh -c and the line", c.Args)
	}
	out, err := c.Output()
	if err != nil || strings.TrimSpace(string(out)) != "hi" {
		t.Errorf("ran to %q, %v", out, err)
	}
}

// Esc stops the whole pipeline. Killing only sh would leave sleep running and
// holding the pipe open, and the run would not end until sleep did.
func TestShellCmdCancelKillsThePipeline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := shellCmd(ctx, "sleep 30 | cat")
	c.WaitDelay = commandWaitDelay
	var out strings.Builder
	c.Stdout = &out
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	cancel()
	_ = c.Wait()
	if took := time.Since(start); took > time.Second {
		t.Fatalf("cancel took %v: the pipeline outlived its shell", took)
	}
}

// A command that will not stop printing is stopped once it has printed what
// is kept, instead of filling memory until someone presses Esc.
func TestRunCommandStopsAtTheOutputCap(t *testing.T) {
	m := newInDir(t, t.TempDir())
	m = press(t, m, "x")
	m = typeKeys(t, m, "yes")

	start := time.Now()
	m = press(t, m, "enter") // never returns if yes is not stopped
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("stopping yes took %v", took)
	}

	app := m.(Model)
	if !strings.Contains(string(app.viewer.raw), "output cut here") {
		t.Error("the cut is not reported at the end of the output")
	}
	if strings.Contains(string(app.viewer.raw), "signal: killed") {
		t.Error("being stopped for printing too much is reported as a failure")
	}
	if n := len(app.viewer.raw); n > commandOutputLimit+200 {
		t.Errorf("kept %d bytes, want about %d", n, commandOutputLimit)
	}
}
