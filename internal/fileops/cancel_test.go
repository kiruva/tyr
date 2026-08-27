package fileops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// drain reads a job's channel to the end and returns the Result, failing if the
// channel closes without one or if the job outlives the deadline. Every test
// here is really asking "did this stop?", so a hang has to be a failure rather
// than a hung test binary.
func drain(t *testing.T, ch <-chan Event, within time.Duration) (Result, int) {
	t.Helper()
	deadline := time.After(within)
	steps := 0
	for {
		select {
		case v, ok := <-ch:
			if !ok {
				t.Fatal("channel closed without a Result")
			}
			switch msg := v.(type) {
			case Progress:
				steps++
			case Result:
				// The Result is last; the channel closes right after it.
				return msg, steps
			default:
				t.Fatalf("unexpected value on the channel: %T", msg)
			}
		case <-deadline:
			t.Fatalf("job did not finish within %s: it ignored the cancellation", within)
		}
	}
}

// tree writes n numbered files under dir and returns it.
func tree(t *testing.T, dir string, n int) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		name := filepath.Join(dir, "f"+strconv.Itoa(i)+".txt")
		if err := os.WriteFile(name, []byte("contents\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A context already cancelled must stop the job before it touches anything.
func TestRunStopsImmediatelyOnACancelledContext(t *testing.T) {
	root := t.TempDir()
	src := tree(t, filepath.Join(root, "src"), 50)
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, _ := drain(t, Run(ctx, Job{Op: OpCopy, Srcs: []string{src}, Dest: dst}), 10*time.Second)
	if !errors.Is(res.Err, ErrCancelled) {
		t.Fatalf("Err = %v, want ErrCancelled", res.Err)
	}

	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("copied %d entries despite starting cancelled", len(entries))
	}
}

// Cancelling part-way must stop the job, and what it had already done must still
// be reported — that is what the undo history is built from.
//
// The timing is not a race: the progress channel is unbuffered, so once this
// test has taken one value the job runs at most one more step before parking on
// the next send. Cancelling from here therefore always lands mid-batch.
//
// The sources are listed one by one rather than as their parent directory: a
// directory moved onto a name that is free is a single os.Rename, so a whole
// tree would go in one step with nothing to interrupt.
func TestRunCancelledMidwayKeepsWhatItDid(t *testing.T) {
	root := t.TempDir()
	src := tree(t, filepath.Join(root, "src"), 400)
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	srcs := make([]string, 0, len(entries))
	for _, e := range entries {
		srcs = append(srcs, filepath.Join(src, e.Name()))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := Run(ctx, Job{Op: OpMove, Srcs: srcs, Dest: dst})

	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before any progress")
		}
		if res, done := v.(Result); done {
			t.Fatalf("job finished before it could be cancelled: %v", res.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no progress from the job")
	}
	cancel()

	res, steps := drain(t, ch, 10*time.Second)
	if !errors.Is(res.Err, ErrCancelled) {
		t.Fatalf("Err = %v, want ErrCancelled", res.Err)
	}
	if len(res.Moved) == 0 {
		t.Fatal("Moved is empty: a cancelled job still has to report what it moved")
	}
	if len(res.Moved) == len(srcs) {
		t.Fatalf("Moved all %d sources: the cancellation did not stop anything", len(srcs))
	}
	// Every move it claims must really be on disk, and the source gone.
	for _, pair := range res.Moved {
		if _, err := os.Lstat(pair.Dst); err != nil {
			t.Errorf("Moved names %s, which is not there: %v", pair.Dst, err)
		}
		if _, err := os.Lstat(pair.Src); err == nil {
			t.Errorf("Moved names %s as moved, but it is still at the source", pair.Src)
		}
	}
	// Nothing may be lost in between: every source is either still at the
	// source or recorded as moved, never neither.
	moved := make(map[string]bool, len(res.Moved))
	for _, pair := range res.Moved {
		moved[pair.Src] = true
	}
	for _, s := range srcs {
		if _, err := os.Lstat(s); err != nil && !moved[s] {
			t.Errorf("%s is gone from the source and not recorded as moved", s)
		}
	}
	t.Logf("cancelled after %d of %d steps", steps, len(srcs))
}

// A cancellation must reach a job blocked on the UI's answer to a collision:
// nothing is going to reply, so waiting forever would hang the whole app.
func TestRunCancelledWhileWaitingOnAConflict(t *testing.T) {
	root := t.TempDir()
	src := tree(t, filepath.Join(root, "src"), 3)
	dst := tree(t, filepath.Join(root, "dst"), 3)

	// Same names on both sides, and no standing policy, so the first file asks.
	job := Job{Op: OpCopy, Srcs: []string{filepath.Join(src, "f0.txt")}, Dest: dst}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := Run(ctx, job)

	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before the conflict was asked")
		}
		if _, isConflict := v.(Conflict); !isConflict {
			t.Fatalf("got %T, want a Conflict", v)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no Conflict was emitted")
	}

	// Answer it by cancelling rather than replying, which is what pressing Esc
	// at the progress dialog does.
	cancel()

	res, _ := drain(t, ch, 10*time.Second)
	if !errors.Is(res.Err, ErrCancelled) {
		t.Fatalf("Err = %v, want ErrCancelled", res.Err)
	}
}

// A cancelled job must not leave its goroutine parked on a send nobody will
// read. Run's contract is that the Result always arrives and the channel then
// closes, however the job ended.
func TestRunAlwaysClosesItsChannel(t *testing.T) {
	root := t.TempDir()
	src := tree(t, filepath.Join(root, "src"), 200)
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch := Run(ctx, Job{Op: OpCopy, Srcs: []string{src}, Dest: dst})
	cancel()

	if _, _ = drain(t, ch, 10*time.Second); true {
		select {
		case _, ok := <-ch:
			if ok {
				t.Fatal("something followed the Result")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("channel never closed after the Result")
		}
	}
}

// An uncancelled job must be entirely unaffected by any of this.
func TestRunUncancelledStillCopiesEverything(t *testing.T) {
	root := t.TempDir()
	src := tree(t, filepath.Join(root, "src"), 25)
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	res, _ := drain(t, Run(context.Background(), Job{Op: OpCopy, Srcs: []string{src}, Dest: dst}), 30*time.Second)
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	entries, err := os.ReadDir(filepath.Join(dst, "src"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 25 {
		t.Errorf("copied %d entries, want 25", len(entries))
	}
}

// copyFile is where a single huge file would otherwise run to completion before
// anything got to ask whether it should still be running.
func TestCopyFileStopsPartWayThroughOneFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big")
	if err := os.WriteFile(src, make([]byte, 8<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dst := filepath.Join(dir, "copy")
	err := copyFile(ctx, src, dst, 0o644)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copyFile = %v, want context.Canceled", err)
	}
	if info, err := os.Stat(dst); err == nil && info.Size() == 8<<20 {
		t.Error("the whole file was copied despite the cancellation")
	}
}

// An archive tool is a child process: cancelling has to kill it, not wait
// politely for it to finish unpacking. This builds an archive big enough that
// the tool is certainly still running when the cancel lands.
func TestUnpackCancellationKillsTheTool(t *testing.T) {
	if tool("tar") == "" || tool("gzip") == "" {
		t.Skip("tar and gzip are needed to build the fixture")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	// Incompressible content, so gzip cannot collapse it to nothing and the
	// unpack has real work to do.
	blob := make([]byte, 4<<20)
	for i := range blob {
		blob[i] = byte(i * 2654435761 >> 13)
	}
	for i := range 40 {
		if err := os.WriteFile(filepath.Join(src, "b"+strconv.Itoa(i)), blob, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	archive := filepath.Join(root, "big.tar.gz")
	pk := drainResult(t, Run(context.Background(), Job{
		Op: OpPack, Srcs: []string{src}, Out: archive,
		Pack: PackOpts{Format: PackTarGz, Level: 1},
	}), 120*time.Second)
	if pk.Err != nil {
		t.Skipf("could not build the fixture: %v", pk.Err)
	}

	dest := filepath.Join(root, "out")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := Run(ctx, Job{Op: OpUnpack, Srcs: []string{archive}, Dest: dest})

	// Wait until the tool has actually started producing entries, then stop it.
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before the unpack started")
		}
		if res, done := v.(Result); done {
			t.Skipf("unpack finished too fast to cancel: %v", res.Err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("no progress from the unpack")
	}
	cancel()

	// The point of the test: this returns only because the child was killed.
	res, _ := drain(t, ch, 30*time.Second)
	if !errors.Is(res.Err, ErrCancelled) {
		t.Fatalf("Err = %v, want ErrCancelled", res.Err)
	}
}

// drainResult is drain without the step count, for the setup jobs.
func drainResult(t *testing.T, ch <-chan Event, within time.Duration) Result {
	t.Helper()
	res, _ := drain(t, ch, within)
	return res
}
