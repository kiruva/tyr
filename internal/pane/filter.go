package pane

import (
	"path/filepath"
	"strings"
)

// A filter narrows what a pane shows without touching the directory it is in.
// It is applied after the hidden-file rule and before sorting, so a filtered
// pane still sorts and pages the way an unfiltered one does. ".." is never
// filtered out: whatever is on screen, leaving the directory stays possible.
//
// A pattern containing a wildcard (*, ? or [) is matched as a glob against the
// whole name; anything else matches as a substring. Both are case-insensitive —
// a filter is something you type in a hurry.

// Filter returns the active filter pattern ("" when there is none).
func (m *Model) Filter() string { return m.filter }

// SetFilter narrows the listing to entries matching pattern and re-reads it.
func (m *Model) SetFilter(pattern string) {
	m.filter = pattern
	m.reloadEntries()
}

// ClearFilter drops the filter and shows everything again.
func (m *Model) ClearFilter() {
	if m.filter == "" {
		return
	}
	m.filter = ""
	m.reloadEntries()
}

// applyFilter keeps the entries matching the active pattern.
func (m *Model) applyFilter(entries []Entry) []Entry {
	if m.filter == "" {
		return entries
	}
	kept := entries[:0]
	for _, e := range entries {
		if e.Name == ".." || MatchName(e.Name, m.filter) {
			kept = append(kept, e)
		}
	}
	return kept
}

// MatchName reports whether name matches pattern: a glob when the pattern has
// a wildcard in it, a substring otherwise. Matching ignores case.
func MatchName(name, pattern string) bool {
	if pattern == "" {
		return true
	}
	name, pattern = strings.ToLower(name), strings.ToLower(pattern)
	if strings.ContainsAny(pattern, "*?[") {
		ok, err := filepath.Match(pattern, name)
		return err == nil && ok
	}
	return strings.Contains(name, pattern)
}
