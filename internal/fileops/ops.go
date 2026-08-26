// Package fileops performs recursive copy/move/delete off the UI thread and
// streams progress over a channel. It has no knowledge of Bubble Tea — the app
// layer adapts the emitted values into messages.
package fileops

import (
	"context"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"

	"github.com/kiruva/tyr/internal/fsutil"
	"github.com/kiruva/tyr/internal/remote"
	"github.com/kiruva/tyr/internal/rename"
	"github.com/kiruva/tyr/internal/trash"
)

// Op identifies a filesystem operation.
type Op int

const (
	OpCopy Op = iota
	OpMove
	OpDelete
	OpPack         // create an archive (Out) from Srcs
	OpUnpack       // extract Srcs (archives) into Dest (the other pane)
	OpUnwrap       // extract Srcs (archives) in place (Dest = source dir)
	OpAddToArchive // add real files (Srcs) into archive Dest at VDir
	OpDownload     // copy remote Srcs into local dir Dest (over ssh)
	OpUpload       // copy local Srcs into remote dir Dest (over ssh)
	OpRemoteCopy   // copy/move Srcs to Dest within one host
	OpRemoteDelete // remove remote Srcs
	OpRename       // rename Renames in place, under Dest
	OpRenameUndo   // put an applied rename batch back
	OpTrash        // move Srcs to the desktop trash
	OpRestore      // put Trash items back where they came from
	OpSync         // copy every Pair, overwriting what is in the way
	OpMovePairs    // move every Pair, for putting a move back
	OpChmod        // change the mode of Srcs, optionally recursively
)

func (o Op) String() string {
	switch o {
	case OpCopy:
		return "Copy"
	case OpMove:
		return "Move"
	case OpDelete:
		return "Delete"
	case OpPack:
		return "Pack"
	case OpUnpack:
		return "Unpack"
	case OpUnwrap:
		return "Unpack here"
	case OpAddToArchive:
		return "Add to archive"
	case OpDownload:
		return "Download"
	case OpUpload:
		return "Upload"
	case OpRemoteCopy:
		return "Copy"
	case OpRemoteDelete:
		return "Delete"
	case OpRename:
		return "Rename"
	case OpRenameUndo:
		return "Undo rename"
	case OpTrash:
		return "Move to trash"
	case OpRestore:
		return "Restore"
	case OpSync:
		return "Synchronize"
	case OpMovePairs:
		return "Move back"
	case OpChmod:
		return "Change permissions"
	default:
		return "?"
	}
}

// Present returns the present-continuous form for progress titles.
func (o Op) Present() string {
	switch o {
	case OpCopy:
		return "Copying"
	case OpMove:
		return "Moving"
	case OpDelete:
		return "Deleting"
	case OpPack:
		return "Packing"
	case OpUnpack, OpUnwrap:
		return "Unpacking"
	case OpAddToArchive:
		return "Adding to archive"
	case OpDownload:
		return "Downloading"
	case OpUpload:
		return "Uploading"
	case OpRemoteCopy:
		return "Copying"
	case OpRemoteDelete:
		return "Deleting"
	case OpRename:
		return "Renaming"
	case OpRenameUndo:
		return "Undoing"
	case OpTrash:
		return "Moving to trash"
	case OpRestore:
		return "Restoring"
	case OpSync:
		return "Synchronizing"
	case OpMovePairs:
		return "Moving back"
	case OpChmod:
		return "Changing permissions"
	default:
		return "Working"
	}
}

// Job describes work to perform. Dest is the destination directory (ignored for
// OpDelete). Out is the archive path to create (OpPack only).
type Job struct {
	Op   Op
	Srcs []string // absolute source paths (archives for unpack/unwrap)
	Dest string   // destination directory, or archive path (add-to-archive)
	Out  string   // output archive path (pack)
	VDir string   // virtual directory within the archive (add-to-archive)
	Move bool     // delete the sources once the transfer succeeded

	// Pack is the format, level and password OpPack builds Out with.
	Pack PackOpts

	// Password decrypts an encrypted archive (OpUnpack / OpUnwrap). It is held
	// only for as long as the job runs and is never written anywhere.
	Password string

	// Renames is the batch for OpRename, or the inverted batch for OpRenameUndo,
	// relative to Dest.
	Renames []rename.Change

	// Host is the ssh destination for the remote ops; zero for local work.
	Host remote.Host

	// OnConflict is the standing answer for a destination that already exists.
	// The zero value asks the UI about every collision; a job with a policy
	// (a directory sync, an undo putting files back) never asks.
	OnConflict ConflictAction

	// Trash is the batch OpRestore puts back.
	Trash []trash.Item

	// Mode and Recursive are OpChmod's new permissions and how far they reach.
	Mode      os.FileMode
	Recursive bool

	// Pairs is explicit source→destination work, for the jobs that cannot be
	// described as "these sources into that directory": a sync copying both
	// ways, or an undo moving files back where they came from.
	Pairs []Pair
}

