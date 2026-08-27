package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/rename"
)

// Two rename flows share the engine in internal/rename. F2 renames the entry
// under the cursor through a one-field modal, because that is the common case
// and a preview of a single name is just the name again. M opens the batch tool:
// a full-screen form over a live preview of every candidate, which is the only
// honest way to show what a pattern is about to do to a hundred files.

// renameField indexes the batch tool's inputs, in tab order.
type renameField int

const (
	renFind renameField = iota
	renReplace
	renNameMask
	renExtMask
	renCounter
	renStep
	renFieldCount
)

var renamePlaceholders = [renFieldCount]string{
	renFind:     "pattern (empty = every name)",
	renReplace:  "replacement",
	renNameMask: "[N]",
	renExtMask:  "[E]",
	renCounter:  "1",
	renStep:     "1",
}

// renameState is the batch tool while it is open.
type renameState struct {
	open    bool
	paneIdx int
	root    string // the directory the candidates are relative to

	fields [renFieldCount]textinput.Model
	focus  renameField

	mode   rename.Mode
	ignore bool
	kase   rename.CaseOp
	trim   bool
	opt    rename.Options

	loading   bool
	cands     []rename.Candidate
	truncated bool

	changes []rename.Change
	summary rename.Summary
	scroll  int
	status  string // parse error or notice, shown under the form
}

// renameOneState is the single-entry prompt.
type renameOneState struct {
	input   textinput.Model
	paneIdx int
	orig    string
	isDir   bool
	status  string
}

// renameCollectedMsg carries the candidate sweep back from off the UI thread.
type renameCollectedMsg struct {
	pane      int
	root      string
	cands     []rename.Candidate
	truncated bool
	err       error
}

// Single rename ---------------------------------------------------------------

// openRenameOne opens the one-field prompt on the highlighted entry.
func (m *Model) openRenameOne() tea.Cmd {
	p := &m.panes[m.active]
	if err := m.renameAvailable(); err != nil {
		m.errText = err.Error()
		return nil
	}
	cur, ok := p.Current()
	if !ok || cur.Name == ".." {
		m.errText = "select an entry to rename"
		return nil
	}

	ti := newConnInput("new name", false, contentWidth-2)
	ti.SetValue(cur.Name)
	ti.CursorEnd()
	m.renOne = renameOneState{input: ti, paneIdx: m.active, orig: cur.Name, isDir: cur.IsDir}
	m.mode = modeRenameOne
	return m.renOne.input.Focus()
}

// onRenameOneKey drives the prompt; a rejected name keeps it open with why.
func (m Model) onRenameOneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeRenameOne()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		return m.commitRenameOne()
	}
	m.renOne.status = ""
	var cmd tea.Cmd
	m.renOne.input, cmd = m.renOne.input.Update(msg)
	return m, cmd
}

// commitRenameOne renames the entry. One rename is a single syscall, so it runs
// inline rather than through the progress machinery.
func (m Model) commitRenameOne() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.renOne.input.Value())
	if name == m.renOne.orig {
		m.closeRenameOne()
		return m, nil
	}
	if err := rename.ValidateName(name); err != nil {
		m.renOne.status = err.Error()
		return m, nil
	}

	p := &m.panes[m.renOne.paneIdx]
	change := rename.Change{Rel: m.renOne.orig, New: name, IsDir: m.renOne.isDir, Changed: true}
	// Inline, so there is no progress modal to cancel from and no context to
	// carry: a single rename is one syscall.
	applied, err := rename.Apply(context.Background(), p.Path, []rename.Change{change}, func(string) {})
	if err != nil {
		m.renOne.status = err.Error()
		return m, nil
	}

	root := p.Path
	m.closeRenameOne()
	m.pushUndoRename(root, applied)
	p.Refresh()
	p.Focus(name)
	m.noticeText = "renamed · ctrl+z to undo"
	return m, nil
}

func (m *Model) closeRenameOne() {
	m.mode = modeNormal
	m.renOne.input.Blur()
	m.renOne = renameOneState{}
}

// Batch rename ----------------------------------------------------------------

