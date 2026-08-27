package app

import (
	"path/filepath"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/config"
	"github.com/kiruva/tyr/internal/ui"
)

// A bookmark is a directory you keep coming back to. B names the one the pane
// is in, b lists them, and Enter takes the active pane there. They live in the
// same config file as everything else, one line each, so the list can also be
// written by hand.

// bookmarkNameCol is the width of the name column in the list.
const bookmarkNameCol = 14

// bookmarkState is the list while it is open.
type bookmarkState struct {
	marks    []config.Bookmark
	cursor   int
	deleting bool // the highlighted row is one y away from being removed
	status   string
	paneIdx  int
}

// bookmarkAddState is the naming prompt.
type bookmarkAddState struct {
	input  textinput.Model
	path   string
	status string
}

// openBookmarks lists what is saved.
func (m *Model) openBookmarks() tea.Cmd {
	marks, err := config.Bookmarks()
	m.bookmarks = bookmarkState{marks: marks, paneIdx: m.active}
	if err != nil {
		m.bookmarks.status = "could not read the bookmarks: " + err.Error()
	}
	if len(marks) == 0 {
		m.bookmarks.status = "no bookmarks yet — B saves the directory the pane is in"
	}
	m.mode = modeBookmarks
	return nil
}

// onBookmarksKey drives the list.
func (m Model) onBookmarksKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.bookmarks.deleting {
		return m.onBookmarkDeleteKey(msg)
	}
	last := len(m.bookmarks.marks) - 1

	switch msg.String() {
	case "esc", "q", "b":
		m.mode = modeNormal
		m.bookmarks = bookmarkState{}
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.bookmarks.cursor = max(m.bookmarks.cursor-1, 0)
	case "down", "j":
		m.bookmarks.cursor = min(m.bookmarks.cursor+1, last)
	case "home", "g":
		m.bookmarks.cursor = 0
	case "end", "G":
		m.bookmarks.cursor = max(last, 0)
	case "d":
		if len(m.bookmarks.marks) > 0 {
			m.bookmarks.deleting = true
		}
	case "enter":
		return m.gotoBookmark()
	}
	return m, nil
}

// onBookmarkDeleteKey answers the "delete this one?" question.
func (m Model) onBookmarkDeleteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		mark := m.bookmarks.marks[m.bookmarks.cursor]
		if err := config.DeleteBookmark(mark.Name); err != nil {
			m.bookmarks.status = err.Error()
			m.bookmarks.deleting = false
			return m, nil
		}
		marks, _ := config.Bookmarks()
		m.bookmarks.marks = marks
		m.bookmarks.cursor = min(m.bookmarks.cursor, max(len(marks)-1, 0))
		m.bookmarks.deleting = false
		m.bookmarks.status = "deleted " + mark.Name
	default:
		m.bookmarks.deleting = false
	}
	return m, nil
}

// gotoBookmark takes the pane the list was opened from to the saved directory.
func (m Model) gotoBookmark() (tea.Model, tea.Cmd) {
	if m.bookmarks.cursor < 0 || m.bookmarks.cursor >= len(m.bookmarks.marks) {
		return m, nil
	}
	mark := m.bookmarks.marks[m.bookmarks.cursor]

	idx := m.bookmarks.paneIdx
	p := &m.panes[idx]
	if p.IsRemote() {
		p.LeaveRemote(mark.Path)
		m.mode = modeNormal
		m.bookmarks = bookmarkState{}
		m.active = idx
		return m, nil
	}
	if err := p.GoTo(mark.Path); err != nil {
		m.bookmarks.status = err.Error()
		return m, nil
	}

	m.mode = modeNormal
	m.bookmarks = bookmarkState{}
	m.active = idx
	return m, nil
}

// openBookmarkAdd asks what to call the directory the pane is in.
func (m *Model) openBookmarkAdd() tea.Cmd {
	p := &m.panes[m.active]
	if p.IsRemote() {
		m.errText = "bookmarks are local directories — this pane is on a host"
		return nil
	}

	dir := p.Path
	if p.InArchive() {
		dir = filepath.Dir(p.ArchivePath())
	}

	ti := newConnInput("name", false, contentWidth-2)
	ti.SetValue(filepath.Base(dir))
	ti.CursorEnd()

	m.bookmarkAdd = bookmarkAddState{input: ti, path: dir}
	m.mode = modeBookmarkAdd
	return m.bookmarkAdd.input.Focus()
}

// onBookmarkAddKey drives the naming prompt.
func (m Model) onBookmarkAddKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeBookmarkAdd()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		mark := config.Bookmark{Name: m.bookmarkAdd.input.Value(), Path: m.bookmarkAdd.path}
		if err := config.SaveBookmark(mark); err != nil {
			m.bookmarkAdd.status = err.Error()
			return m, nil
		}
		m.closeBookmarkAdd()
		m.noticeText = "bookmarked as " + mark.Name + " · b opens the list"
		return m, nil
	}

	m.bookmarkAdd.status = ""
	var cmd tea.Cmd
	m.bookmarkAdd.input, cmd = m.bookmarkAdd.input.Update(msg)
	return m, cmd
}

func (m *Model) closeBookmarkAdd() {
	m.mode = modeNormal
	m.bookmarkAdd.input.Blur()
	m.bookmarkAdd = bookmarkAddState{}
}

// renderBookmarks draws the list.
func (m Model) renderBookmarks() string {
	lines := []string{ui.DialogTitle.Render("Bookmarks"), ""}

	pathW := contentWidth - 2 - bookmarkNameCol - 1
	for i, mark := range m.bookmarks.marks {
		name := ui.PadRight(ui.TruncTail(mark.Name, bookmarkNameCol), bookmarkNameCol)
		path := ui.PadRight(ui.TruncTail(mark.Path, pathW), pathW)

		if i != m.bookmarks.cursor {
			lines = append(lines, "  "+name+" "+ui.Faint.Render(path))
			continue
		}
		marker := "▸ "
		if m.bookmarks.deleting {
			marker = ui.Danger.Render("✗ ")
		}
		lines = append(lines, marker+ui.Cursor.Render(name+" "+path))
	}

	if m.bookmarks.status != "" {
		lines = append(lines, "", ui.Faint.Render(ui.TruncTail(m.bookmarks.status, contentWidth)))
	}

	footer := ui.Faint.Render("enter go · d delete · esc close")
	if m.bookmarks.deleting {
		name := m.bookmarks.marks[m.bookmarks.cursor].Name
		footer = ui.Danger.Render("delete "+name+"?") + ui.Faint.Render("  y / n")
	}
	lines = append(lines, "", footer)

	return ui.Dialog.Width(dialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// renderBookmarkAdd draws the naming prompt.
func (m Model) renderBookmarkAdd() string {
	lines := []string{
		ui.DialogTitle.Render("Bookmark this directory"),
		"",
		ui.Faint.Render(ui.TruncTail(m.bookmarkAdd.path, contentWidth)),
		ui.AddrEdit.Width(contentWidth).Render(m.bookmarkAdd.input.View()),
	}
	if m.bookmarkAdd.status != "" {
		lines = append(lines, "", ui.Danger.Render(ui.TruncTail(m.bookmarkAdd.status, contentWidth)))
	}
	lines = append(lines, "",
		ui.DialogHint.Render("enter")+" save    "+ui.DialogHint.Render("esc")+" cancel")

	return ui.Dialog.Width(dialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
