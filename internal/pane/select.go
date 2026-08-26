package pane

import "strings"

// Selection beyond one entry at a time: everything visible, the inverse of what
// is marked now, or whatever matches a mask. All three work on the entries the
// pane is currently showing, so a filtered pane selects only within the filter —
// which is the point of combining the two.

// SelectAll marks every visible entry (never "..").
func (m *Model) SelectAll() int {
	n := 0
	for _, e := range m.Entries {
		if e.Name == ".." || m.selected[e.Name] {
			continue
		}
		m.selected[e.Name] = true
		n++
	}
	return n
}

// InvertSelection flips every visible entry's mark and returns how many end up
// selected.
func (m *Model) InvertSelection() int {
	for _, e := range m.Entries {
		if e.Name == ".." {
			continue
		}
		if m.selected[e.Name] {
			delete(m.selected, e.Name)
		} else {
			m.selected[e.Name] = true
		}
	}
	return len(m.selected)
}

// SelectMatching marks (on) or unmarks (off) the visible entries matching mask,
// and returns how many changed. The mask is one or more patterns separated by
// ";" — each a glob when it has a wildcard, a substring otherwise, matched the
// same way the pane filter matches.
func (m *Model) SelectMatching(mask string, on bool) int {
	patterns := splitMask(mask)
	if len(patterns) == 0 {
		return 0
	}

	n := 0
	for _, e := range m.Entries {
		if e.Name == ".." || !matchAny(e.Name, patterns) {
			continue
		}
		if on == m.selected[e.Name] {
			continue // already in the wanted state
		}
		if on {
			m.selected[e.Name] = true
		} else {
			delete(m.selected, e.Name)
		}
		n++
	}
	return n
}

// splitMask breaks "*.go;*.md" into its patterns, dropping empty ones.
func splitMask(mask string) []string {
	var out []string
	for _, p := range strings.Split(mask, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func matchAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if MatchName(name, p) {
			return true
		}
	}
	return false
}
