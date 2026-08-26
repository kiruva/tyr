package app

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/pane"
	"github.com/kiruva/tyr/internal/ui"
)

// The filter narrows the active pane as you type: the listing behind the status
// bar shrinks on every keystroke, so the pattern is judged by what it leaves
// rather than by what it says. Enter keeps the narrowed pane and hands the keys
// back to navigation; Esc puts back whatever filter was there before, which for
// a pane that had none means showing everything again.

// filterWidth is how much room the pattern gets in the status bar.
const filterWidth = 28

// filterState is the prompt while it is open.
type filterState struct {
	input   textinput.Model
	prev    string // filter to restore on Esc
	paneIdx int
}

// openFilter starts editing the active pane's filter.
func (m *Model) openFilter() tea.Cmd {
	p := &m.panes[m.active]

	ti := newConnInput("name or *.glob", false, filterWidth)
	ti.SetValue(p.Filter())
	ti.CursorEnd()

	m.filter = filterState{input: ti, prev: p.Filter(), paneIdx: m.active}
	m.mode = modeFilter
	return m.filter.input.Focus()
}

// onFilterKey drives the prompt, applying every edit to the pane immediately.
func (m Model) onFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.panes[m.filter.paneIdx]

	switch msg.String() {
	case "enter":
		m.closeFilter()
		return m, nil
	case "esc":
		p.SetFilter(m.filter.prev)
		m.closeFilter()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "up", "down", "pgup", "pgdown":
		// Move through what is left without leaving the prompt.
		m.moveInPane(p, msg.String())
		return m, nil
	}

	var cmd tea.Cmd
	m.filter.input, cmd = m.filter.input.Update(msg)
	p.SetFilter(m.filter.input.Value())
	return m, cmd
}

// moveInPane applies a cursor key to a pane while another component has focus.
func (m *Model) moveInPane(p *pane.Model, key string) {
	switch key {
	case "up":
		p.MoveUp()
	case "down":
		p.MoveDown()
	case "pgup":
		p.PageUp()
	case "pgdown":
		p.PageDown()
	}
}

func (m *Model) closeFilter() {
	m.mode = modeNormal
	m.filter.input.Blur()
	m.filter = filterState{}
}

// renderFilterBar draws the prompt in place of the status bar.
func (m Model) renderFilterBar() string {
	left := " filter: " + m.filter.input.View()
	right := "enter keep · esc clear"
	return ui.StatusBar.Width(m.width).Render(left + spacer(m.width, left, right) + right)
}