// Pair is one source and where it goes.
type Pair struct{ Src, Dst string }

// Progress is emitted repeatedly as the job runs.
type Progress struct {
	Current     string
	Done, Total int
}

// Result is emitted exactly once, last, when the job finishes.
type Result struct {
	Op  Op
	Err error

	// Renamed is what a rename job actually did, which is what an undo of it has
	// to be built from. It is set even when Err is: a batch that stopped part-way
	// still moved everything before the failure.
	Renamed []rename.Change

	// Created lists the top-level paths the job brought into being, and Moved
	// what it moved and to where. Both are what an undo of the job is built
	// from, and both are set even when the job failed part-way: whatever
	// happened before the failure still happened.
	Created []string
	Moved   []Pair

	// Trashed is what a delete put in the trash, in the order it went there.
	Trashed []trash.Item
}

// Run starts the job in a goroutine and returns a channel that yields zero or
// more Progress values followed by a single Result, then closes.
//
// Cancelling ctx stops the job: the recursive walkers check between entries, a
// long file copy checks between chunks, an external tool is killed, and an ssh
// session is closed under the command running on it. Whatever had already been
// done stays done and is still reported in the Result, so an undo of a
// cancelled job puts back exactly what it managed to move — the same contract
// as a job that failed part-way.
//
// The Result is always sent, cancelled or not: it is what tells the UI the job
// is over. Progress values are not, so a job being torn down cannot block on a
// reader that has stopped listening.
func Run(ctx context.Context, job Job) <-chan any {
	ch := make(chan any)
	go func() {
		defer close(ch)

		r := &reporter{ctx: ctx, ch: ch, total: computeTotal(ctx, job)}

		if isRemoteOp(job.Op) {
			ch <- Result{Op: job.Op, Err: r.cancelled(runRemote(ctx, job, r))}
			return
		}

		var (
			err     error
			renamed []rename.Change
		)
		switch job.Op {
		case OpPack:
			err = pack(ctx, job, r)
		case OpUnpack, OpUnwrap:
			err = extractAll(ctx, job, r)
		case OpAddToArchive:
			err = addToArchive(ctx, job, r)
		case OpRename:
			renamed, err = rename.Apply(ctx, job.Dest, job.Renames, r.step)
		case OpRenameUndo:
			renamed, err = rename.Undo(ctx, job.Dest, job.Renames, r.step)
		default:
			res := runLocal(ctx, job, r, ch)
			ch <- Result{
				Op: job.Op, Err: r.cancelled(res.err),
				Created: res.created, Moved: res.moved, Trashed: res.trashed,
			}
			return
		}
		ch <- Result{Op: job.Op, Err: r.cancelled(err), Renamed: renamed}
	}()
	return ch
}

// computeTotal estimates the number of progress steps for the job's bar. It
// walks the sources, so it takes ctx too: cancelling during the count returns
// early and the bar simply runs indeterminate for the moment the job then
// spends unwinding.
func computeTotal(ctx context.Context, job Job) int {
	total := 0
	switch job.Op {
	case OpRename, OpRenameUndo:
		return len(rename.Applicable(job.Renames))
	case OpDownload, OpRemoteCopy, OpRemoteDelete:
		// Counting remote items would cost another round trip; the bar runs
		// indeterminate instead.
		return 0
	case OpUpload:
		for _, s := range job.Srcs {
			total += countItems(ctx, s)
		}
	case OpUnpack, OpUnwrap:
		for _, a := range job.Srcs {
			total += countArchiveEntries(ctx, a)
		}
	case OpTrash:
		return len(job.Srcs)
	case OpRestore:
		return len(job.Trash)
	case OpSync, OpMovePairs:
		for _, pair := range job.Pairs {
			total += countItems(ctx, pair.Src)
		}
	case OpChmod:
		if !job.Recursive {
			return len(job.Srcs)
		}
		for _, s := range job.Srcs {
			total += countItems(ctx, s)
		}
	default:
		for _, s := range job.Srcs {
			total += countItems(ctx, s)
		}
	}
	return total
}