// openRename opens the batch tool on the active pane's selection.
func (m *Model) openRename() tea.Cmd {
	if err := m.renameAvailable(); err != nil {
		m.errText = err.Error()
		return nil
	}
	p := &m.panes[m.active]
	names := p.SelectedNames()
	if len(names) == 0 {
		m.errText = "select what to rename"
		return nil
	}

	st := renameState{
		open:    true,
		paneIdx: m.active,
		root:    p.Path,
		mode:    rename.ModeRegex,
		opt:     rename.Options{Recursive: true, Hidden: p.HiddenShown()},
		loading: true,
	}
	for f := range st.fields {
		st.fields[f] = newConnInput(renamePlaceholders[f], false, renameFieldWidth(renameField(f)))
	}
	st.fields[renNameMask].SetValue("[N]")
	st.fields[renExtMask].SetValue("[E]")
	st.fields[renCounter].SetValue("1")
	st.fields[renStep].SetValue("1")

	m.ren = st
	m.mode = modeRename
	return tea.Batch(
		m.ren.fields[renFind].Focus(),
		collectRenameCmd(m.active, p.Path, names, st.opt),
	)
}

// renameAvailable reports why rename cannot run here, if it cannot. Renaming is
// local: an archive member would mean a repack, and a remote name would mean a
// round trip per keystroke to keep the preview honest.
func (m *Model) renameAvailable() error {
	p := &m.panes[m.active]
	switch {
	case p.IsRemote():
		return fmt.Errorf("rename is local-only — copy the files across first")
	case p.InArchive():
		return fmt.Errorf("not available inside an archive — unpack it first")
	}
	return nil
}

// collectRenameCmd sweeps the selection off the UI thread: a recursive walk of a
// deep tree is not something to do between keystrokes.
func collectRenameCmd(paneIdx int, root string, names []string, opt rename.Options) tea.Cmd {
	return func() tea.Msg {
		cands, truncated, err := rename.Collect(root, names, opt)
		return renameCollectedMsg{pane: paneIdx, root: root, cands: cands, truncated: truncated, err: err}
	}
}

// onRenameCollected stores a finished sweep and plans against it.
func (m Model) onRenameCollected(msg renameCollectedMsg) (tea.Model, tea.Cmd) {
	if !m.ren.open || msg.root != m.ren.root {
		return m, nil // the tool was closed, or moved on, while the walk ran
	}
	m.ren.loading = false
	if msg.err != nil {
		m.ren.status = msg.err.Error()
		m.ren.cands, m.ren.changes = nil, nil
		return m, nil
	}
	m.ren.cands, m.ren.truncated = msg.cands, msg.truncated
	m.ren.scroll = 0
	m.replan()
	return m, nil
}

// onRenameKey drives the batch tool.
func (m Model) onRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeRename()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		return m.commitRename()
	case "tab":
		cmd := m.focusRenameField(m.ren.focus.next())
		m.replan()
		return m, cmd
	case "shift+tab":
		cmd := m.focusRenameField(m.ren.focus.prev())
		m.replan()
		return m, cmd
	case "up":
		m.scrollRename(-1)
		return m, nil
	case "down":
		m.scrollRename(1)
		return m, nil
	case "pgup", "ctrl+u":
		m.scrollRename(-m.renameRows())
		return m, nil
	case "pgdown":
		m.scrollRename(m.renameRows())
		return m, nil
	case "ctrl+o":
		m.ren.mode = m.ren.mode.Next()
		m.replan()
		return m, nil
	case "alt+i":
		m.ren.ignore = !m.ren.ignore
		m.replan()
		return m, nil
	case "ctrl+t":
		m.ren.kase = m.ren.kase.Next()
		m.replan()
		return m, nil
	case "ctrl+p":
		m.ren.trim = !m.ren.trim
		m.replan()
		return m, nil
	case "ctrl+r":
		m.ren.opt.Recursive = !m.ren.opt.Recursive
		return m, m.recollect()
	case "ctrl+y":
		m.ren.opt.Dirs = !m.ren.opt.Dirs
		return m, m.recollect()
	}

	var cmd tea.Cmd
	m.ren.fields[m.ren.focus], cmd = m.ren.fields[m.ren.focus].Update(msg)
	m.replan()
	return m, cmd
}

