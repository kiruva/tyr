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
	s.mode = newInput("644, +x", false, 10)
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

	perm, err := parseMode(typed, m.props.info.Mode(), m.props.info.IsDir())
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
	if job.Recursive {
		// A whole tree is a lot to change on one Enter, and there is no undo:
		// the confirm prompt says what folders and files will each get.
		m.undoing, m.willOverwrite = false, false
		m.mode = modeConfirm
		return m, nil
	}
	return m, m.startPending()
}

func (m *Model) closeProps() {
	m.mode = modeNormal
	m.props.mode.Blur()
	m.props = propsState{}
}

// parseMode reads the permissions a person types: octal ("644", "0755") or
// chmod's symbolic form ("+x", "u+x", "go-w", "a=r", "u+rw,g-w"), the second
// worked out from cur, what the entry has now. No who means everyone — chmod
// would mask that with the umask, which matters for +w and not for +x.
func parseMode(in string, cur os.FileMode, isDir bool) (os.FileMode, error) {
	if in == "" {
		return 0, fmt.Errorf("enter permissions, e.g. 644, 755 or +x")
	}
	if in[0] >= '0' && in[0] <= '9' {
		value, err := strconv.ParseUint(in, 8, 32)
		if err != nil || value > 0o7777 {
			return 0, fmt.Errorf("%q is not octal permissions — try 644 or 755", in)
		}
		return os.FileMode(value), nil
	}
	mode, ok := symbolicMode(in, uint32(cur.Perm()), isDir)
	if !ok {
		return 0, fmt.Errorf("%q is not a mode — try 644, 755, +x or go-w", in)
	}
	return os.FileMode(mode), nil
}

// symbolicMode applies chmod clauses like "u+x,go-w" to mode.
func symbolicMode(in string, mode uint32, isDir bool) (uint32, bool) {
	for _, clause := range strings.Split(in, ",") {
		var who, special uint32
		i := 0
		for ; i < len(clause) && strings.IndexByte("ugoa", clause[i]) >= 0; i++ {
			switch clause[i] {
			case 'u':
				who, special = who|0o700, special|0o4000
			case 'g':
				who, special = who|0o070, special|0o2000
			case 'o':
				who, special = who|0o007, special|0o1000
			case 'a':
				who, special = who|0o777, special|0o7000
			}
		}
		if who == 0 {
			who, special = 0o777, 0o7000
		}
		if i == len(clause) {
			return 0, false // a who with nothing to do
		}
		for i < len(clause) {
			op := clause[i]
			if strings.IndexByte("+-=", op) < 0 {
				return 0, false
			}
			var bits uint32
			for i++; i < len(clause) && strings.IndexByte("+-=", clause[i]) < 0; i++ {
				switch clause[i] {
				case 'r':
					bits |= 0o444 & who
				case 'w':
					bits |= 0o222 & who
				case 'x':
					bits |= 0o111 & who
				case 'X': // execute only where it already makes sense: folders, programs
					if isDir || mode&0o111 != 0 {
						bits |= 0o111 & who
					}
				case 's':
					bits |= 0o6000 & special
				case 't':
					bits |= 0o1000 & special
				default:
					return 0, false
				}
			}
			switch op {
			case '+':
				mode |= bits
			case '-':
				mode &^= bits
			case '=':
				mode = mode&^(who|special&0o6000) | bits
			}
		}
	}
	return mode, true
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
