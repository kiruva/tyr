package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/compare"
	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/pane"
)

// Two panes side by side are already half of a directory comparison; this is
// the other half. F9 pairs up what is in both trees, proposes a direction for
// every difference — newer wins, present-on-one-side gets copied — and lets
// each row be argued with before anything runs.
//
// The proposal is the point. A sync that asked about every file would be no
// faster than copying by hand, and one that ran without showing its work would
// be no safer.

// syncAction is what will happen to one pair.
type syncAction int

const (
	syncSkip syncAction = iota
	syncToRight
	syncToLeft
)

func (a syncAction) arrow() string {
	switch a {
	case syncToRight:
		return "→"
	case syncToLeft:
		return "←"
	default:
		return "·"
	}
}

// syncStage is which part of the tool is on screen.
type syncStage int

const (
	syncStageRunning syncStage = iota // the comparison is walking both trees
	syncStageList                     // the pairs are listed
)

// syncRow is one compared path and what to do about it.
type syncRow struct {
	pair   compare.Pair
	action syncAction
}

// syncState is the tool while it is open.
type syncState struct {
	stage     syncStage
	leftRoot  string
	rightRoot string

	rows      []syncRow
	cursor    int
	truncated bool
	status    string

	showSame  bool
	hidden    bool
	byContent bool

	cancel context.CancelFunc
}

// syncDoneMsg carries a finished comparison back to the UI.
type syncDoneMsg struct {
	leftRoot, rightRoot string
	pairs               []compare.Pair
	truncated           bool
	err                 error
}

// openSync starts a comparison of the two panes.
func (m *Model) openSync() tea.Cmd {
	left, right := &m.panes[0], &m.panes[1]
	for _, p := range []*pane.Model{left, right} {
		if p.IsRemote() {
			m.errText = "compare is local-only — transfer the files across first"
			return nil
		}
		if p.InArchive() {
			m.errText = "not available inside an archive — unpack it first"
			return nil
		}
	}
	if left.Path == right.Path {
		m.errText = "both panes are in the same directory"
		return nil
	}

	m.sync = syncState{leftRoot: left.Path, rightRoot: right.Path, hidden: left.HiddenShown()}
	m.mode = modeSync
	return m.startCompare()
}

// startCompare kicks off the walk with the current switches.
func (m *Model) startCompare() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.sync.cancel = cancel
	m.sync.stage = syncStageRunning
	m.sync.status = ""

	opts := compare.Options{
		Recursive: true,
		Hidden:    m.sync.hidden,
		ByContent: m.sync.byContent,
	}
	left, right := m.sync.leftRoot, m.sync.rightRoot
	return func() tea.Msg {
		pairs, truncated, err := compare.Walk(ctx, left, right, opts)
		return syncDoneMsg{leftRoot: left, rightRoot: right, pairs: pairs, truncated: truncated, err: err}
	}
}

// onSyncDone installs a finished comparison, with a direction proposed for
// every row that needs one.
func (m Model) onSyncDone(msg syncDoneMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeSync || msg.leftRoot != m.sync.leftRoot || msg.rightRoot != m.sync.rightRoot {
		return m, nil
	}
	m.sync.cancel = nil

	if msg.err != nil && !errorIsCancelled(msg.err) {
		m.closeSync()
		m.errText = "compare failed: " + msg.err.Error()
		return m, nil
	}
	if errorIsCancelled(msg.err) {
		m.closeSync()
		return m, nil
	}

	rows := make([]syncRow, 0, len(msg.pairs))
	for _, p := range msg.pairs {
		rows = append(rows, syncRow{pair: p, action: proposedAction(p)})
	}

	m.sync.rows = rows
	m.sync.truncated = msg.truncated
	m.sync.cursor = 0
	m.sync.stage = syncStageList
	if len(rows) == 0 {
		m.sync.status = "the two directories already match"
	}
	return m, nil
}

// proposedAction is what the tool suggests for one pair: copy what is missing,
// let the newer side win where both have it, and leave equal files alone.
func proposedAction(p compare.Pair) syncAction {
	switch p.Kind {
	case compare.OnlyLeft:
		return syncToRight
	case compare.OnlyRight:
		return syncToLeft
	case compare.Differs:
		if p.Newer == compare.Right {
			return syncToLeft
		}
		return syncToRight
	default:
		return syncSkip
	}
}

