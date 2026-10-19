package app

import (
	"context"
	"sync/atomic"
)

// taskSeq numbers background runs across the whole app. It lives outside the
// Model on purpose: a tool's state is zeroed when it closes, and a counter kept
// in there would start again at 1 — the number a reply from before the close
// may still carry.
var taskSeq atomic.Uint64

// bgTask is the one run a tool has off the UI thread: what stops it, and the
// number its reply must carry to be accepted.
//
// Matching replies on anything else — the mode, the root — is not enough. A run
// cancelled and started again on the same root sends back a reply that looks
// exactly like the new run's, and would be shown as its result and clear the
// cancel the new run still needs.
type bgTask struct {
	seq    uint64
	cancel context.CancelFunc
}

// start stops whatever ran before and begins a new run. The returned seq goes
// into the run's reply, for finish to check.
func (t *bgTask) start() (context.Context, uint64) {
	t.stop()
	ctx, cancel := context.WithCancel(context.Background())
	t.seq, t.cancel = taskSeq.Add(1), cancel
	return ctx, t.seq
}

// stop cancels the run, if there is one. Its reply will no longer be accepted.
func (t *bgTask) stop() {
	if t.cancel != nil {
		t.cancel()
	}
	*t = bgTask{}
}

// finish reports whether a reply carrying seq is the run being waited for, and
// if so marks the run over. A reply that is not is to be dropped untouched.
func (t *bgTask) finish(seq uint64) bool {
	if seq == 0 || seq != t.seq {
		return false
	}
	t.stop() // the run is over; this only releases the context
	return true
}

// running reports whether a run is out.
func (t bgTask) running() bool { return t.cancel != nil }
