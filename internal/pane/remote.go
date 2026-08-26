package pane

import (
	"github.com/kiruva/tyr/internal/remote"
)

// Remote browsing works differently from local and archive browsing: a listing
// costs a network round trip, so the pane never fetches anything itself. It
// records where it wants to be, reports Loading, and waits for the app layer to
// hand back entries (or an error) from a Bubble Tea command.

// IsRemote reports whether the pane is browsing over ssh.
func (m *Model) IsRemote() bool { return !m.host.IsZero() }

// Host returns the ssh destination the pane is browsing (zero if local).
func (m *Model) Host() remote.Host { return m.host }

// Loading reports whether a remote listing is in flight.
func (m *Model) Loading() bool { return m.loading }

// OpenRemote points the pane at host:path and marks it as loading. The path may
// be empty, meaning the login directory — the listing reports where that was.
func (m *Model) OpenRemote(h remote.Host, path string) {
	m.host = h
	m.Path = path
	m.archive, m.vpath, m.members = "", "", nil
	m.Entries = nil
	m.remoteRaw = nil
	m.focusPending = ""
	m.Cursor = 0
	m.selected = map[string]bool{}
	m.filter = ""
	m.clearDirSizes()
	m.loading = true
	m.syncAddr()
}

// GoRemote navigates the current host to path (absolute, or relative to the
// current directory) and marks the pane as loading.
func (m *Model) GoRemote(path string) string {
	target := remote.Join(m.Path, path)
	m.Path = target
	m.loading = true
	m.syncAddr()
	return target
}

// FocusAfterLoad remembers an entry to highlight once the next listing arrives —
// a remote refresh is asynchronous, so the caller cannot move the cursor itself.
func (m *Model) FocusAfterLoad(name string) { m.focusPending = name }

// SetRemoteListing installs a completed listing. Stale replies — a listing for
// a directory the pane has already navigated away from — are ignored.
func (m *Model) SetRemoteListing(requested string, l remote.Listing) bool {
	if !m.IsRemote() || requested != m.Path {
		return false
	}
	m.Path = l.Dir
	m.loading = false
	m.Cursor = 0
	m.selected = map[string]bool{}
	m.clearDirSizes()

	m.remoteRaw = make([]Entry, 0, len(l.Entries))
	for _, e := range l.Entries {
		m.remoteRaw = append(m.remoteRaw, Entry{
			Name:    e.Name,
			IsDir:   e.IsDir,
			Size:    e.Size,
			Mode:    e.Mode,
			ModTime: e.ModTime,
			HasSize: !e.IsDir,
		})
	}
	m.reload()
	if m.focusPending != "" {
		m.Focus(m.focusPending)
		m.focusPending = ""
	}
	return true
}

// RemoteFailed reverts a failed navigation, putting the pane back where it was.
func (m *Model) RemoteFailed(requested, previous string) bool {
	if !m.IsRemote() || requested != m.Path {
		return false
	}
	m.loading = false
	if previous != "" {
		m.Path = previous
	}
	m.syncAddr()
	return true
}

// LeaveRemote drops the connection state and returns the pane to a local path.
func (m *Model) LeaveRemote(path string) {
	m.host = remote.Host{}
	m.loading = false
	m.remoteRaw = nil
	m.focusPending = ""
	m.Path = path
	m.enterDir()
}

// RemoteEntryPath is the full remote path of the highlighted entry.
func (m *Model) RemoteEntryPath() (string, bool) {
	cur, ok := m.Current()
	if !ok || cur.Name == ".." {
		return "", false
	}
	return remote.Join(m.Path, cur.Name), true
}

// remoteRawEntries is the cached listing with ".." in front; the view options
// are applied by reloadEntries, the same as for a local directory.
func (m *Model) remoteRawEntries() []Entry {
	entries := make([]Entry, 0, len(m.remoteRaw)+1)
	if m.Path != "/" {
		entries = append(entries, Entry{Name: "..", IsDir: true})
	}
	return append(entries, m.remoteRaw...)
}