// onSyncKey drives the tool.
func (m Model) onSyncKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.cancelCompare()
		return m, tea.Quit
	}
	if m.sync.stage == syncStageRunning {
		if msg.String() == "esc" {
			m.cancelCompare()
			m.closeSync()
		}
		return m, nil
	}

	visible := m.visibleSyncRows()
	last := len(visible) - 1

	switch msg.String() {
	case "esc", "q":
		m.closeSync()
		return m, nil
	case "up", "k":
		m.sync.cursor = max(m.sync.cursor-1, 0)
	case "down", "j":
		m.sync.cursor = min(m.sync.cursor+1, last)
	case "pgup", "ctrl+u":
		m.sync.cursor = max(m.sync.cursor-syncRowsPerPage, 0)
	case "pgdown", "ctrl+d":
		m.sync.cursor = min(m.sync.cursor+syncRowsPerPage, last)
	case "home", "g":
		m.sync.cursor = 0
	case "end", "G":
		m.sync.cursor = max(last, 0)
	case "right", "l":
		m.setSyncAction(syncToRight)
	case "left", "h":
		m.setSyncAction(syncToLeft)
	case "s", " ":
		m.setSyncAction(syncSkip)
	case "a":
		m.resetSyncActions()
	case "e":
		m.sync.showSame = !m.sync.showSame
		m.sync.cursor = 0
	case ".":
		m.sync.hidden = !m.sync.hidden
		return m, m.startCompare()
	case "c":
		m.sync.byContent = !m.sync.byContent
		return m, m.startCompare()
	case "enter":
		return m.confirmSync()
	}
	return m, nil
}

// setSyncAction changes the highlighted row and steps down, so a column of
// decisions can be made without moving the cursor by hand.
func (m *Model) setSyncAction(action syncAction) {
	visible := m.visibleSyncRows()
	if m.sync.cursor < 0 || m.sync.cursor >= len(visible) {
		return
	}
	m.sync.rows[visible[m.sync.cursor]].action = action
	m.sync.cursor = min(m.sync.cursor+1, len(visible)-1)
}

// resetSyncActions puts every row back to what the tool proposed.
func (m *Model) resetSyncActions() {
	for i := range m.sync.rows {
		m.sync.rows[i].action = proposedAction(m.sync.rows[i].pair)
	}
}

// visibleSyncRows indexes the rows currently listed, which excludes the equal
// ones unless they have been asked for.
func (m Model) visibleSyncRows() []int {
	out := make([]int, 0, len(m.sync.rows))
	for i, r := range m.sync.rows {
		if r.pair.Kind == compare.Same && !m.sync.showSame {
			continue
		}
		out = append(out, i)
	}
	return out
}

// syncPairs turns the decisions into the copies that carry them out.
func (m Model) syncPairs() []fileops.Pair {
	var pairs []fileops.Pair
	for _, r := range m.sync.rows {
		rel := filepath.FromSlash(r.pair.Rel)
		switch r.action {
		case syncToRight:
			pairs = append(pairs, fileops.Pair{
				Src: filepath.Join(m.sync.leftRoot, rel),
				Dst: filepath.Join(m.sync.rightRoot, rel),
			})
		case syncToLeft:
			pairs = append(pairs, fileops.Pair{
				Src: filepath.Join(m.sync.rightRoot, rel),
				Dst: filepath.Join(m.sync.leftRoot, rel),
			})
		}
	}
	return pairs
}

// confirmSync hands the decisions to the usual confirm prompt.
func (m Model) confirmSync() (tea.Model, tea.Cmd) {
	pairs := m.syncPairs()
	if len(pairs) == 0 {
		m.sync.status = "every row is set to skip — nothing to do"
		return m, nil
	}

	// A sync is a decision already made, row by row: the copies it runs must not
	// stop to ask about the very files the tool just listed as different.
	m.pending = fileops.Job{
		Op:         fileops.OpSync,
		Pairs:      pairs,
		Dest:       m.sync.rightRoot,
		OnConflict: fileops.ConflictOverwrite,
	}
	m.closeSync()
	m.mode = modeConfirm
	return m, nil
}

// syncCounts is how many copies each direction would run.
func (m Model) syncCounts() (toRight, toLeft int) {
	for _, r := range m.sync.rows {
		switch r.action {
		case syncToRight:
			toRight++
		case syncToLeft:
			toLeft++
		}
	}
	return toRight, toLeft
}

func (m *Model) cancelCompare() {
	if m.sync.cancel != nil {
		m.sync.cancel()
		m.sync.cancel = nil
	}
}

func (m *Model) closeSync() {
	m.cancelCompare()
	m.mode = modeNormal
	m.sync = syncState{}
}

// errorIsCancelled reports whether err is a cancelled context.
func errorIsCancelled(err error) bool { return err == context.Canceled }

// syncDirections counts a pending sync's copies by direction, which the confirm
// prompt states before anything is overwritten. The job's Dest is the right-hand
// root, so a destination under it is a copy rightwards.
func syncDirections(pairs []fileops.Pair, rightRoot string) (toRight, toLeft int) {
	for _, p := range pairs {
		if strings.HasPrefix(p.Dst, rightRoot+string(filepath.Separator)) {
			toRight++
			continue
		}
		toLeft++
	}
	return toRight, toLeft
}

// syncLabel describes the pending sync for the confirm prompt.
func syncLabel(job fileops.Job) string {
	return fmt.Sprintf("%d %s", len(job.Pairs), items(len(job.Pairs)))
}
