package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
// otherwise read as syntax.

// commandOutputLimit caps what is kept from a command that will not stop
// printing. Past this the output is cut and said to be cut.
const commandOutputLimit = 256 * 1024

// commandState is the prompt and the run that follows it.
type commandState struct {
	input   textinput.Model
	dir     string
	line    string // what is running, for the "running…" box
	status  string
	cancel  context.CancelFunc
	paneIdx int
}

// commandDoneMsg carries a finished command back to the UI.
type commandDoneMsg struct {
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
	ctx, cancel := context.WithCancel(context.Background())

	m.command.cancel = cancel
	m.command.line = expanded
	m.command.input.Blur()
	m.mode = modeRunning

	dir := m.command.dir
	return m, func() tea.Msg {
		name, args := shellCommand(expanded)
		c := exec.CommandContext(ctx, name, args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "TYR=1")
		out, err := c.CombinedOutput()

		text := string(out)
		if len(text) > commandOutputLimit {
			text = text[:commandOutputLimit] + "\n…output cut here\n"
		}
		return commandDoneMsg{line: expanded, output: text, err: err}
	}
}

// onCommandDone shows what the command printed, in the pager the viewer uses.
func (m Model) onCommandDone(msg commandDoneMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeRunning {
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
		m.cancelCommand()
		m.mode = modeNormal
		m.noticeText = "command cancelled"
	}
	return m, nil
}

func (m *Model) cancelCommand() {
	if m.command.cancel != nil {
		m.command.cancel()
		m.command.cancel = nil
	}
}

func (m *Model) closeCommand() {
	m.cancelCommand()
	m.mode = modeNormal
	m.command.input.Blur()
	m.command = commandState{}
}

// expandCommand replaces the placeholders with what the panes are pointing at.
func (m Model) expandCommand(line string) string {
	p := &m.panes[m.active]
	other := &m.panes[1-m.active]

	current := ""
	if cur, ok := p.Current(); ok && cur.Name != ".." {
		current = cur.Name
	}

	selection := make([]string, 0, 4)
	for _, name := range p.SelectedNames() {
		selection = append(selection, shellQuote(name))
	}

	replacer := strings.NewReplacer(
		"%%", "\x00",
		"%f", shellQuote(current),
		"%F", shellQuote(filepath.Join(p.Path, current)),
		"%d", shellQuote(p.Path),
		"%D", shellQuote(other.Path),
		"%s", strings.Join(selection, " "),
	)
	return strings.ReplaceAll(replacer.Replace(line), "\x00", "%")
}

// renderCommand draws the prompt.
func (m Model) renderCommand() string {
	lines := []string{
		ui.DialogTitle.Render("Run a command"),
		"",
		ui.Faint.Render("in " + truncTail(m.command.dir, contentWidth-3)),
		ui.AddrEdit.Width(contentWidth).Render(m.command.input.View()),
		"",
		ui.Faint.Render("%f name · %F path · %s selection · %d dir · %D other"),
	}
	if m.command.status != "" {
		lines = append(lines, "", ui.Danger.Render(truncTail(m.command.status, contentWidth)))
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
		truncTail(m.command.line, contentWidth),
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
