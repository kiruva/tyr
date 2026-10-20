package app

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
	ti := newInput("*.txt;notes", false, contentWidth-2)
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
	case "enter":
		p := &m.panes[m.selMask.paneIdx]
		on := m.selMask.on // closing zeroes the prompt, this with it
		n := p.SelectMatching(m.selMask.input.Value(), on)
		m.closeSelectMask()
		m.noticeText = fmt.Sprintf("%s %d %s", maskVerb(on), n, items(n))
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
	return prompt{
		title:  title,
		about:  "* ? [] globs · plain text matches anywhere · ; separates",
		field:  m.selMask.input,
		action: "apply",
	}.render()
}
