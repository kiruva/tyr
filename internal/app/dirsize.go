package app

import (
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

// maxSizeWalks caps how many directories one "=" starts at once: a pane with
// hundreds of directories would otherwise spawn a walk for each.
const maxSizeWalks = 200

// dirSizeMsg carries a measured total back to the pane that asked for it. The
// directory it was measured in travels with it, so a total that lands after the
// pane has moved on is discarded rather than shown against another name.
type dirSizeMsg struct {
	pane int
	dir  string // the pane's directory when the walk started
	name string // the entry that was measured
	size int64
}

// dirSizeCmd measures one directory off the UI thread.
func dirSizeCmd(paneIdx int, dir, name string) tea.Cmd {
	return func() tea.Msg {
		return dirSizeMsg{pane: paneIdx, dir: dir, name: name, size: treeSize(filepath.Join(dir, name))}
	}
}

// treeSize adds up the regular files under root. Unreadable corners are skipped:
// a total that ignores what it cannot see beats no total at all.
func treeSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
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
	return total
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
	return dirSizeCmd(m.active, p.Path, cur.Name)
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

	cmds := make([]tea.Cmd, 0, len(names))
	for _, n := range names {
		cmds = append(cmds, dirSizeCmd(m.active, p.Path, n))
	}
	return tea.Batch(cmds...)
}

// onDirSize shows a measured total, unless the pane has moved since.
func (m Model) onDirSize(msg dirSizeMsg) (tea.Model, tea.Cmd) {
	p := &m.panes[msg.pane]
	if p.Path != msg.dir || p.IsRemote() || p.InArchive() {
		return m, nil
	}
	p.SetDirSize(msg.name, msg.size)
	return m, nil
}
