// Package pane models a single directory view: its path, entries, and cursor.
package pane

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/remote"
	"github.com/kiruva/tyr/internal/ui"
)

// Model is one dual-pane side.
type Model struct {
	Path    string
	Entries []Entry
	Cursor  int

	width, height int

	sort       SortMode
	showHidden bool
	selected   map[string]bool  // keys are entry names in the current dir
	filter     string           // narrows the listing; "" shows everything
	dirSizes   map[string]int64 // measured directory totals, by entry name

	// address bar (top line): mirrors the location, editable for direct jumps
	addr        textinput.Model
	editingAddr bool

	// archive browsing (virtual filesystem inside a packed file)
	archive string           // "" = real filesystem; otherwise the archive's real path
	vpath   string           // current virtual directory within the archive ("" = root)
	members []fileops.Member // cached full member listing

	// remote browsing over ssh (zero host = local)
	host         remote.Host
	loading      bool    // a remote listing is in flight
	remoteRaw    []Entry // last listing, before hidden-file filtering and sorting
	focusPending string  // entry to highlight once the next listing lands
}

// New builds a pane rooted at path and loads its contents.
func New(path string) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 0
	ti.ShowSuggestions = true

	m := Model{Path: path, selected: map[string]bool{}, addr: ti}
	m.reload()
	return m
}

// SetSize records the pane's outer dimensions (used for paging math and render).
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.addr.Width = max(w-3, 1) // borders plus room for the cursor
}

func (m *Model) reload() {
	m.reloadEntries()
	m.syncAddr()
}

// reloadEntries rebuilds the visible listing: read it from wherever the pane is
// pointed, then apply the view options — hidden files, the filter, any measured
// directory totals — and sort what is left.
func (m *Model) reloadEntries() {
	var entries []Entry

	switch {
	case m.IsRemote():
		entries = m.remoteRawEntries()
	case m.archive != "":
		entries = virtualEntries(m.members, m.vpath)
	default:
		hasParent := filepath.Dir(m.Path) != m.Path
		read, err := readRaw(m.Path, hasParent)
		if err != nil {
			m.Entries = nil
			m.Cursor = 0
			return
		}
		entries = read
	}

	if !m.showHidden {
		entries = dropDotfiles(entries)
	}
	entries = m.applyFilter(entries)
	m.applyDirSizes(entries)
	sortEntries(entries, m.sort)
	m.Entries = entries
	m.clampCursor()
}

