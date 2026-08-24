package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/find"
)

// Find searches the active pane's directory and everything under it, by name
// and optionally by what is inside the files. The form collects the query, the
// search runs off the UI thread with Esc cancelling it, and the hits land in a
// list: Enter on one takes the pane to it with the cursor already there.
//
// It is a local tool. A remote pane would need the far side to do the walking,
// and an archive is a listing tyr already holds in full.

// findStage is which part of the tool is on screen.
type findStage int

const (
	findStageForm    findStage = iota // typing the query
	findStageRunning                  // the walk is in flight
	findStageResults                  // the hits are listed
)

// findField is a row in the form.
type findField int

const (
	findFieldName findField = iota
	findFieldContent
	findFieldCase
	findFieldHidden
	findFieldCount
)

// findRows is how many hits the list shows at once.
const findRows = 12

// findState is the tool while it is open.
type findState struct {
	stage findStage
	focus findField

	name    textinput.Model
	content textinput.Model

	caseSense bool
	hidden    bool

	root    string // directory the search walks
	paneIdx int    // pane the results jump

	results   []find.Result
	cursor    int
	truncated bool
	status    string

	cancel context.CancelFunc
}

// findDoneMsg carries a finished search back to the UI.
type findDoneMsg struct {
	root      string
	results   []find.Result
	truncated bool
	err       error
}

// openFind starts the tool on the active pane's directory.
func (m *Model) openFind() tea.Cmd {
	p := &m.panes[m.active]
	if p.IsRemote() {
		m.errText = "find is local-only — it walks this machine's filesystem"
		return nil
	}
	if p.InArchive() {
		m.errText = "not available inside an archive — the pane already lists all of it"
		return nil
	}

	s := findState{
		root:    p.Path,
		paneIdx: m.active,
		name:    newConnInput("*.go, notes, …", false, contentWidth-findLabelW),
		content: newConnInput("text inside the file", false, contentWidth-findLabelW),
		hidden:  p.HiddenShown(),
	}
	// A filter typed a moment ago is usually what the search is about too.
	if f := p.Filter(); f != "" {
		s.name.SetValue(f)
		s.name.CursorEnd()
	}

	m.find = s
	m.mode = modeFind
	return m.find.name.Focus()
}

// onFindKey dispatches to whichever stage is on screen.
func (m Model) onFindKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.cancelFind()
		return m, tea.Quit
	}

	switch m.find.stage {
	case findStageRunning:
		if msg.String() == "esc" {
			m.cancelFind()
			m.find.stage = findStageForm
			m.find.status = "cancelled"
			return m, m.refocusFind()
		}
		return m, nil
	case findStageResults:
		return m.onFindResultsKey(msg)
	default:
		return m.onFindFormKey(msg)
	}
}

// onFindFormKey drives the query form.
func (m Model) onFindFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeFind()
		return m, nil
	case "enter":
		return m.runFind()
	case "tab", "down":
		return m.moveFindFocus(1)
	case "shift+tab", "up":
		return m.moveFindFocus(-1)
	case " ":
		// Space toggles whichever switch has focus; in a text field it is a space.
		switch m.find.focus {
		case findFieldCase:
			m.find.caseSense = !m.find.caseSense
			return m, nil
		case findFieldHidden:
			m.find.hidden = !m.find.hidden
			return m, nil
		}
	}

	m.find.status = ""
	var cmd tea.Cmd
	switch m.find.focus {
	case findFieldName:
		m.find.name, cmd = m.find.name.Update(msg)
	case findFieldContent:
		m.find.content, cmd = m.find.content.Update(msg)
	}
	return m, cmd
}

// moveFindFocus walks the form's rows.
func (m Model) moveFindFocus(step int) (tea.Model, tea.Cmd) {
	m.find.status = ""
	m.find.focus = findField((int(m.find.focus) + step + int(findFieldCount)) % int(findFieldCount))
	return m, m.refocusFind()
}

// refocusFind points the cursor at whichever text field has focus.
func (m *Model) refocusFind() tea.Cmd {
	m.find.name.Blur()
	m.find.content.Blur()
	switch m.find.focus {
	case findFieldName:
		return m.find.name.Focus()
	case findFieldContent:
		return m.find.content.Focus()
	}
	return nil
}

