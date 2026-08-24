package pane

import "sort"

// SortMode controls how entries are ordered within a pane. Directories always
// sort before files; the mode orders items within each group.
type SortMode int

const (
	SortName SortMode = iota // name A→Z
	SortSize                 // largest first
	SortTime                 // newest first
)

func (s SortMode) String() string {
	switch s {
	case SortSize:
		return "size"
	case SortTime:
		return "time"
	default:
		return "name"
	}
}

// ParseSort resolves a mode's name back into the mode, for a setting read from
// the config file. An unknown name reports false and is ignored by the caller.
func ParseSort(name string) (SortMode, bool) {
	switch name {
	case "name":
		return SortName, true
	case "size":
		return SortSize, true
	case "time":
		return SortTime, true
	}
	return SortName, false
}

// Next cycles to the following sort mode.
func (s SortMode) Next() SortMode {
	return (s + 1) % 3
}

// sortEntries orders entries in place: ".." first, then directories, then files,
// each group ordered by mode.
func sortEntries(entries []Entry, mode SortMode) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Name == ".." {
			return true
		}
		if b.Name == ".." {
			return false
		}
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		switch mode {
		case SortSize:
			if a.Size != b.Size {
				return a.Size > b.Size
			}
		case SortTime:
			if !a.ModTime.Equal(b.ModTime) {
				return a.ModTime.After(b.ModTime)
			}
		}
		return a.Name < b.Name
	})
}
