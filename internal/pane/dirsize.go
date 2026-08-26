package pane

// A directory's own size says nothing about what is in it, so a pane leaves the
// size column blank for directories until something measures one. Measuring
// walks the tree, which is slow enough to belong off the UI thread: the app
// layer runs the walk and hands the total back here.
//
// Totals are cached per directory name and cleared whenever the pane moves,
// since the names then mean something else.

// DirSizeKnown reports whether the named directory already has a measured total.
func (m *Model) DirSizeKnown(name string) bool {
	_, ok := m.dirSizes[name]
	return ok
}

// SetDirSize records a measured total and shows it in the size column. When the
// pane is sorted by size the row moves, so the cursor follows the entry it was
// on rather than the position it was at.
func (m *Model) SetDirSize(name string, size int64) {
	if m.dirSizes == nil {
		m.dirSizes = map[string]int64{}
	}
	m.dirSizes[name] = size

	for i := range m.Entries {
		if m.Entries[i].IsDir && m.Entries[i].Name == name {
			m.Entries[i].Size = size
			m.Entries[i].HasSize = true
		}
	}

	if m.sort == SortSize {
		cur, ok := m.Current()
		sortEntries(m.Entries, m.sort)
		if ok {
			m.Focus(cur.Name)
		}
	}
}

// UnsizedDirs lists the visible directories that have not been measured yet.
func (m *Model) UnsizedDirs() []string {
	var out []string
	for _, e := range m.Entries {
		if e.IsDir && e.Name != ".." && !m.DirSizeKnown(e.Name) {
			out = append(out, e.Name)
		}
	}
	return out
}

// applyDirSizes puts cached totals back onto a freshly read listing.
func (m *Model) applyDirSizes(entries []Entry) {
	if len(m.dirSizes) == 0 {
		return
	}
	for i := range entries {
		if !entries[i].IsDir || entries[i].Name == ".." {
			continue
		}
		if size, ok := m.dirSizes[entries[i].Name]; ok {
			entries[i].Size = size
			entries[i].HasSize = true
		}
	}
}

// clearDirSizes forgets every measured total (the pane is moving elsewhere).
func (m *Model) clearDirSizes() { m.dirSizes = nil }