func dropDotfiles(entries []Entry) []Entry {
	filtered := entries[:0]
	for _, e := range entries {
		if !e.isDotfile() {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func (m *Model) clampCursor() {
	if m.Cursor >= len(m.Entries) {
		m.Cursor = len(m.Entries) - 1
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
}

// visibleRows is how many entry rows fit in the pane body (excludes borders/title).
func (m Model) visibleRows() int {
	return max(m.height-2-1, 1)
}

// Current returns the entry under the cursor, if any.
func (m *Model) Current() (Entry, bool) {
	if m.Cursor < 0 || m.Cursor >= len(m.Entries) {
		return Entry{}, false
	}
	return m.Entries[m.Cursor], true
}

// Focus moves the cursor to the named entry when it is visible; entries that are
// filtered out (a dotfile with hidden files off) leave the cursor alone.
func (m *Model) Focus(name string) {
	for i, e := range m.Entries {
		if e.Name == name {
			m.Cursor = i
			return
		}
	}
}

// Cursor movement ------------------------------------------------------------

func (m *Model) MoveUp() {
	if m.Cursor > 0 {
		m.Cursor--
	}
}

func (m *Model) MoveDown() {
	if m.Cursor < len(m.Entries)-1 {
		m.Cursor++
	}
}

func (m *Model) PageUp()   { m.Cursor = max(m.Cursor-m.visibleRows(), 0) }
func (m *Model) PageDown() { m.Cursor = min(m.Cursor+m.visibleRows(), len(m.Entries)-1) }
func (m *Model) Top()      { m.Cursor = 0 }
func (m *Model) Bottom()   { m.Cursor = max(len(m.Entries)-1, 0) }

// Navigation -----------------------------------------------------------------

// Enter descends into the highlighted directory (or ".." to the parent). Inside
// an archive it walks the virtual tree and exits the archive at the root.
func (m *Model) Enter() {
	if m.IsRemote() {
		return // remote navigation needs a round trip; the app layer drives it
	}
	cur, ok := m.Current()
	if !ok || !cur.IsDir {
		return
	}
	if m.archive != "" {
		m.navigateArchive(cur.Name)
		return
	}
	if cur.Name == ".." {
		m.Path = filepath.Dir(m.Path)
	} else {
		m.Path = filepath.Join(m.Path, cur.Name)
	}
	m.enterDir()
}

// Ascend moves to the parent directory (or up/out of the archive).
func (m *Model) Ascend() {
	if m.IsRemote() {
		return // see Enter
	}
	if m.archive != "" {
		m.navigateArchive("..")
		return
	}
	if parent := filepath.Dir(m.Path); parent != m.Path {
		m.Path = parent
		m.enterDir()
	}
}

// GoTo moves the pane to a real directory on this machine, leaving any archive
// it was browsing. It is how the app layer jumps a pane somewhere the user did
// not walk to — a search hit, or a location restored from the last session.
func (m *Model) GoTo(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}
	m.archive, m.vpath, m.members = "", "", nil
	m.Path = path
	m.enterDir()
	return nil
}

func (m *Model) enterDir() {
	m.Cursor = 0
	m.selected = map[string]bool{} // selection is per-directory
	m.filter = ""                  // and so is the filter
	m.clearDirSizes()
	m.reload()
}

// Selection ------------------------------------------------------------------

// ToggleSelect flips the selection state of the current entry (never ".."),
// then advances the cursor — the familiar "tap Space down a list" flow.
func (m *Model) ToggleSelect() {
	cur, ok := m.Current()
	if !ok || cur.Name == ".." {
		return
	}
	if m.selected[cur.Name] {
		delete(m.selected, cur.Name)
	} else {
		m.selected[cur.Name] = true
	}
	m.MoveDown()
}

// SelectedCount returns how many entries are selected.
func (m *Model) SelectedCount() int { return len(m.selected) }

// ClearSelection drops all marks (called after an operation completes).
func (m *Model) ClearSelection() { m.selected = map[string]bool{} }

// Refresh re-reads the current location in place, keeping path and cursor. When
// browsing an archive it re-lists the members so on-disk changes are reflected.
func (m *Model) Refresh() {
	if m.archive != "" {
		if members, err := fileops.ListMembers(m.archive); err == nil {
			m.members = members
		}
	}
	m.reload()
}

// SelectedNames returns the selected entry names, or the current entry's name
// if nothing is explicitly selected (the "act on cursor" fallback).
func (m *Model) SelectedNames() []string {
	if len(m.selected) > 0 {
		names := make([]string, 0, len(m.selected))
		for n := range m.selected {
			names = append(names, n)
		}
		return names
	}
	if cur, ok := m.Current(); ok && cur.Name != ".." {
		return []string{cur.Name}
	}
	return nil
}

// View options ---------------------------------------------------------------

// ToggleHidden shows or hides dotfiles.
func (m *Model) ToggleHidden() {
	m.showHidden = !m.showHidden
	m.reload()
}

// CycleSort advances to the next sort mode.
func (m *Model) CycleSort() {
	m.sort = m.sort.Next()
	m.reload()
}

// SetView installs a sort mode and hidden-file setting in one go, which is how
// a session saved on the last run is restored without reloading twice.
func (m *Model) SetView(sort SortMode, showHidden bool) {
	m.sort, m.showHidden = sort, showHidden
	m.reload()
}

// SortMode returns the active sort mode (for display).
func (m *Model) SortModeLabel() string { return m.sort.String() }

// HiddenShown reports whether dotfiles are visible.
func (m *Model) HiddenShown() bool { return m.showHidden }

// Rendering ------------------------------------------------------------------

// View renders the pane using its stored size.
func (m Model) View(active bool) string {
	border := ui.InactiveBorder
	if active {
		border = ui.ActiveBorder
	}

	innerW := max(m.width-2, 1)
	innerH := max(m.height-2, 1)

	title := m.addrView(innerW, active)
	rows := max(innerH-1, 1)

	start := 0
	if m.Cursor >= rows {
		start = m.Cursor - rows + 1
	}
	end := min(start+rows, len(m.Entries))

	var b strings.Builder
	b.WriteString(title)
	if m.loading && len(m.Entries) == 0 {
		b.WriteString("\n" + ui.Faint.Render("  connecting…"))
		return border.Width(innerW).Height(innerH).Render(b.String())
	}
	for i := start; i < end; i++ {
		e := m.Entries[i]
		b.WriteByte('\n')
		line := formatEntry(e, m.selected[e.Name], innerW)
		switch {
		case i == m.Cursor && active:
			line = ui.Cursor.Width(innerW).Render(line)
		case i == m.Cursor:
			line = ui.CursorInactive.Width(innerW).Render(line)
		case m.selected[e.Name]:
			line = ui.Selected.Render(line)
		case e.IsDir:
			line = ui.DirName.Render(line)
		}
		b.WriteString(line)
	}

	return border.Width(innerW).Height(innerH).Render(b.String())
}

// formatEntry lays out "gutter name ........ size" padded to width.
func formatEntry(e Entry, selected bool, width int) string {
	gutter := " "
	if selected {
		gutter = "●"
	}

	name := e.Name
	if e.IsDir && e.Name != ".." {
		name += "/"
	}

	size := ""
	if e.HasSize {
		size = HumanSize(e.Size)
	}

	avail := width - lipgloss.Width(gutter) - 1 // gutter + one space after it
	gap := avail - lipgloss.Width(name) - lipgloss.Width(size)
	if gap < 1 {
		name = truncate(name, max(avail-lipgloss.Width(size)-1, 1))
		gap = max(avail-lipgloss.Width(name)-lipgloss.Width(size), 0)
	}
	return gutter + " " + name + strings.Repeat(" ", gap) + size
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	var out strings.Builder
	for _, r := range s {
		if lipgloss.Width(out.String()+string(r)) > w-1 {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + "…"
}

// HumanSize renders a byte count the way the panes show it.
func HumanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(size)/float64(div), "KMGTPE"[exp])
}