// runFind starts the walk, or explains why the query cannot run.
func (m Model) runFind() (tea.Model, tea.Cmd) {
	opts := find.Options{
		Root:          m.find.root,
		Name:          m.find.name.Value(),
		Content:       m.find.content.Value(),
		CaseSensitive: m.find.caseSense,
		Hidden:        m.find.hidden,
	}
	if opts.Name == "" && opts.Content == "" {
		m.find.status = find.ErrEmptyQuery.Error()
		return m, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.find.cancel = cancel
	m.find.stage = findStageRunning
	m.find.results = nil
	m.find.cursor = 0
	m.find.truncated = false
	m.find.status = ""
	m.find.name.Blur()
	m.find.content.Blur()

	return m, findCmd(ctx, opts)
}

// findCmd runs one search off the UI thread.
func findCmd(ctx context.Context, opts find.Options) tea.Cmd {
	return func() tea.Msg {
		results, truncated, err := find.Run(ctx, opts)
		return findDoneMsg{root: opts.Root, results: results, truncated: truncated, err: err}
	}
}

// onFindDone installs a finished search. A reply for a search that was
// cancelled, or for a tool that has since been closed, is dropped.
func (m Model) onFindDone(msg findDoneMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeFind || m.find.stage != findStageRunning || msg.root != m.find.root {
		return m, nil
	}
	m.find.cancel = nil

	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		m.find.stage = findStageForm
		m.find.status = msg.err.Error()
		return m, m.refocusFind()
	}
	if len(msg.results) == 0 {
		m.find.stage = findStageForm
		m.find.status = "nothing matched"
		return m, m.refocusFind()
	}

	m.find.results = msg.results
	m.find.truncated = msg.truncated
	m.find.cursor = 0
	m.find.stage = findStageResults
	return m, nil
}

// onFindResultsKey drives the hit list.
func (m Model) onFindResultsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	last := len(m.find.results) - 1

	switch msg.String() {
	case "esc", "q":
		m.closeFind()
		return m, nil
	case "up", "k":
		m.find.cursor = max(m.find.cursor-1, 0)
	case "down", "j":
		m.find.cursor = min(m.find.cursor+1, last)
	case "pgup", "ctrl+u":
		m.find.cursor = max(m.find.cursor-findRows, 0)
	case "pgdown", "ctrl+d":
		m.find.cursor = min(m.find.cursor+findRows, last)
	case "home", "g":
		m.find.cursor = 0
	case "end", "G":
		m.find.cursor = last
	case "/", "ctrl+f":
		// Back to the form with the query intact, to narrow it.
		m.find.stage = findStageForm
		return m, m.refocusFind()
	case "enter":
		return m.jumpToResult()
	}
	return m, nil
}

// jumpToResult takes the pane to the highlighted hit and closes the tool.
func (m Model) jumpToResult() (tea.Model, tea.Cmd) {
	if m.find.cursor < 0 || m.find.cursor >= len(m.find.results) {
		return m, nil
	}
	res := m.find.results[m.find.cursor]

	dir, name := filepath.Dir(res.Path), filepath.Base(res.Path)
	if res.IsDir {
		dir, name = res.Path, ""
	}

	idx := m.find.paneIdx
	p := &m.panes[idx]
	if err := p.GoTo(dir); err != nil {
		m.find.status = err.Error()
		m.find.stage = findStageForm
		return m, m.refocusFind()
	}
	if name != "" {
		p.Focus(name)
	}

	m.closeFind()
	m.active = idx
	return m, nil
}

// cancelFind stops a running walk, if there is one.
func (m *Model) cancelFind() {
	if m.find.cancel != nil {
		m.find.cancel()
		m.find.cancel = nil
	}
}

func (m *Model) closeFind() {
	m.cancelFind()
	m.mode = modeNormal
	m.find.name.Blur()
	m.find.content.Blur()
	m.find = findState{}
}

// findSummary is the line above the hit list.
func (m Model) findSummary() string {
	n := len(m.find.results)
	s := fmt.Sprintf("%d %s", n, items(n))
	if m.find.truncated {
		s = fmt.Sprintf("first %d %s — narrow the query for the rest", n, items(n))
	}
	return s
}