// reporter emits one Progress per processed item.
type reporter struct {
	ctx         context.Context
	ch          chan<- any
	done, total int
}

func (r *reporter) step(name string) {
	r.done++
	select {
	case r.ch <- Progress{Current: name, Done: r.done, Total: r.total}:
	case <-r.ctx.Done():
		// The job is being torn down and nobody needs this number any more.
		// Blocking on the send here is what would leak the goroutine.
	}
}

// cancelled maps whatever a torn-down job failed with onto ErrCancelled. A
// killed tool reports "signal: killed" and a half-written copy reports a closed
// file; neither is worth putting in front of someone who pressed Esc, and the
// UI already knows how to say "cancelled" from the collision prompt.
func (r *reporter) cancelled(err error) error {
	if err != nil && r.ctx.Err() != nil {
		return ErrCancelled
	}
	return err
}

func countItems(ctx context.Context, root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(_ string, _ iofs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err == nil {
			n++
		}
		return nil
	})
	if n == 0 {
		return 1
	}
	return n
}

// runner carries what a local copy or move needs through the recursion: where
// to report progress, who answers a collision, and when to give up.
type runner struct {
	ctx context.Context
	r   *reporter
	rv  *resolver
}

// localResult is what a local batch did, for the Result the job ends with.
type localResult struct {
	err     error
	created []string
	moved   []Pair
	trashed []trash.Item
}

// runLocal performs the copy / move / delete family, one source at a time. It
// records what it brought into being and what it moved, which is what an undo
// of the job is built from.
func runLocal(ctx context.Context, job Job, r *reporter, ch chan<- any) localResult {
	rn := &runner{ctx: ctx, r: r, rv: newResolver(ctx, ch, job.OnConflict)}
	var out localResult

	// Restoring works from the trash batch rather than from source paths: the
	// paths it puts back are the ones recorded when the delete happened.
	if job.Op == OpRestore {
		if out.err = trash.Restore(job.Trash); out.err == nil {
			for _, item := range job.Trash {
				r.step(item.Original)
			}
			out.trashed = job.Trash
		}
		return out
	}

	for _, src := range job.Srcs {
		// Between sources is the cheapest place to notice a cancellation, and
		// the one that leaves the least half-done.
		if out.err = ctx.Err(); out.err != nil {
			return out
		}
		switch job.Op {
		case OpCopy, OpMove:
			dst := filepath.Join(job.Dest, filepath.Base(src))
			existed := exists(dst)

			var (
				landed string
				err    error
			)
			if job.Op == OpCopy {
				landed, err = rn.copyPath(src, dst)
			} else {
				landed, err = rn.movePath(src, dst)
			}
			if landed != "" {
				if job.Op == OpMove {
					out.moved = append(out.moved, Pair{Src: src, Dst: landed})
				} else if landed != dst || !existed {
					out.created = append(out.created, landed)
				}
			}
			out.err = err

		case OpDelete:
			out.err = deletePath(ctx, src, r)

		case OpTrash:
			item, err := trash.Move(src)
			if err == nil {
				out.trashed = append(out.trashed, item)
				r.step(src)
			}
			out.err = err

		case OpChmod:
			out.err = chmodPath(ctx, src, job.Mode, job.Recursive, r)
		}
		if out.err != nil {
			return out
		}
	}

	// The pair-driven jobs work from Pairs rather than Srcs: a sync copying in
	// both directions, an undo putting files back where they came from.
	if job.Op == OpSync || job.Op == OpMovePairs {
		for _, pair := range job.Pairs {
			if err := ctx.Err(); err != nil {
				out.err = err
				return out
			}
			if err := os.MkdirAll(filepath.Dir(pair.Dst), 0o755); err != nil {
				out.err = err
				return out
			}
			var err error
			if job.Op == OpSync {
				_, err = rn.copyPath(pair.Src, pair.Dst)
			} else {
				_, err = rn.movePath(pair.Src, pair.Dst)
			}
			if err != nil {
				out.err = err
				return out
			}
		}
	}
	return out
}

