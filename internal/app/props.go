package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
)

// The properties dialog answers "what is this, exactly" — type, size, times,
// owner, link target — and lets the one field worth editing be edited: the
// permissions. A directory can take them recursively, which is the reason a
// chmod goes through the job machinery rather than happening inline.

// propsField is a focusable row in the dialog.
type propsField int

const (
	propsFieldMode propsField = iota
	propsFieldRecursive
	propsFieldCount
)

// propsState is the dialog while it is open.
type propsState struct {
	paneIdx int
	path    string
	name    string

	info  os.FileInfo
	link  string // symlink target, when it is one
	owner string
	group string
	links uint64

	mode      textinput.Model // octal permissions, editable
	orig      string          // what the mode field started as
	recursive bool
	focus     propsField
	status    string
}

// openProps reads everything about the highlighted entry and shows it.
func (m *Model) openProps() tea.Cmd {
	p := &m.panes[m.active]
	if p.IsRemote() {
		m.errText = "properties are local-only — the listing over ssh is all tyr knows"
		return nil
	}
	if p.InArchive() {
		m.errText = "not available inside an archive — unpack it first"
		return nil
	}
	cur, ok := p.Current()
	if !ok || cur.Name == ".." {
		m.errText = "select an entry"
		return nil
	}

	full := filepath.Join(p.Path, cur.Name)
	info, err := os.Lstat(full)
	if err != nil {
		m.errText = err.Error()
		return nil
	}

	s := propsState{paneIdx: m.active, path: full, name: cur.Name, info: info}
	if info.Mode()&os.ModeSymlink != 0 {
		s.link, _ = os.Readlink(full)
	}
	s.owner, s.group, s.links = ownership(info)

	s.orig = fmt.Sprintf("%04o", info.Mode().Perm())
	s.mode = newConnInput("644", false, 8)
	s.mode.SetValue(s.orig)
	s.mode.CursorEnd()

	m.props = s
	m.mode = modeProps
	return m.props.mode.Focus()
}

// onPropsKey drives the dialog.
func (m Model) onPropsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.closeProps()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "tab", "down", "shift+tab", "up":
		return m.movePropsFocus(msg.String())
	case " ":
		if m.props.focus == propsFieldRecursive {
			m.props.recursive = !m.props.recursive
			return m, nil
		}
	case "enter":
		return m.applyProps()
	}

	m.props.status = ""
	var cmd tea.Cmd
	if m.props.focus == propsFieldMode {
		m.props.mode, cmd = m.props.mode.Update(msg)
	}
	return m, cmd
}

// movePropsFocus walks between the mode field and the recursive switch, which
// only exists for a directory.
func (m Model) movePropsFocus(key string) (tea.Model, tea.Cmd) {
	if !m.props.info.IsDir() {
		return m, nil
	}
	step := 1
	if key == "shift+tab" || key == "up" {
		step = -1
	}
	m.props.focus = propsField((int(m.props.focus) + step + int(propsFieldCount)) % int(propsFieldCount))

	m.props.mode.Blur()
	if m.props.focus == propsFieldMode {
		return m, m.props.mode.Focus()
	}
	return m, nil
}

// applyProps validates the typed permissions and runs the change. Nothing
// changed means nothing runs — closing the dialog is not an operation.
func (m Model) applyProps() (tea.Model, tea.Cmd) {
	typed := strings.TrimSpace(m.props.mode.Value())
	if typed == m.props.orig && !m.props.recursive {
		m.closeProps()
		return m, nil
	}

	perm, err := parseMode(typed)
	if err != nil {
		m.props.status = err.Error()
		return m, nil
	}
	if m.props.info.Mode()&os.ModeSymlink != 0 {
		m.props.status = "a symlink has no permissions of its own"
		return m, nil
	}

	job := fileops.Job{
		Op:        fileops.OpChmod,
		Srcs:      []string{m.props.path},
		Mode:      perm,
		Recursive: m.props.recursive && m.props.info.IsDir(),
	}
	m.closeProps()
	m.pending = job
	return m, m.startPending()
}

func (m *Model) closeProps() {
	m.mode = modeNormal
	m.props.mode.Blur()
	m.props = propsState{}
}

// parseMode reads the octal permissions a person types: "644", "0644", "755".
func parseMode(in string) (os.FileMode, error) {
	if in == "" {
		return 0, fmt.Errorf("enter permissions as octal, e.g. 644")
	}
	value, err := strconv.ParseUint(in, 8, 32)
	if err != nil || value > 0o7777 {
		return 0, fmt.Errorf("%q is not octal permissions — try 644 or 755", in)
	}
	return os.FileMode(value), nil
}

// entryKind describes what the entry is, in one word.
func entryKind(info os.FileInfo) string {
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode.IsDir():
		return "directory"
	case mode&os.ModeNamedPipe != 0:
		return "named pipe"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeDevice != 0:
		return "device"
	case !mode.IsRegular():
		return "special file"
	default:
		return "file"
	}
}
