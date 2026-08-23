package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/remote"
	"github.com/kiruva/tyr/internal/ui"
)

// The create prompt is a one-field modal naming a new file or directory in the
// active pane's current location. A local create is instant, so it happens
// inline; a remote one costs a round trip and runs as a command instead.

// createState is everything the prompt needs while it is open.
type createState struct {
	input   textinput.Model
	dir     bool // creating a directory rather than a file
	paneIdx int  // the pane the prompt was opened from
	status  string
}

// createdMsg reports the outcome of a remote create.
type createdMsg struct {
	pane int
	name string // as typed, for highlighting the result
	dir  bool
	err  error
}

// openCreate opens the prompt for a new file (dir = false) or directory.
func (m *Model) openCreate(dir bool) tea.Cmd {
	p := &m.panes[m.active]
	if p.InArchive() {
		m.errText = "not available inside an archive — unpack it first"
		return nil
	}

	ti := newConnInput("name", false, contentWidth-2)
	m.create = createState{input: ti, dir: dir, paneIdx: m.active}
	m.mode = modeCreate
	return m.create.input.Focus()
}

// onCreateKey drives the prompt. A rejected name keeps the prompt open with the
// reason under the field so it can be corrected.
func (m Model) onCreateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeCreate()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		return m.commitCreate()
	}
	m.create.status = ""
	var cmd tea.Cmd
	m.create.input, cmd = m.create.input.Update(msg)
	return m, cmd
}

// commitCreate validates the typed name and creates it.
func (m Model) commitCreate() (tea.Model, tea.Cmd) {
	name, err := cleanNewName(m.create.input.Value())
	if err != nil {
		m.create.status = err.Error()
		return m, nil
	}

	idx := m.create.paneIdx
	p := &m.panes[idx]
	dir := m.create.dir

	if p.IsRemote() {
		target := remote.Join(p.Path, name)
		m.closeCreate()
		return m, createRemoteCmd(idx, p.Host(), target, name, dir)
	}

	if err := createLocal(filepath.Join(p.Path, name), dir); err != nil {
		m.create.status = err.Error()
		return m, nil
	}
	m.closeCreate()
	p.Refresh()
	p.Focus(firstSegment(name))
	return m, nil
}

func (m *Model) closeCreate() {
	m.mode = modeNormal
	m.create.input.Blur()
	m.create = createState{}
}

// createLocal makes the name on this machine.
func createLocal(target string, dir bool) error {
	if dir {
		return fileops.CreateDir(target)
	}
	return fileops.CreateFile(target)
}

// createRemoteCmd makes the name on the far side, off the UI thread.
func createRemoteCmd(paneIdx int, h remote.Host, target, name string, dir bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if dir {
			err = remote.Mkdir(h, target)
		} else {
			err = remote.Touch(h, target)
		}
		return createdMsg{pane: paneIdx, name: name, dir: dir, err: err}
	}
}

// onCreated refreshes the pane a remote create landed in, or reports why it
// didn't. The listing is asynchronous, so the new entry is highlighted by the
// pane once it arrives.
func (m Model) onCreated(msg createdMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.errText = createVerb(msg.dir) + " failed: " + msg.err.Error()
		return m, nil
	}
	m.panes[msg.pane].FocusAfterLoad(firstSegment(msg.name))
	return m, m.refreshPane(msg.pane)
}

// cleanNewName normalizes what was typed and rejects what create cannot mean.
// A relative name with separators is allowed — "src/main.go" creates the missing
// directories on the way — but absolute paths and "..", which would put the new
// name somewhere other than this pane, are not.
func cleanNewName(in string) (string, error) {
	name := strings.TrimSpace(in)
	if name == "" {
		return "", fmt.Errorf("enter a name")
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "~") {
		return "", fmt.Errorf("enter a name, not a path — use the address bar to move")
	}
	name = strings.TrimRight(name, "/")
	name = strings.TrimRight(name, string(filepath.Separator))
	if name == "" {
		return "", fmt.Errorf("enter a name")
	}
	for _, seg := range strings.FieldsFunc(name, isSeparator) {
		if seg == "." || seg == ".." {
			return "", fmt.Errorf("%q is not a name", seg)
		}
	}
	return name, nil
}

func isSeparator(r rune) bool { return r == '/' || r == filepath.Separator }

// firstSegment is the part of a name that will show up as an entry in the pane.
func firstSegment(name string) string {
	if i := strings.IndexFunc(name, isSeparator); i >= 0 {
		return name[:i]
	}
	return name
}

func createVerb(dir bool) string {
	if dir {
		return "New folder"
	}
	return "New file"
}

// renderCreate draws the prompt, sized like the connection modal so the two
// don't jump around relative to each other.
func (m Model) renderCreate() string {
	p := &m.panes[m.create.paneIdx]
	where := p.Path
	if p.IsRemote() {
		where = p.Host().Display(p.Path)
	}

	lines := []string{
		ui.DialogTitle.Render(createVerb(m.create.dir)),
		"",
		ui.Faint.Render("in " + truncTail(where, contentWidth)),
		ui.AddrEdit.Width(contentWidth).Render(m.create.input.View()),
	}
	if m.create.status != "" {
		lines = append(lines, "", ui.Danger.Render(truncTail(m.create.status, contentWidth)))
	}
	lines = append(lines, "",
		ui.DialogHint.Render("enter")+" create    "+ui.DialogHint.Render("esc")+" cancel")

	return ui.Dialog.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
