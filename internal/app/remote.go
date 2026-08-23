package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/remote"
)

// remoteListMsg carries the result of a directory listing back to the UI. It
// names the pane and the directory that was asked for so a reply that arrives
// after the user has moved on can be discarded.
type remoteListMsg struct {
	pane     int
	host     remote.Host
	path     string // what was requested
	previous string // where the pane was, to restore on failure
	listing  remote.Listing
	err      error
}

// listRemote runs a listing off the UI thread.
func listRemote(paneIdx int, h remote.Host, path, previous string) tea.Cmd {
	return func() tea.Msg {
		l, err := remote.List(h, path)
		return remoteListMsg{
			pane:     paneIdx,
			host:     h,
			path:     path,
			previous: previous,
			listing:  l,
			err:      err,
		}
	}
}

// onRemoteList applies a listing, or reports why it failed.
func (m Model) onRemoteList(msg remoteListMsg) (tea.Model, tea.Cmd) {
	p := &m.panes[msg.pane]
	if p.Host() != msg.host {
		return m, nil // the pane has left this host
	}
	if msg.err != nil {
		if p.RemoteFailed(msg.path, msg.previous) {
			m.errText = msg.err.Error()
		}
		return m, nil
	}
	p.SetRemoteListing(msg.path, msg.listing)
	return m, nil
}

// openRemote points a pane at a host and starts the first listing.
func (m *Model) openRemote(paneIdx int, h remote.Host, path string) tea.Cmd {
	p := &m.panes[paneIdx]
	p.OpenRemote(h, path)
	return listRemote(paneIdx, h, path, "")
}

// remoteNavigate walks the active remote pane to path.
func (m *Model) remoteNavigate(paneIdx int, path string) tea.Cmd {
	p := &m.panes[paneIdx]
	previous := p.Path
	target := p.GoRemote(path)
	return listRemote(paneIdx, p.Host(), target, previous)
}

// remoteEnter descends into the highlighted entry of a remote pane. Symlinks
// are worth trying: `ls -l` cannot say whether one points at a directory, so
// the attempt either lands or comes back as a plain error.
func (m *Model) remoteEnter(paneIdx int) tea.Cmd {
	p := &m.panes[paneIdx]
	cur, ok := p.Current()
	if !ok {
		return nil
	}
	if cur.Name == ".." {
		return m.remoteAscend(paneIdx)
	}
	if !cur.IsDir && cur.Mode&os.ModeSymlink == 0 {
		m.errText = "not a directory: " + cur.Name
		return nil
	}
	return m.remoteNavigate(paneIdx, cur.Name)
}

// remoteAscend moves a remote pane to its parent directory.
func (m *Model) remoteAscend(paneIdx int) tea.Cmd {
	p := &m.panes[paneIdx]
	parent := remote.Parent(p.Path)
	if parent == p.Path {
		return nil
	}
	return m.remoteNavigate(paneIdx, parent)
}

// refreshPane re-reads a pane after an operation, over ssh when it is remote.
func (m *Model) refreshPane(paneIdx int) tea.Cmd {
	p := &m.panes[paneIdx]
	if !p.IsRemote() {
		p.Refresh()
		return nil
	}
	return listRemote(paneIdx, p.Host(), p.Path, p.Path)
}

// commitAddress interprets what was typed in the address bar. Remote targets
// switch the pane to ssh; on a pane that is already remote a bare path stays
// remote, and the "local:" prefix is the way back to the local filesystem.
func (m *Model) commitAddress() (tea.Cmd, error) {
	p := &m.panes[m.active]
	value := strings.TrimSpace(p.AddrValue())

	if rest, ok := cutLocalPrefix(value); ok {
		if p.IsRemote() {
			p.LeaveRemote(homeOrRoot())
		}
		p.SetAddrValue(rest)
		if err := p.CommitEditPath(); err != nil {
			return nil, err // stay in the bar so the path can be corrected
		}
		m.mode = modeNormal
		return nil, nil
	}

	if h, path, ok := remote.Parse(value); ok {
		p.FinishEditPath()
		if remote.Connected(h) {
			m.mode = modeNormal
			return m.openRemote(m.active, h, path), nil
		}
		// Not connected yet: hand the target to the connection modal, which
		// collects a password or a host-key decision as needed.
		return m.openConnForAddress(h, path), nil
	}

	if p.IsRemote() {
		p.FinishEditPath()
		m.mode = modeNormal
		return m.remoteNavigate(m.active, value), nil
	}

	if err := p.CommitEditPath(); err != nil {
		return nil, err // stay in the bar so the path can be corrected
	}
	m.mode = modeNormal
	return nil, nil
}

// cutLocalPrefix strips the explicit "go back to this machine" prefixes.
func cutLocalPrefix(s string) (string, bool) {
	for _, prefix := range []string{"local:", "file://"} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			if rest == "" {
				rest = homeOrRoot()
			}
			return rest, true
		}
	}
	return "", false
}

// homeOrRoot is the local directory to fall back to when leaving a remote pane.
func homeOrRoot() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return string(os.PathSeparator)
}

// beginRemoteOp assembles an ssh-backed job when either pane is on a host.
// Direction follows the panes: the active pane is always the source.
func (m *Model) beginRemoteOp(op fileops.Op) {
	src := &m.panes[m.active]
	dst := &m.panes[1-m.active]

	switch op {
	case fileops.OpCopy, fileops.OpMove, fileops.OpDelete:
	default:
		m.errText = "archive operations are not available over ssh — transfer the files first"
		return
	}
	if src.InArchive() || dst.InArchive() {
		m.errText = "not available inside an archive — unpack it first"
		return
	}

	names := src.SelectedNames()
	if len(names) == 0 {
		return
	}
	move := op == fileops.OpMove

	switch {
	case src.IsRemote() && op == fileops.OpDelete:
		m.pending = fileops.Job{
			Op:   fileops.OpRemoteDelete,
			Host: src.Host(),
			Srcs: remotePaths(src.Path, names),
		}
		m.willOverwrite = false

	case src.IsRemote() && dst.IsRemote():
		if src.Host() != dst.Host() {
			m.errText = "copying between two hosts is not supported — go via this machine"
			return
		}
		if src.Path == dst.Path {
			m.errText = "source and destination are the same directory"
			return
		}
		m.pending = fileops.Job{
			Op:   fileops.OpRemoteCopy,
			Host: src.Host(),
			Srcs: remotePaths(src.Path, names),
			Dest: dst.Path,
			Move: move,
		}
		m.willOverwrite = false

	case src.IsRemote(): // remote → local
		m.pending = fileops.Job{
			Op:   fileops.OpDownload,
			Host: src.Host(),
			Srcs: remotePaths(src.Path, names),
			Dest: dst.Path,
			Move: move,
		}
		m.willOverwrite = anyExist(dst.Path, names)

	default: // local → remote
		srcs := make([]string, 0, len(names))
		for _, n := range names {
			srcs = append(srcs, filepath.Join(src.Path, n))
		}
		m.pending = fileops.Job{
			Op:   fileops.OpUpload,
			Host: dst.Host(),
			Srcs: srcs,
			Dest: dst.Path,
			Move: move,
		}
		// Checking the far side would mean another round trip before the
		// dialog can be drawn; the dialog warns unconditionally instead.
		m.willOverwrite = false
	}

	m.mode = modeConfirm
}

// remotePaths joins entry names onto a remote directory.
func remotePaths(dir string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, remote.Join(dir, n))
	}
	return out
}
