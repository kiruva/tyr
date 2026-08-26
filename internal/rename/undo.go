package rename

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// A rename batch is undone by running it backwards. The record to run it from is
// what Apply returned: the renames that actually happened, as "old → new" pairs
// relative to the same root.
//
// The one subtlety is directories. Apply renames the entries inside a directory
// before the directory itself, so a child's record is written in terms of the old
// directory name — "v1/notes.txt → v1-notes.txt" even though the file ended up at
// "v2/v1-notes.txt". Undo therefore restores the shallowest names first, and the
// checks below skip anything sitting under a directory that has yet to be put
// back, because until then its recorded path is not a path.

// PlanUndo inverts an applied batch and marks what can no longer be undone: a
// name that has since moved on ("gone"), or one whose old name is taken again
// ("exists"). The rest is still undone — a stale entry never blocks the batch.
func PlanUndo(root string, applied []Change) []Change {
	inv := make([]Change, 0, len(applied))
	for _, c := range applied {
		if !c.Changed || c.Problem != "" {
			continue // never happened, so there is nothing to put back
		}
		inv = append(inv, Change{
			Rel:     c.NewRel(),
			New:     filepath.Base(c.Rel),
			IsDir:   c.IsDir,
			Changed: true,
		})
	}

	deferred := deferredDirs(inv)
	sources := make(map[string]bool, len(inv))
	for _, c := range inv {
		sources[fold(c.Rel)] = true
	}

	seen := make(map[string]int, len(inv))
	for i := range inv {
		c := &inv[i]
		key := fold(c.NewRel())

		if j, dup := seen[key]; dup {
			c.Problem = "same name as " + filepath.Base(inv[j].Rel)
			continue
		}
		seen[key] = i

		if under(c.Rel, deferred) {
			continue // checkable only once the directory above it is restored
		}
		if _, err := os.Lstat(filepath.Join(root, c.Rel)); err != nil {
			c.Problem = "gone"
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, c.NewRel())); err != nil {
			continue // the old name is free
		}
		if key == fold(c.Rel) || sources[key] {
			continue // a change of case, or a name the batch itself frees up
		}
		c.Problem = "exists"
	}
	return inv
}

// Undo restores an inverted batch, shallowest path first. It is Apply in reverse
// and keeps every one of its promises: nothing is renamed onto an existing name,
// and a collision inside one directory is staged through temporary names.
func Undo(ctx context.Context, root string, inverted []Change, step func(string)) ([]Change, error) {
	return applyGroups(ctx, root, byDir(Applicable(inverted), false), step)
}

// deferredDirs is the set of old directory paths the batch has yet to restore.
func deferredDirs(inv []Change) map[string]bool {
	out := make(map[string]bool)
	for _, c := range inv {
		if c.IsDir {
			out[c.NewRel()] = true
		}
	}
	return out
}

// under reports whether rel sits inside one of the given directories.
func under(rel string, dirs map[string]bool) bool {
	if len(dirs) == 0 {
		return false
	}
	sep := string(filepath.Separator)
	for dir := filepath.Dir(rel); dir != "." && dir != sep && dir != ""; dir = filepath.Dir(dir) {
		if dirs[dir] {
			return true
		}
	}
	return false
}

// UndoLabel describes a batch for a prompt: "3 files", "a folder and 2 files".
func UndoLabel(changes []Change) string {
	var files, dirs int
	for _, c := range changes {
		if c.IsDir {
			dirs++
			continue
		}
		files++
	}

	var parts []string
	if dirs > 0 {
		parts = append(parts, plural(dirs, "folder"))
	}
	if files > 0 {
		parts = append(parts, plural(files, "file"))
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, " and ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
