package app

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/ui"
)

// Marking a long list one Space at a time is the slow way. Ctrl+A takes
// everything on screen, * flips what is marked, and +/- open a one-field prompt
// that marks or unmarks by mask. All four act on what the pane is showing, so
// with a filter on they act only within it.

// selectMaskState is the +/- prompt while it is open.
type selectMaskState struct {
	input   textinput.Model
	on      bool // true = select the matches, false = unmark them
	paneIdx int
}

// openSelectMask opens the prompt in select (on) or deselect mode.
func (m *Model) openSelectMask(on bool) tea.Cmd {
	ti := newConnInput("*.txt;notes", false, contentWidth-2)
	ti.SetValue("*")
	ti.CursorEnd()

	m.selMask = selectMaskState{input: ti, on: on, paneIdx: m.active}
	m.mode = modeSelectMask
	return m.selMask.input.Focus()
}

// onSelectMaskKey drives the prompt.
func (m Model) onSelectMaskKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeSelectMask()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		p := &m.panes[m.selMask.paneIdx]
		n := p.SelectMatching(m.selMask.input.Value(), m.selMask.on)
		m.closeSelectMask()
		m.noticeText = fmt.Sprintf("%s %d %s", maskVerb(m.selMask.on), n, items(n))
		return m, nil
	}

	var cmd tea.Cmd
	m.selMask.input, cmd = m.selMask.input.Update(msg)
	return m, cmd
}

func (m *Model) closeSelectMask() {
	m.mode = modeNormal
	m.selMask.input.Blur()
	m.selMask = selectMaskState{}
}

func maskVerb(on bool) string {
	if on {
		return "selected"
	}
	return "deselected"
}

// selectAll marks everything visible in the active pane.
func (m *Model) selectAll() {
	n := m.panes[m.active].SelectAll()
	m.noticeText = fmt.Sprintf("selected %d %s", n, items(n))
}

// invertSelection flips every visible mark in the active pane.
func (m *Model) invertSelection() {
	n := m.panes[m.active].InvertSelection()
	m.noticeText = fmt.Sprintf("%d %s selected", n, items(n))
}

// renderSelectMask draws the prompt, sized like the other one-field modals.
func (m Model) renderSelectMask() string {
	title := "Select by mask"
	if !m.selMask.on {
		title = "Deselect by mask"
	}

	lines := []string{
		ui.DialogTitle.Render(title),
		"",
		ui.Faint.Render("* ? [] globs · plain text matches anywhere · ; separates"),
		ui.AddrEdit.Width(contentWidth).Render(m.selMask.input.View()),
		"",
		ui.DialogHint.Render("enter") + " apply    " + ui.DialogHint.Render("esc") + " cancel",
	}
	return ui.Dialog.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
