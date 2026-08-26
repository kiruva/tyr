package app

import (
	"fmt"
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
			})
		}
	case fileops.OpMove:
		if len(res.Moved) > 0 {
			m.pushUndo(undoEntry{
				kind:  undoMove,
				label: fmt.Sprintf("move of %s", countLabel(len(res.Moved))),
				moved: res.Moved,
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

// openUndo builds the reverse of the most recent operation and asks to confirm
// it. An entry nothing can be done with is dropped rather than left at the top
// of the stack blocking the ones under it.
func (m *Model) openUndo() {
	if len(m.undoStack) == 0 {
		m.errText = "nothing to undo — the history is this session only"
		return
	}

	job, err := m.undoJob(m.undoStack[len(m.undoStack)-1])
	if err != nil {
		m.undoStack = m.undoStack[:len(m.undoStack)-1]
		m.errText = err.Error()
		return
	}

	m.pending = job
	m.undoing = true
	m.willOverwrite = false
	m.mode = modeConfirm
}

// undoJob is the job that reverses one entry.
func (m *Model) undoJob(e undoEntry) (fileops.Job, error) {
	switch e.kind {
	case undoRename:
		plan := rename.PlanUndo(e.root, e.changes)
		if rename.Summarize(plan).Renamed == 0 {
			return fileops.Job{}, fmt.Errorf("cannot undo that rename — those names have moved on since")
		}
		return fileops.Job{Op: fileops.OpRenameUndo, Dest: e.root, Renames: plan}, nil

	case undoMove:
		pairs := make([]fileops.Pair, 0, len(e.moved))
		for _, p := range e.moved {
			if !localExists(p.Dst) {
				continue // moved on again since; there is nothing to bring back
			}
			pairs = append(pairs, fileops.Pair{Src: p.Dst, Dst: p.Src})
		}
		if len(pairs) == 0 {
			return fileops.Job{}, fmt.Errorf("cannot undo that move — the files have moved on since")
		}
		return fileops.Job{Op: fileops.OpMovePairs, Pairs: pairs, Dest: filepath.Dir(pairs[0].Dst)}, nil

	case undoCopy:
		srcs := make([]string, 0, len(e.created))
		for _, p := range e.created {
			if localExists(p) {
				srcs = append(srcs, p)
			}
		}
		if len(srcs) == 0 {
			return fileops.Job{}, fmt.Errorf("cannot undo that copy — the copies are already gone")
		}
		return fileops.Job{Op: fileops.OpDelete, Srcs: srcs}, nil

	case undoTrash:
		return fileops.Job{Op: fileops.OpRestore, Trash: e.trashed}, nil
	}
	return fileops.Job{}, fmt.Errorf("nothing to undo")
}

// popUndo drops the entry whose reversal has just run.
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
