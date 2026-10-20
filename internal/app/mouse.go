package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The mouse is not how a file manager is driven, but it is how a file manager
// is pointed at: the wheel moves through a list, a click puts the cursor
// somewhere without counting rows, and a double-click opens the thing under it.
// Everything it does has a key that does the same, so nothing depends on it.
//
// Clicks reach the panes only in normal mode. With a dialog open the pointer
// would be aiming at a box tyr draws over the top of them, and acting on what
// is underneath is not what anyone means by that.

// doubleClickWindow is how close together two clicks on the same entry have to
// be to count as opening it.
const doubleClickWindow = 500 * time.Millisecond

// wheelStep is how many rows one notch of the wheel moves.
const wheelStep = 3

// clickRecord is the last click, for spotting the second half of a double one.
type clickRecord struct {
	pane  int
	index int
	when  time.Time
}

// onMouse routes a mouse event to whatever is on screen: the modal's own
// handler, or else the wheel over the list it shows.
func (m Model) onMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	md := m.modal()
	if md.mouse != nil {
		return md.mouse(m, msg)
	}
	up := msg.Button == tea.MouseButtonWheelUp
	down := msg.Button == tea.MouseButtonWheelDown
	if md.wheel == nil || (!up && !down) {
		return m, nil
	}
	step := wheelStep
	if up {
		step = -wheelStep
	}
	md.wheel(&m, step)
	return m, nil
}

// viewerMouse is the pointer over the pager, which scrolls it.
func (m Model) viewerMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// editorMouse is the pointer over the editor: clicks place the cursor, drags
// select, the wheel scrolls.
func (m Model) editorMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	msg.Y-- // below the header line
	m.editor.Update(msg)
	return m, nil
}

// paneMouse is the pointer over the two panes.
func (m Model) paneMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	idx, x, y := m.paneAt(msg.X, msg.Y)
	if idx < 0 {
		return m, nil
	}
	p := &m.panes[idx]

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.active = idx
		for range wheelStep {
			p.MoveUp()
		}
		return m, nil

	case tea.MouseButtonWheelDown:
		m.active = idx
		for range wheelStep {
			p.MoveDown()
		}
		return m, nil
	}

	if msg.Action != tea.MouseActionPress {
		return m, nil
	}

	switch msg.Button {
	case tea.MouseButtonLeft:
		if p.OnAddressBar(x, y) {
			m.active = idx
			m.mode = modeAddress
			return m, p.BeginEditPath()
		}
		index, ok := p.EntryAt(x, y)
		if !ok {
			return m, nil
		}
		m.active = idx
		p.SetCursor(index)

		if m.isDoubleClick(idx, index) {
			m.lastClick = clickRecord{}
			if p.IsRemote() {
				return m, m.remoteEnter(idx)
			}
			m.enterOrOpen()
			return m, nil
		}
		m.lastClick = clickRecord{pane: idx, index: index, when: time.Now()}
		return m, nil

	case tea.MouseButtonRight:
		index, ok := p.EntryAt(x, y)
		if !ok {
			return m, nil
		}
		m.active = idx
		p.SetCursor(index)
		cmd := m.sizeCurrentDir()
		p.ToggleSelect()
		p.SetCursor(index) // marking with the mouse should not walk the cursor on
		return m, cmd
	}
	return m, nil
}

// isDoubleClick reports whether this click completes a double one.
func (m Model) isDoubleClick(pane, index int) bool {
	last := m.lastClick
	return last.pane == pane && last.index == index &&
		!last.when.IsZero() && time.Since(last.when) < doubleClickWindow
}

// paneAt says which pane a point is in, and where in that pane it is. It undoes
// the split resizePanes lays out.
func (m Model) paneAt(x, y int) (idx, px, py int) {
	if m.width == 0 || y >= m.height-1 { // the last line is the status bar
		return -1, 0, 0
	}
	leftW := m.width / 2
	if x < leftW {
		return 0, x, y
	}
	return 1, x - leftW, y
}

// clampIndex keeps a cursor inside a list of n rows.
func clampIndex(index, n int) int {
	if n == 0 {
		return 0
	}
	return min(max(index, 0), n-1)
}
