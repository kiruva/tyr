package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/rename"
	"github.com/kiruva/tyr/internal/trash"
)

// Undo is a session-only history of the operations that can honestly be put
// back: a rename returns to its old name, a move goes back where it came from,
// a copy is the files it created and nothing else, and a delete that went to
// the trash comes out of it again.
//
// A permanent delete is not in here, and never will be. Neither is anything
// that ran over ssh — the far side is not tyr's to reason about — nor an
// archive operation, which rewrites a file rather than moving names around.

// undoDepth is how many operations back the history remembers. Nothing about it
// is written to disk.
const undoDepth = 20

// undoKind is which of the reversible operations an entry describes.
type undoKind int

const (
	undoRename undoKind = iota
	undoMove
	undoCopy
	undoTrash
)

// undoEntry is one operation that happened, kept so it can be reversed. Each
// kind carries only what its own reversal needs.
type undoEntry struct {
	kind  undoKind
	label string // what the prompt says was done

	root    string          // the directory a rename batch happened in
	changes []rename.Change // what the rename actually did
	moved   []fileops.Pair  // what a move moved, and to where
	created []string        // what a copy brought into being
	trashed []trash.Item    // what a delete put in the trash

	// landed is what a copy created or a move left behind, as it was when the
	// job finished, keyed by path. An undo only touches what still matches:
	// a copy edited since is somebody's work now, and a file put where a moved
	// one landed is not the one that was moved.
	landed map[string]os.FileInfo

	// permanent is set once the trash has refused a copy's undo — the copies
	// are on another volume — so the next Ctrl+Z offers a delete for good.
	permanent bool
}

// pushUndo records an operation that landed.
func (m *Model) pushUndo(e undoEntry) {
	m.undoStack = append(m.undoStack, e)
	if len(m.undoStack) > undoDepth {
		m.undoStack = m.undoStack[len(m.undoStack)-undoDepth:]
	}
}

// pushUndoRename records a rename batch, which is the one operation whose undo
// is planned rather than replayed.
func (m *Model) pushUndoRename(root string, applied []rename.Change) {
	if len(applied) == 0 {
		return
	}
	m.pushUndo(undoEntry{
		kind:    undoRename,
		label:   "rename of " + rename.UndoLabel(applied),
		root:    root,
		changes: applied,
	})
}

// recordUndo turns a finished job into a history entry, when it left anything
// worth reversing behind.
func (m *Model) recordUndo(res fileops.Result) {
	switch res.Op {
	case fileops.OpCopy:
		if len(res.Created) > 0 {
			m.pushUndo(undoEntry{
				kind:    undoCopy,
				label:   fmt.Sprintf("copy of %s", countLabel(len(res.Created))),
				created: res.Created,
				landed:  stampAll(res.Created),
			})
		}
	case fileops.OpConvert:
		// The originals are untouched, so putting a conversion back is removing
		// what it wrote — the same reversal as a copy's.
		if len(res.Created) > 0 {
			m.pushUndo(undoEntry{
				kind:    undoCopy,
				label:   fmt.Sprintf("conversion of %s", countLabel(len(res.Created))),
				created: res.Created,
				landed:  stampAll(res.Created),
			})
		}
	case fileops.OpMove:
		if len(res.Moved) > 0 {
			dsts := make([]string, len(res.Moved))
			for i, p := range res.Moved {
				dsts[i] = p.Dst
			}
			m.pushUndo(undoEntry{
				kind:   undoMove,
				label:  fmt.Sprintf("move of %s", countLabel(len(res.Moved))),
				moved:  res.Moved,
				landed: stampAll(dsts),
			})
		}
	case fileops.OpTrash:
		if len(res.Trashed) > 0 {
			m.pushUndo(undoEntry{
				kind:    undoTrash,
				label:   fmt.Sprintf("delete of %s", trash.Label(res.Trashed)),
				trashed: res.Trashed,
			})
		}
	}
}

// stampAll records how each path looks now, for unchanged to compare against.
func stampAll(paths []string) map[string]os.FileInfo {
	out := make(map[string]os.FileInfo, len(paths))
	for _, p := range paths {
		if info, err := os.Lstat(p); err == nil {
			out[p] = info
		}
	}
	return out
}

// unchanged reports whether path is still the thing the job left there: the
// same file (inode and device, or the Windows file index), the same kind, size
// and modification time. A directory's time moves when an entry is added to or
// taken out of it, which is what keeps a folder somebody has since filled from
// being swept up with the copy. What a sweep does reach goes to the trash.
func (e undoEntry) unchanged(path string) bool {
	then, ok := e.landed[path]
	if !ok {
		return false
	}
	now, err := os.Lstat(path)
	if err != nil || !os.SameFile(then, now) || then.Mode() != now.Mode() || !then.ModTime().Equal(now.ModTime()) {
		return false
	}
	return now.IsDir() || then.Size() == now.Size()
}

// openUndo builds the reverse of the most recent operation and asks to confirm
// it. An entry nothing can be done with is dropped rather than left at the top
// of the stack blocking the ones under it.
func (m *Model) openUndo() {
	if len(m.undoStack) == 0 {
		m.errText = "nothing to undo — the history is this session only"
		return
	}

	job, left, err := m.undoJob(m.undoStack[len(m.undoStack)-1])
	if err != nil {
		m.undoStack = m.undoStack[:len(m.undoStack)-1]
		m.errText = err.Error()
		return
	}

	m.pending = job
	m.undoLeft = left
	m.undoing = true
	m.willOverwrite = false
	m.mode = modeConfirm
}

