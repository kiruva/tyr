package pane

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/ui"
)

// The address bar is the pane's top line. It shows the current location while
// navigating and turns into a text input when the user edits it.

// EditingPath reports whether the address bar currently has focus.
func (m *Model) EditingPath() bool { return m.editingAddr }

// BeginEditPath focuses the address bar, seeded with the current location.
func (m *Model) BeginEditPath() tea.Cmd {
	m.editingAddr = true
	if m.IsRemote() {
		m.addr.SetValue(m.host.Display(m.Path))
	} else {
		m.addr.SetValue(m.baseDir())
	}
	m.addr.CursorEnd()
	m.suggest()
	return m.addr.Focus()
}

// CancelEditPath abandons the edit and restores the displayed location.
func (m *Model) CancelEditPath() {
	m.stopEditPath()
}

// FinishEditPath closes the edit after the caller has already moved the pane.
func (m *Model) FinishEditPath() { m.stopEditPath() }

// AddrValue is the text currently typed in the address bar.
func (m *Model) AddrValue() string { return m.addr.Value() }

// SetAddrValue replaces the text in the address bar.
func (m *Model) SetAddrValue(s string) {
	m.addr.SetValue(s)
	m.addr.CursorEnd()
}

// CommitEditPath jumps to the typed location. A directory is entered directly;
// a browsable archive is opened as a virtual tree. The edit stays open on
// failure so the user can correct the path.
func (m *Model) CommitEditPath() error {
	target, err := m.resolve(m.addr.Value())
	if err != nil {
		return err
	}
	fi, err := os.Stat(target)
	if os.IsNotExist(err) {
		return fmt.Errorf("no such directory: %s", target)
	}
	if err != nil {
		return err
	}

	switch {
	case fi.IsDir():
		m.archive, m.vpath, m.members = "", "", nil
		m.Path = target
		m.stopEditPath()
		m.enterDir()
		return nil
	case fileops.Browsable(filepath.Base(target)):
		if err := m.EnterArchive(target); err != nil {
			return err
		}
		m.stopEditPath()
		return nil
	default:
		return fmt.Errorf("not a directory: %s", target)
	}
}

// UpdateAddr forwards a message to the address input (typing, cursor blink).
func (m *Model) UpdateAddr(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.addr, cmd = m.addr.Update(msg)
	m.suggest()
	return cmd
}

func (m *Model) stopEditPath() {
	m.editingAddr = false
	m.addr.Blur()
	m.syncAddr()
}

// syncAddr keeps the bar showing the live location while not being edited.
func (m *Model) syncAddr() {
	if !m.editingAddr {
		m.addr.SetValue(m.displayPath())
		m.addr.CursorStart()
	}
}

// baseDir is the real directory the pane sits in — inside an archive that's the
// directory holding the archive file.
func (m *Model) baseDir() string {
	if m.archive != "" {
		return filepath.Dir(m.archive)
	}
	return m.Path
}

// resolve expands ~, environment variables and relative input into a full path.
func (m *Model) resolve(in string) (string, error) {
	p := strings.TrimSpace(os.ExpandEnv(strings.TrimSpace(in)))
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if p == "~" || strings.HasPrefix(p, "~"+string(os.PathSeparator)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(m.baseDir(), p)
	}
	return filepath.Clean(p), nil
}

// suggest offers tab-completion from the directories under whatever the user
// has typed so far.
func (m *Model) suggest() {
	if m.IsRemote() {
		// Completing against the local filesystem would be actively wrong here,
		// and completing remotely would mean a round trip per keystroke.
		m.addr.SetSuggestions(nil)
		return
	}

	value := m.addr.Value()
	dir, prefix := filepath.Split(value)
	if dir == "" {
		dir = "." + string(os.PathSeparator)
	}
	full, err := m.resolve(dir)
	if err != nil {
		m.addr.SetSuggestions(nil)
		return
	}

	entries, err := os.ReadDir(full)
	if err != nil {
		m.addr.SetSuggestions(nil)
		return
	}

	var out []string
	for _, e := range entries {
		if !e.IsDir() && !fileops.Browsable(e.Name()) {
			continue
		}
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") && !m.showHidden && prefix == "" {
			continue
		}
		s := dir + e.Name()
		if e.IsDir() {
			s += string(os.PathSeparator)
		}
		out = append(out, s)
	}
	sort.Strings(out)
	m.addr.SetSuggestions(out)
}

// addrView renders the top line: the location, or the input while editing.
func (m Model) addrView(width int, active bool) string {
	if m.editingAddr {
		return ui.AddrEdit.Width(width).Render(m.addr.View())
	}
	style := ui.Faint
	if active {
		style = ui.Title
	}
	return style.Render(truncTail(m.displayPath(), width))
}

// truncTail keeps the tail of a path, prefixing "…" when it doesn't fit — the
// end of a path identifies the location better than its start.
func truncTail(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return "…" + string(r[len(r)-(w-1):])
}
