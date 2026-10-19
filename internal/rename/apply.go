package rename

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// tempPrefix marks the staging names Apply uses to get around a collision. It is
// long and specific so a leftover from a crash is recognisable.
const tempPrefix = ".tyr-rename-"

// Apply performs the plan. Only the applicable changes run, deepest path first
// so renaming a directory never invalidates the paths of the entries inside it,
// and step is called with each source path as it completes.
//
// Renames that would overwrite each other are staged through temporary names, so
// a swap (a→b, b→a), a chain (a→b, b→c) and a change of case alone all work.
// Nothing is ever renamed onto an existing name: a target that appeared since the
// plan was made stops the batch with an error.
//
// The returned slice is what actually happened, in the order it happened. Hand it
// to PlanUndo to put it back — including after an error, where it is the part of
// the batch that did land, and including after a cancellation, which stops the
// batch before the next plain rename and is reported the same way.
func Apply(ctx context.Context, root string, changes []Change, step func(string)) ([]Change, error) {
	return applyGroups(ctx, root, byDir(Applicable(changes), true), step)
}

// applyGroups runs the groups in order, collecting what completed. A group is
// one directory's worth of renames.
func applyGroups(ctx context.Context, root string, groups [][]Change, step func(string)) ([]Change, error) {
	var done []Change
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		applied, err := applyDir(ctx, root, group, step)
		done = append(done, applied...)
		if err != nil {
			return done, err
		}
	}
	return done, nil
}

// byDir groups the changes by their directory. A rename never leaves its
// directory, so a collision is always between two entries of one group — which is
// what lets the staging below stay local.
//
// Deepest first is the order for applying: a directory is renamed only once the
// entries inside it are done. Undo needs the reverse — a directory has to be back
// under its old name before the paths recorded inside it mean anything again.
func byDir(changes []Change, deepestFirst bool) [][]Change {
	groups := map[string][]Change{}
	var dirs []string
	for _, c := range changes {
		dir := filepath.Dir(c.Rel)
		if _, ok := groups[dir]; !ok {
			dirs = append(dirs, dir)
		}
		groups[dir] = append(groups[dir], c)
	}

	sort.Slice(dirs, func(i, j int) bool {
		di, dj := depth(dirs[i]), depth(dirs[j])
		if di != dj {
			return (di > dj) == deepestFirst
		}
		return dirs[i] < dirs[j]
	})

	out := make([][]Change, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, groups[d])
	}
	return out
}

// staged is a rename parked under a temporary name, waiting for its real one.
type staged struct {
	tmp    string // absolute path it is sitting at
	to     string // absolute path it is going to
	from   string // absolute path it came from, for the rollback
	change Change // what it is doing, for the progress callback and the undo record
}

// applyDir renames one directory's worth. The colliding ones are parked under
// temporary names first, then the plain ones run, then the parked ones land.
// A cancellation is honoured during the plain renames — one directory can hold
// thousands — and puts the parked ones back; once they start landing, they all
// land, since stopping half-way through a swap is the state worth avoiding.
func applyDir(ctx context.Context, root string, group []Change, step func(string)) ([]Change, error) {
	// Names this group frees up. A target among them cannot simply be renamed
	// onto, so the source goes to a temporary name first.
	sources := make(map[string]bool, len(group))
	for _, c := range group {
		sources[fold(filepath.Base(c.Rel))] = true
	}

	var (
		pending []staged
		direct  []Change
		done    []Change
	)
	for _, c := range group {
		if sources[fold(c.New)] {
			from := filepath.Join(root, c.Rel)
			tmp, err := tempName(from)
			if err != nil {
				return nil, unstage(pending, err)
			}
			if err := os.Rename(from, tmp); err != nil {
				return nil, unstage(pending, err)
			}
			pending = append(pending, staged{tmp: tmp, to: filepath.Join(root, c.NewRel()), from: from, change: c})
			continue
		}
		direct = append(direct, c)
	}

	for _, c := range direct {
		if err := ctx.Err(); err != nil {
			return done, unstage(pending, err)
		}
		from, to := filepath.Join(root, c.Rel), filepath.Join(root, c.NewRel())
		if err := renameNew(from, to); err != nil {
			return done, unstage(pending, err)
		}
		done = append(done, c)
		step(c.Rel)
	}

	for i, p := range pending {
		if err := renameNew(p.tmp, p.to); err != nil {
			// What landed stays landed and is in done, for the undo. Only
			// what is still parked goes back.
			return done, unstage(pending[i:], err)
		}
		done = append(done, p.change)
		step(p.change.Rel)
	}
	return done, nil
}

// renameNew renames from onto to, refusing to replace anything already there.
// os.Rename would happily overwrite it, and a batch rename that silently eats a
// file is not worth the convenience.
func renameNew(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s already exists", filepath.Base(to))
	}
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(from), err)
	}
	return nil
}

// tempName finds an unused staging name next to path.
func tempName(path string) (string, error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	for i := 0; i < 1000; i++ {
		cand := filepath.Join(dir, tempPrefix+strconv.Itoa(i)+"-"+base)
		if _, err := os.Lstat(cand); err != nil {
			return cand, nil
		}
	}
	return "", fmt.Errorf("no free temporary name for %s", base)
}

// unstage puts parked entries back where they came from, so a failure part-way
// through a group does not leave anything under a temporary name. It never
// puts one back over something: in a swap the old name may already hold the
// other file. What cannot go back stays parked, and the returned error — err,
// which is what got us here — names where, so it can be found.
func unstage(pending []staged, err error) error {
	var stuck []string
	for _, p := range pending {
		if renameNew(p.tmp, p.from) != nil {
			stuck = append(stuck, filepath.Base(p.from)+" is at "+filepath.Base(p.tmp))
		}
	}
	if len(stuck) > 0 {
		return fmt.Errorf("%w (%s)", err, strings.Join(stuck, ", "))
	}
	return err
}

// IsStagingName reports whether a name is a leftover from an interrupted batch,
// which is worth telling the user about rather than hiding.
func IsStagingName(name string) bool { return strings.HasPrefix(name, tempPrefix) }