// recollect re-sweeps after a scope toggle, keeping the form as it is.
func (m *Model) recollect() tea.Cmd {
	p := &m.panes[m.ren.paneIdx]
	names := p.SelectedNames()
	if len(names) == 0 {
		m.ren.status = "the selection is gone"
		return nil
	}
	m.ren.loading = true
	return collectRenameCmd(m.ren.paneIdx, m.ren.root, names, m.ren.opt)
}

// commitRename hands the plan to the confirm prompt, which starts it as a job so
// a big batch reports progress like every other operation.
func (m Model) commitRename() (tea.Model, tea.Cmd) {
	if m.ren.loading {
		return m, nil
	}
	if m.ren.status != "" && m.ren.summary.Renamed == 0 {
		return m, nil // an unusable pattern; the status already says why
	}
	if m.ren.summary.Renamed == 0 {
		m.ren.status = "nothing to rename yet"
		return m, nil
	}

	m.pending = fileops.Job{
		Op:      fileops.OpRename,
		Dest:    m.ren.root,
		Renames: m.ren.changes,
	}
	m.willOverwrite = false // Apply never overwrites; conflicts are held back
	m.mode = modeConfirm
	return m, nil
}

func (m *Model) closeRename() {
	m.mode = modeNormal
	m.ren.fields[m.ren.focus].Blur()
	m.ren = renameState{}
}

// focusRenameField moves the cursor between inputs.
func (m *Model) focusRenameField(f renameField) tea.Cmd {
	m.ren.fields[m.ren.focus].Blur()
	m.ren.focus = f
	return m.ren.fields[f].Focus()
}

func (f renameField) next() renameField { return (f + 1) % renFieldCount }
func (f renameField) prev() renameField { return (f + renFieldCount - 1) % renFieldCount }

// scrollRename moves the preview window, clamped to the plan.
func (m *Model) scrollRename(delta int) {
	rows := m.renameRows()
	m.ren.scroll = min(max(m.ren.scroll+delta, 0), max(len(m.ren.changes)-rows, 0))
}

// replan re-runs the planner against the current form. It is called on every
// keystroke: planning is pure and does one stat per changed name, which is cheap
// enough to keep the preview live.
func (m *Model) replan() {
	spec, err := m.ren.spec()
	if err != nil {
		m.ren.status = err.Error()
		m.ren.changes, m.ren.summary = nil, rename.Summary{}
		return
	}

	changes, err := rename.Plan(m.ren.root, m.ren.cands, spec)
	if err != nil {
		m.ren.status = err.Error()
		m.ren.changes, m.ren.summary = nil, rename.Summary{}
		return
	}

	m.ren.status = ""
	m.ren.changes = changes
	m.ren.summary = rename.Summarize(changes)
	m.scrollRename(0) // the plan may have shrunk under the window
}

// spec reads the form into a Spec, reporting a field that is not a number.
func (s renameState) spec() (rename.Spec, error) {
	counter, err := numField(s.fields[renCounter].Value(), 1)
	if err != nil {
		return rename.Spec{}, fmt.Errorf("counter: %w", err)
	}
	step, err := numField(s.fields[renStep].Value(), 1)
	if err != nil {
		return rename.Spec{}, fmt.Errorf("step: %w", err)
	}

	return rename.Spec{
		Find:    s.fields[renFind].Value(),
		Replace: s.fields[renReplace].Value(),
		Mode:    s.mode,
		Ignore:  s.ignore,
		Name:    s.fields[renNameMask].Value(),
		Ext:     s.fields[renExtMask].Value(),
		Counter: counter,
		Step:    step,
		Case:    s.kase,
		Trim:    s.trim,
	}, nil
}

// numField parses a counter field, treating empty as the default.
func numField(in string, def int) (int, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return def, nil
	}
	n, err := strconv.Atoi(in)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", in)
	}
	return n, nil
}

// renameDisplay is the "old → new" pair a preview row draws, with the directory
// prefix kept so a recursive batch says where each entry lives.
func renameDisplay(c rename.Change) (from, to string) {
	from = filepath.ToSlash(c.Rel)
	if !c.Changed {
		return from, ""
	}
	return from, filepath.ToSlash(c.NewRel())
}