// copyPath copies src to dst, asking about a collision unless both sides are
// directories — those merge, which is what dragging one folder onto another has
// always meant. It returns where the copy landed, or "" when it was skipped.
func (rn *runner) copyPath(src, dst string) (string, error) {
	if err := rn.ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return "", err
	}

	if exists(dst) && (!info.IsDir() || !isDir(dst)) {
		action, err := rn.rv.resolve(src, dst)
		if err != nil {
			return "", err
		}
		switch action {
		case ConflictSkip:
			return "", nil
		case ConflictKeepBoth:
			free, err := freeName(dst)
			if err != nil {
				return "", err
			}
			dst = free
		case ConflictOverwrite:
			if err := os.RemoveAll(dst); err != nil {
				return "", err
			}
		}
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return "", err
		}
		_ = os.Remove(dst)
		if err := os.Symlink(target, dst); err != nil {
			return "", err
		}
		rn.r.step(src)
		return dst, nil

	case info.IsDir():
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return "", err
		}
		rn.r.step(src)
		entries, err := os.ReadDir(src)
		if err != nil {
			return "", err
		}
		for _, e := range entries {
			if _, err := rn.copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return "", err
			}
		}
		return dst, nil

	default:
		if err := copyFile(rn.ctx, src, dst, info.Mode().Perm()); err != nil {
			return "", err
		}
		rn.r.step(src)
		return dst, nil
	}
}

// copyFile copies one file, in chunks so that a cancellation is noticed part-way
// through a large one. A plain io.Copy would run to the end of a hundred-gigabyte
// file before anything got to ask whether it should still be running.
func copyFile(ctx context.Context, src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, &ctxReader{ctx: ctx, r: in}); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ctxReader fails the read once ctx is done, which is what stops an io.Copy
// mid-file. io.Copy asks for 32KB at a time, so that is the granularity a
// cancellation is noticed at.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// movePath moves src onto dst. Two directories merge, entry by entry, so a
// collision inside one is still answered file by file; anything else is a
// collision in its own right. It returns where the move landed, or "" when it
// was skipped.
func (rn *runner) movePath(src, dst string) (string, error) {
	if err := rn.ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return "", err
	}

	if exists(dst) {
		if info.IsDir() && isDir(dst) {
			entries, err := os.ReadDir(src)
			if err != nil {
				return "", err
			}
			for _, e := range entries {
				if _, err := rn.movePath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
					return "", err
				}
			}
			// Whatever was skipped is still in there, so this only succeeds when
			// the source really is empty now.
			_ = os.Remove(src)
			return dst, nil
		}

		action, err := rn.rv.resolve(src, dst)
		if err != nil {
			return "", err
		}
		switch action {
		case ConflictSkip:
			return "", nil
		case ConflictKeepBoth:
			free, err := freeName(dst)
			if err != nil {
				return "", err
			}
			dst = free
		case ConflictOverwrite:
			if err := os.RemoveAll(dst); err != nil {
				return "", err
			}
		}
	}

	// Fast path: same filesystem → atomic rename.
	if err := os.Rename(src, dst); err == nil {
		rn.r.step(src)
		return dst, nil
	} else if !fsutil.CrossDevice(err) {
		return "", err
	}
	// Cross-device: copy then remove the original.
	if _, err := rn.copyPath(src, dst); err != nil {
		return "", err
	}
	return dst, os.RemoveAll(src)
}

// isDir reports whether path is a directory (following no symlinks).
func isDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

func deletePath(ctx context.Context, src string, r *reporter) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := deletePath(ctx, filepath.Join(src, e.Name()), r); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(src); err != nil {
		return err
	}
	r.step(src)
	return nil
}

// chmodPath changes one path's permissions, walking into it when asked. A
// directory that cannot be read stops the walk with its error rather than
// leaving half the tree changed silently.
func chmodPath(ctx context.Context, path string, mode os.FileMode, recursive bool, r *reporter) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	// A symlink has no permissions of its own worth changing; chmod would follow
	// it and change something the user did not point at.
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}

	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	r.step(path)

	if !recursive || !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := chmodPath(ctx, filepath.Join(path, e.Name()), mode, recursive, r); err != nil {
			return err
		}
	}
	return nil
}