// undoJob is the job that reverses one entry, and how many of the entry's
// files it leaves alone because they have changed since.
func (m *Model) undoJob(e undoEntry) (fileops.Job, int, error) {
	switch e.kind {
	case undoRename:
		plan := rename.PlanUndo(e.root, e.changes)
		if rename.Summarize(plan).Renamed == 0 {
			return fileops.Job{}, 0, fmt.Errorf("cannot undo that rename — those names have moved on since")
		}
		return fileops.Job{Op: fileops.OpRenameUndo, Dest: e.root, Renames: plan}, 0, nil

	case undoMove:
		pairs := make([]fileops.Pair, 0, len(e.moved))
		changed := 0
		for _, p := range e.moved {
			switch {
			case !localExists(p.Dst):
				continue // moved on again since; there is nothing to bring back
			case !e.unchanged(p.Dst):
				changed++ // something else is there now
				continue
			}
			pairs = append(pairs, fileops.Pair{Src: p.Dst, Dst: p.Src})
		}
		if len(pairs) == 0 {
			return fileops.Job{}, 0, fmt.Errorf("cannot undo that move — %s", movedOn(changed, "the files have moved on since"))
		}
		return fileops.Job{Op: fileops.OpMovePairs, Pairs: pairs, Dest: filepath.Dir(pairs[0].Dst)}, changed, nil

	case undoCopy:
		srcs := make([]string, 0, len(e.created))
		changed := 0
		for _, p := range e.created {
			switch {
			case !localExists(p):
				continue
			case !e.unchanged(p):
				changed++
				continue
			}
			srcs = append(srcs, p)
		}
		if len(srcs) == 0 {
			return fileops.Job{}, 0, fmt.Errorf("cannot undo that copy — %s", movedOn(changed, "the copies are already gone"))
		}
		// To the trash when there is one, so even a copy whose change the
		// stamp could not see — an edit deep inside a copied folder — can be
		// got back. Once the trash has refused them, a delete for good.
		op := fileops.OpDelete
		if !e.permanent && trash.Available() {
			op = fileops.OpTrash
		}
		return fileops.Job{Op: op, Srcs: srcs}, changed, nil

	case undoTrash:
		return fileops.Job{Op: fileops.OpRestore, Trash: e.trashed}, 0, nil
	}
	return fileops.Job{}, 0, fmt.Errorf("nothing to undo")
}

// movedOn is why an undo found nothing to do.
func movedOn(changed int, gone string) string {
	if changed > 0 {
		return fmt.Sprintf("%s changed since, and is left alone", countLabel(changed))
	}
	return gone
}

// spendUndo settles the top entry once its reversal has run. Usually that is
// the end of it, but some reversals that stopped part-way keep what they did
// not get to, and Ctrl+Z is still the way to finish them:
//
//   - one cancelled, which was a decision to stop, not to give up the rest;
//   - a copy's that the trash turned away, which is offered again as a delete
//     for good;
//   - a restore from the trash, where each item is tried on its own.
//
// Any other failure spends the entry. Kept, it would fail the same way every
// time and sit on top of the history blocking everything under it.
func (m *Model) spendUndo(res fileops.Result) {
	if res.Err == nil || len(m.undoStack) == 0 {
		m.popUndo()
		return
	}
	top := &m.undoStack[len(m.undoStack)-1]
	cancelled := errors.Is(res.Err, fileops.ErrCancelled)
	refused := errors.Is(res.Err, trash.ErrOtherFilesystem)

	switch {
	case top.kind == undoCopy && (cancelled || refused):
		// What the undo reached is gone; what is still there is still to do.
		left := top.created[:0:0]
		for _, p := range top.created {
			if localExists(p) {
				left = append(left, p)
			}
		}
		if len(left) > 0 {
			top.created = left
			top.permanent = top.permanent || refused
			return
		}
	case top.kind == undoMove && cancelled:
		left := top.moved[:0:0]
		for _, p := range top.moved {
			if localExists(p.Dst) && !localExists(p.Src) {
				left = append(left, p)
			}
		}
		if len(left) > 0 {
			top.moved = left
			return
		}
	case top.kind == undoRename && cancelled:
		return // the plan is made afresh from the names as they are now
	case top.kind == undoTrash:
		back := make(map[string]bool, len(res.Trashed))
		for _, it := range res.Trashed {
			back[it.Path] = true
		}
		left := top.trashed[:0:0]
		for _, it := range top.trashed {
			if !back[it.Path] {
				left = append(left, it)
			}
		}
		if len(left) > 0 {
			top.trashed = left
			top.label = "delete of " + trash.Label(left)
			return
		}
	}
	m.popUndo()
}

func (m *Model) popUndo() {
	if len(m.undoStack) > 0 {
		m.undoStack = m.undoStack[:len(m.undoStack)-1]
	}
}

// undoLabel is what the top of the history is, for the confirm prompt.
func (m Model) undoLabel() string {
	if len(m.undoStack) == 0 {
		return ""
	}
	return m.undoStack[len(m.undoStack)-1].label
}

func countLabel(n int) string {
	return fmt.Sprintf("%d %s", n, items(n))
}
