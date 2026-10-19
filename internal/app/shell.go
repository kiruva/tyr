package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/syntax"
	"github.com/kiruva/tyr/internal/ui"
)

// A file manager that cannot reach the shell makes you leave it to do anything
// it has no key for. Two ways out: `!` hands the terminal to $SHELL in the
// active pane's directory and takes it back when the shell exits, and `x` runs
// one command line and shows what it printed.
//
// The one-liner is where the pane's state gets in: %f is the entry under the
// cursor, %s the selection, %d this directory and %D the other one — each
// quoted, because a filename is allowed to contain anything a shell would
// otherwise read as syntax. How a value is put in front of the shell is the
// platform's to say (shellWord): quoted into the line for sh, passed through
// the environment for cmd, which expands %VAR% even inside quotes.

// commandOutputLimit caps what is kept from a command that will not stop
// printing. Past this the output is cut and said to be cut.
const commandOutputLimit = 256 * 1024

// commandWaitDelay is how long a cancelled command's output is waited for once
// it has been killed. A grandchild that kept the pipe open would otherwise hold
// the run open until it finished on its own.
const commandWaitDelay = 2 * time.Second

// commandState is the prompt and the run that follows it.
type commandState struct {
	input   textinput.Model
	dir     string
	line    string // what is running, for the "running…" box
	status  string
	task    bgTask // the run in flight
	paneIdx int
}

// commandDoneMsg carries a finished command back to the UI.
type commandDoneMsg struct {
	seq    uint64
	line   string
	output string
	err    error
}

// openShell hands the terminal over to an interactive shell.
func (m *Model) openShell() tea.Cmd {
	p := &m.panes[m.active]
	if p.IsRemote() {
		m.errText = "the shell opens on this machine — the pane is on a host"
		return nil
	}

	dir := p.Path
	if p.InArchive() {
		dir = filepath.Dir(p.ArchivePath())
	}

	c := exec.Command(interactiveShell())
	c.Dir = dir
	c.Env = append(os.Environ(), "TYR=1")

	// ExecProcess suspends the UI, gives the child the real terminal, and
	// restores the alt screen when it exits.
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return shellDoneMsg{err: err}
	})
}

// shellDoneMsg reports that the interactive shell exited.
type shellDoneMsg struct{ err error }

// onShellDone refreshes the pane the shell was opened in: it was a shell, so
// something in there has probably changed.
func (m Model) onShellDone(msg shellDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.errText = "shell: " + msg.err.Error()
		return m, nil
	}
	return m, m.refreshPane(m.active)
}

// openCommand asks for a command line to run in the active pane's directory.
func (m *Model) openCommand() tea.Cmd {
	p := &m.panes[m.active]
	if p.IsRemote() {
		m.errText = "commands run on this machine — the pane is on a host"
		return nil
	}

	dir := p.Path
	if p.InArchive() {
		dir = filepath.Dir(p.ArchivePath())
	}

	ti := newConnInput("git status, wc -l %s, …", false, contentWidth-2)
	m.command = commandState{input: ti, dir: dir, paneIdx: m.active}
	m.mode = modeCommand
	return m.command.input.Focus()
}

// onCommandKey drives the prompt.
func (m Model) onCommandKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeCommand()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		return m.runCommand()
	}

	m.command.status = ""
	var cmd tea.Cmd
	m.command.input, cmd = m.command.input.Update(msg)
	return m, cmd
}

// runCommand expands the placeholders and starts the command off the UI thread.
func (m Model) runCommand() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.command.input.Value())
	if line == "" {
		m.command.status = "type a command"
		return m, nil
	}

	expanded := m.expandCommand(line)
	ctx, seq := m.command.task.start()

	m.command.line = expanded.shown
	m.command.input.Blur()
	m.mode = modeRunning

	dir := m.command.dir
	return m, func() tea.Msg {
		ctx, stop := context.WithCancel(ctx)
		defer stop()

		c := shellCmd(ctx, expanded.run)
		c.Dir = dir
		c.Env = append(append(os.Environ(), "TYR=1"), expanded.env...)
		c.WaitDelay = commandWaitDelay

		// One writer for both streams keeps them in the order they were
		// printed. It stops the command once it has printed all that will be
		// kept, rather than holding everything a runaway one prints until Esc.
		out := &capWriter{max: commandOutputLimit, full: stop}
		c.Stdout, c.Stderr = out, out
		err := c.Run()

		text := out.buf.String()
		if out.cut {
			text += "\n…output cut here — the command was stopped\n"
			err = nil // killed for printing too much, which the line above says
		}
		return commandDoneMsg{seq: seq, line: expanded.shown, output: text, err: err}
	}
}

// capWriter keeps the first max bytes written to it and calls full once more
// than that arrives. os/exec calls Write from one goroutine at a time when
// Stdout and Stderr are the same writer, so it needs no lock.
type capWriter struct {
	buf  bytes.Buffer
	max  int
	cut  bool
	full func()
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); len(p) > room {
		w.buf.Write(p[:max(room, 0)])
		if !w.cut {
			w.cut = true
			w.full()
		}
		return len(p), nil // swallowed, so the command is stopped, not broken-piped
	}
	return w.buf.Write(p)
}

