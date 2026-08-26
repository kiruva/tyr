package pane

import (
	"path/filepath"
	"strings"

	"github.com/kiruva/tyr/internal/fileops"
)

// InArchive reports whether the pane is currently browsing inside an archive.
func (m *Model) InArchive() bool { return m.archive != "" }

// ArchivePath returns the real path of the archive being browsed (or "").
func (m *Model) ArchivePath() string { return m.archive }

// VPath returns the current virtual directory within the archive ("" = root).
func (m *Model) VPath() string { return m.vpath }

// EnterArchive switches the pane into virtual browsing of archivePath.
func (m *Model) EnterArchive(archivePath string) error {
	members, err := fileops.ListMembers(archivePath)
	if err != nil {
		return err
	}
	m.archive = archivePath
	m.vpath = ""
	m.members = members
	m.Cursor = 0
	m.selected = map[string]bool{}
	m.filter = ""
	m.clearDirSizes()
	m.reload()
	return nil
}

func (m *Model) exitArchive() {
	m.archive = ""
	m.vpath = ""
	m.members = nil
	m.Cursor = 0
	m.selected = map[string]bool{}
	m.filter = ""
	m.clearDirSizes()
	m.reload()
}

// navigateArchive handles Enter/".." within the virtual tree.
func (m *Model) navigateArchive(name string) {
	if name == ".." {
		if m.vpath == "" {
			m.exitArchive()
			return
		}
		m.vpath = parentVirtual(m.vpath)
	} else {
		m.vpath = joinVirtual(m.vpath, name)
	}
	m.Cursor = 0
	m.selected = map[string]bool{}
	m.filter = ""
	m.clearDirSizes()
	m.reload()
}

// CurrentMemberPath returns the full in-archive path of the highlighted file.
func (m *Model) CurrentMemberPath() (string, bool) {
	if m.archive == "" {
		return "", false
	}
	cur, ok := m.Current()
	if !ok || cur.IsDir || cur.Name == ".." {
		return "", false
	}
	return joinVirtual(m.vpath, cur.Name), true
}

// displayPath is what the pane title shows.
func (m Model) displayPath() string {
	if !m.host.IsZero() {
		return m.host.Display(m.Path)
	}
	if m.archive == "" {
		return m.Path
	}
	base := filepath.Base(m.archive)
	if m.vpath == "" {
		return base + "/"
	}
	return base + "/" + m.vpath + "/"
}

// Title exposes the display path for the status bar.
func (m *Model) Title() string { return m.displayPath() }

// virtualEntries computes the immediate children of vpath from a flat member
// list, synthesizing directory entries and prepending "..".
func virtualEntries(members []fileops.Member, vpath string) []Entry {
	prefix := ""
	if vpath != "" {
		prefix = vpath + "/"
	}

	seenDir := map[string]bool{}
	entries := []Entry{{Name: "..", IsDir: true}}

	for _, mem := range members {
		if !strings.HasPrefix(mem.Path, prefix) {
			continue
		}
		rest := mem.Path[len(prefix):]
		if rest == "" {
			continue
		}
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			// child lives in a subdirectory → surface that dir once
			dir := rest[:i]
			if !seenDir[dir] {
				seenDir[dir] = true
				entries = append(entries, Entry{Name: dir, IsDir: true})
			}
			continue
		}
		// direct child
		if mem.IsDir {
			if !seenDir[rest] {
				seenDir[rest] = true
				entries = append(entries, Entry{Name: rest, IsDir: true})
			}
		} else {
			entries = append(entries, Entry{Name: rest, IsDir: false, Size: mem.Size, HasSize: true})
		}
	}
	return entries
}

func joinVirtual(base, name string) string {
	if base == "" {
		return name
	}
	return base + "/" + name
}

func parentVirtual(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}
