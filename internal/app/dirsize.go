package app

import (
	"context"
	"io/fs"
	"path/filepath"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

// A directory shows no size until one is asked for, because working it out
// means walking the tree. Space measures the directory under the cursor while
// it marks it, and "=" measures every directory in the pane. Both run off the
// UI thread and report back one directory at a time, so a big tree fills in
// while the pane stays usable.

// maxSizeWalks caps how many directories one "=" asks about: a pane with
// thousands of directories is not one anybody reads all the totals of.
const maxSizeWalks = 200

// sizeWalkers is how many trees are walked at once, across both panes. The
// rest wait their turn: two hundred walks at once fight over the same disk and
// finish no sooner.
var sizeWalkers = make(chan struct{}, 4)

// sizing is a pane's measuring in progress: the directory it is for, what
// stops it, and which entries are being measured, so pressing "=" again does
// not start them twice.
type sizing struct {
	dir      string
	ctx      context.Context
	cancel   context.CancelFunc
	inFlight map[string]bool
}

// beginSizing returns the context for measuring in pane i's directory, and
// the set of entries already being measured there. A pane that has moved since
// the last measuring gets a fresh start.
func (m *Model) beginSizing(i int) (context.Context, map[string]bool) {
	s := &m.sizes[i]
	if s.cancel == nil || s.dir != m.panes[i].Path {
		if s.cancel != nil {
			s.cancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		*s = sizing{dir: m.panes[i].Path, ctx: ctx, cancel: cancel, inFlight: map[string]bool{}}
	}
	return s.ctx, s.inFlight
}

// dropStaleSizing stops the measuring of a pane that has left the directory
// it was measuring in.
func (m *Model) dropStaleSizing() {
	for i := range m.sizes {
		s := &m.sizes[i]
		if s.cancel != nil && (m.panes[i].Path != s.dir || m.panes[i].IsRemote() || m.panes[i].InArchive()) {
			s.cancel()
			m.sizes[i] = sizing{}
		}
	}
}

// dirSizeMsg carries a measured total back to the pane that asked for it. The
// directory it was measured in travels with it, so a total that lands after the
// pane has moved on is discarded rather than shown against another name.
type dirSizeMsg struct {
	pane int
	dir  string // the pane's directory when the walk started
	name string // the entry that was measured
	size int64
}

// dirSizeCmd measures one directory off the UI thread, once a walker is free.
// A cancelled measuring reports nothing.
func dirSizeCmd(ctx context.Context, paneIdx int, dir, name string) tea.Cmd {
	return func() tea.Msg {
		select {
		case sizeWalkers <- struct{}{}:
			defer func() { <-sizeWalkers }()
		case <-ctx.Done():
			return nil
		}
		size, err := treeSize(ctx, filepath.Join(dir, name))
		if err != nil {
			return nil
		}
		return dirSizeMsg{pane: paneIdx, dir: dir, name: name, size: size}
	}
}

// treeSize adds up the regular files under root. Unreadable corners are skipped:
// a total that ignores what it cannot see beats no total at all. A cancelled
// walk stops where it is and returns ctx's error.
func treeSize(ctx context.Context, root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// sizeCurrentDir measures the highlighted directory, if that is what it is and
// it has not been measured already.
func (m *Model) sizeCurrentDir() tea.Cmd {
	p := &m.panes[m.active]
	if p.IsRemote() || p.InArchive() {
		return nil // measuring means walking this machine's filesystem
	}
	cur, ok := p.Current()
	if !ok || !cur.IsDir || cur.Name == ".." || p.DirSizeKnown(cur.Name) {
		return nil
	}
	ctx, inFlight := m.beginSizing(m.active)
	if inFlight[cur.Name] {
		return nil
	}
	inFlight[cur.Name] = true
	return dirSizeCmd(ctx, m.active, p.Path, cur.Name)
}

// sizeAllDirs measures every unmeasured directory in the active pane.
func (m *Model) sizeAllDirs() tea.Cmd {
	p := &m.panes[m.active]
	if p.IsRemote() || p.InArchive() {
		m.errText = "directory sizes are measured on this machine only"
		return nil
	}

	names := p.UnsizedDirs()
	if len(names) > maxSizeWalks {
		m.noticeText = "measuring the first " + strconv.Itoa(maxSizeWalks) + " directories"
		names = names[:maxSizeWalks]
	}

	ctx, inFlight := m.beginSizing(m.active)
	cmds := make([]tea.Cmd, 0, len(names))
	for _, n := range names {
		if inFlight[n] {
			continue
		}
		inFlight[n] = true
		cmds = append(cmds, dirSizeCmd(ctx, m.active, p.Path, n))
	}
	return tea.Batch(cmds...)
}

// onDirSize shows a measured total, unless the pane has moved since.
func (m Model) onDirSize(msg dirSizeMsg) (tea.Model, tea.Cmd) {
	p := &m.panes[msg.pane]
	if p.Path != msg.dir || p.IsRemote() || p.InArchive() {
		return m, nil
	}
	if s := m.sizes[msg.pane]; s.dir == msg.dir && s.inFlight != nil {
		delete(s.inFlight, msg.name)
	}
	p.SetDirSize(msg.name, msg.size)
	return m, nil
}