// onCommandDone shows what the command printed, in the pager the viewer uses.
func (m Model) onCommandDone(msg commandDoneMsg) (tea.Model, tea.Cmd) {
	if !m.command.task.finish(msg.seq) {
		return m, nil // cancelled, or the tool was closed
	}
	paneIdx := m.command.paneIdx
	m.closeCommand()

	body := msg.output
	if msg.err != nil {
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		body += "\n" + msg.err.Error()
	}
	if strings.TrimSpace(body) == "" {
		body = "(no output)"
	}

	m.showBytes("$ "+msg.line, []byte(body), syntax.None)
	return m, m.refreshPane(paneIdx)
}

// onRunningKey is what a keypress means while a command runs: Esc kills it.
func (m Model) onRunningKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.command.task.stop()
		m.mode = modeNormal
		m.noticeText = "command cancelled"
	}
	return m, nil
}

func (m *Model) closeCommand() {
	m.command.task.stop()
	m.mode = modeNormal
	m.command.input.Blur()
	m.command = commandState{}
}

// commandLine is a command line with its placeholders filled in: run is what
// the shell is handed, env what it needs in its environment to make sense of
// run, and shown the same line for the user to read.
type commandLine struct {
	run, shown string
	env        []string
}

// expandCommand replaces the placeholders with what the panes are pointing at.
func (m Model) expandCommand(line string) commandLine {
	p := &m.panes[m.active]
	other := &m.panes[1-m.active]

	current := ""
	if cur, ok := p.Current(); ok && cur.Name != ".." {
		current = cur.Name
	}

	var out commandLine
	word := func(value string) (run, shown string) {
		run, env, shown := shellWord(value, len(out.env)+1)
		if env != "" {
			out.env = append(out.env, env)
		}
		return run, shown
	}
	// Each placeholder becomes a run form and a shown form, filled in as it is
	// met, so a placeholder that is not in the line costs nothing.
	fill := func(token string) (string, string) {
		switch token {
		case "%f":
			return word(optionSafe(current))
		case "%F":
			return word(filepath.Join(p.Path, current))
		case "%d":
			return word(p.Path)
		case "%D":
			return word(other.Path)
		case "%s":
			var runs, shown []string
			for _, name := range p.SelectedNames() {
				r, s := word(optionSafe(name))
				runs, shown = append(runs, r), append(shown, s)
			}
			return strings.Join(runs, " "), strings.Join(shown, " ")
		}
		return "%", "%" // %%
	}

	var run, shown strings.Builder
	for len(line) > 0 {
		i := strings.IndexByte(line, '%')
		if i < 0 || i == len(line)-1 {
			run.WriteString(line)
			shown.WriteString(line)
			break
		}
		run.WriteString(line[:i])
		shown.WriteString(line[:i])
		token := line[i : i+2]
		switch token {
		case "%%", "%f", "%F", "%d", "%D", "%s":
			r, s := fill(token)
			run.WriteString(r)
			shown.WriteString(s)
		default:
			run.WriteString(token)
			shown.WriteString(token)
		}
		line = line[i+2:]
	}
	out.run, out.shown = run.String(), shown.String()
	return out
}

// optionSafe keeps a relative name from being read as an option: rm %f on a
// file called -rf must remove the file, not run rm -rf. A ./ in front changes
// nothing about which file it is.
func optionSafe(name string) string {
	if strings.HasPrefix(name, "-") {
		return "." + string(filepath.Separator) + name
	}
	return name
}

// renderCommand draws the prompt.
func (m Model) renderCommand() string {
	lines := []string{
		ui.DialogTitle.Render("Run a command"),
		"",
		ui.Faint.Render("in " + ui.TruncTail(m.command.dir, contentWidth-3)),
		ui.AddrEdit.Width(contentWidth).Render(m.command.input.View()),
		"",
		ui.Faint.Render("%f name · %F path · %s selection · %d dir · %D other"),
	}
	if m.command.status != "" {
		lines = append(lines, "", ui.Danger.Render(ui.TruncTail(m.command.status, contentWidth)))
	}
	lines = append(lines, "",
		ui.DialogHint.Render("enter")+" run    "+ui.DialogHint.Render("esc")+" cancel")

	return ui.Dialog.Width(dialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// renderRunning is the box shown while a command is running.
func (m Model) renderRunning() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		ui.DialogTitle.Render("Running"),
		"",
		ui.TruncTail(m.command.line, contentWidth),
		"",
		ui.DialogHint.Render("esc")+" cancel",
	)
	return ui.Dialog.Width(dialogWidth).Render(content)
}

// ActiveDir is the directory the active pane is in, for the --cd-file handoff
// that lets a shell wrapper follow tyr out of the app.
func (m Model) ActiveDir() string {
	p := &m.panes[m.active]
	switch {
	case p.IsRemote():
		return ""
	case p.InArchive():
		return filepath.Dir(p.ArchivePath())
	default:
		return p.Path
	}
}

// WriteCDFile records a directory for a shell wrapper to read after tyr exits,
// which is how "cd where I left off" is done without tyr being able to change
// its parent's working directory.
func WriteCDFile(path, dir string) error {
	if path == "" || dir == "" {
		return nil
	}
	return os.WriteFile(path, []byte(dir+"\n"), 0o600)
}

// CDFileEnv is the environment variable that names the file, for callers that
// would rather not pass --cd-file every time.
const CDFileEnv = "TYR_CD_FILE"
