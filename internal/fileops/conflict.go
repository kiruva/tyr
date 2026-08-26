package fileops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A copy or a move that lands on a name already in use is the one moment a
// file manager cannot decide anything on its own: only the person watching
// knows whether the thing already there matters. So the job stops and asks —
// it emits a Conflict on its channel and waits on the reply channel inside it.
//
// The answer can be remembered ("…and all the rest"), which is what makes a
// hundred-file copy bearable, and a job started with a standing policy (a
// directory sync, say) never asks at all.

// ConflictAction is what to do about one destination that already exists.
type ConflictAction int

const (
	// ConflictAsk is the zero value: stop and ask the UI about every collision.
	ConflictAsk ConflictAction = iota
	ConflictOverwrite
	ConflictSkip
	// ConflictKeepBoth writes the incoming copy under a free name.
	ConflictKeepBoth
	// ConflictNewer overwrites only when the source is the newer of the two.
	ConflictNewer
	// ConflictCancel abandons the whole job.
	ConflictCancel
)

func (a ConflictAction) String() string {
	switch a {
	case ConflictOverwrite:
		return "overwrite"
	case ConflictSkip:
		return "skip"
	case ConflictKeepBoth:
		return "keep both"
	case ConflictNewer:
		return "overwrite if newer"
	case ConflictCancel:
		return "cancel"
	default:
		return "ask"
	}
}

// ErrCancelled is the error a job fails with when a conflict was answered with
// "cancel". It is not a failure of the filesystem, and the UI says so.
var ErrCancelled = errors.New("cancelled")

// Meta is the little about a file a person needs to choose between two of them.
type Meta struct {
	Size    int64
	ModTime time.Time
	IsDir   bool
	Missing bool // nothing is there (used for a source that vanished mid-job)
}

// Conflict is emitted when a destination exists and the job has no standing
// answer for it. Exactly one Resolution must be sent back on Reply.
type Conflict struct {
	Src, Dst string
	SrcInfo  Meta
	DstInfo  Meta
	Reply    chan<- Resolution
}

// Resolution answers one Conflict. All applies the same action to every
// remaining collision in the job.
type Resolution struct {
	Action ConflictAction
	All    bool
}

// SrcNewer reports whether the incoming file is the newer of the two, which is
// what the dialog highlights and what ConflictNewer decides on.
func (c Conflict) SrcNewer() bool {
	return c.SrcInfo.ModTime.After(c.DstInfo.ModTime)
}

// resolver answers collisions for one job: from the job's standing policy when
// it has one, from the UI when it does not, remembering an "all" answer.
type resolver struct {
	ctx      context.Context
	ch       chan<- any
	standing ConflictAction // ConflictAsk means "ask every time"
}

func newResolver(ctx context.Context, ch chan<- any, policy ConflictAction) *resolver {
	return &resolver{ctx: ctx, ch: ch, standing: policy}
}

// resolve decides what to do about dst, which is known to exist. The action it
// returns is always one of Overwrite, Skip or KeepBoth: "newer" is settled here
// against the two timestamps, and "cancel" comes back as ErrCancelled.
func (rv *resolver) resolve(src, dst string) (ConflictAction, error) {
	action := rv.standing
	if action == ConflictAsk {
		// Asking parks this goroutine on the UI. Both halves have to give way to
		// a cancellation: the question may never be read, and once it has been
		// read the answer may never come, if the user pressed Esc instead.
		reply := make(chan Resolution, 1)
		conflict := Conflict{
			Src: src, Dst: dst,
			SrcInfo: metaOf(src), DstInfo: metaOf(dst),
			Reply: reply,
		}
		select {
		case rv.ch <- conflict:
		case <-rv.ctx.Done():
			return ConflictCancel, rv.ctx.Err()
		}

		var answer Resolution
		select {
		case answer = <-reply:
		case <-rv.ctx.Done():
			return ConflictCancel, rv.ctx.Err()
		}
		action = answer.Action
		if answer.All && action != ConflictCancel {
			rv.standing = action
		}
	}

	switch action {
	case ConflictCancel:
		return ConflictCancel, ErrCancelled
	case ConflictNewer:
		if metaOf(src).ModTime.After(metaOf(dst).ModTime) {
			return ConflictOverwrite, nil
		}
		return ConflictSkip, nil
	case ConflictOverwrite, ConflictSkip, ConflictKeepBoth:
		return action, nil
	default:
		// An answer nothing understands is the safe one.
		return ConflictSkip, nil
	}
}

// metaOf describes a path, reporting Missing rather than an error: a conflict
// dialog that cannot stat one side still has something true to show.
func metaOf(path string) Meta {
	info, err := os.Lstat(path)
	if err != nil {
		return Meta{Missing: true}
	}
	return Meta{Size: info.Size(), ModTime: info.ModTime(), IsDir: info.IsDir()}
}

// freeName finds an unused name beside path: "notes.txt" becomes "notes (2).txt",
// then "notes (3).txt", and so on. The suffix goes before the extension so the
// file still opens in the same program.
func freeName(path string) (string, error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	ext := filepath.Ext(base)
	if strings.HasPrefix(base, ".") && ext == base {
		ext = "" // a dotfile is all "extension"; treat the whole thing as a stem
	}
	stem := strings.TrimSuffix(base, ext)

	for n := 2; n < 10000; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		if !exists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free name beside %s", base)
}
