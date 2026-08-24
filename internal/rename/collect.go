package rename

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Options is what the tool's toggles mean to collection.
type Options struct {
	Recursive bool // descend into the selected directories
	Dirs      bool // let directory names be renamed too
	Hidden    bool // include dotfiles found while descending
	Limit     int  // stop after this many candidates (0 = DefaultLimit)
}

// DefaultLimit caps a recursive sweep. A batch rename is not undoable, so an
// accidental "recursive, from /" should stop and say so rather than build a
// hundred-thousand-row preview.
const DefaultLimit = 20000

// Collect walks the selected names under root and returns what a rename could
// touch, in a stable order. The bool reports that the limit was hit and the list
// is therefore partial.
//
// Names are the pane's selection: a plain file contributes itself, a directory
// contributes its own name (with Options.Dirs) and, recursively, everything
// inside it (with Options.Recursive).
func Collect(root string, names []string, opt Options) ([]Candidate, bool, error) {
	limit := opt.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	var (
		cands     []Candidate
		truncated bool
	)
	add := func(rel string, isDir bool, info os.FileInfo) bool {
		if len(cands) >= limit {
			truncated = true
			return false
		}
		c := Candidate{Rel: rel, IsDir: isDir}
		if info != nil {
			c.Mod = info.ModTime()
		}
		cands = append(cands, c)
		return true
	}

	for _, name := range names {
		if name == ".." || name == "" {
			continue
		}
		full := filepath.Join(root, name)
		info, err := os.Lstat(full)
		if err != nil {
			return nil, false, err
		}

		if !info.IsDir() {
			if !add(name, false, info) {
				break
			}
			continue
		}
		if opt.Dirs && !add(name, true, info) {
			break
		}
		if !opt.Recursive {
			continue
		}
		if err := walk(root, name, opt, add); err != nil {
			return nil, false, err
		}
		if truncated {
			break
		}
	}

	sort.Slice(cands, func(i, j int) bool { return cands[i].Rel < cands[j].Rel })
	return cands, truncated, nil
}

// walk adds everything under the directory at rel. An entry that cannot be read
// is skipped rather than failing the sweep: one unreadable subdirectory should
// not cost the whole preview.
func walk(root, rel string, opt Options, add func(string, bool, os.FileInfo) bool) error {
	base := filepath.Join(root, rel)

	return filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == base {
			return nil // the directory itself was handled by the caller
		}

		name := d.Name()
		if !opt.Hidden && len(name) > 0 && name[0] == '.' {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		child, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() && !opt.Dirs {
			return nil // descend, but do not rename the directory itself
		}

		info, _ := d.Info()
		if !add(child, d.IsDir(), info) {
			return filepath.SkipAll
		}
		return nil
	})
}
