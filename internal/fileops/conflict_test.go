package fileops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// answerWith drains a job's channel, answering every conflict the same way, and
// returns the Result it ends with along with how many times it was asked.
func answerWith(t *testing.T, ch <-chan Event, answer Resolution) (Result, int) {
	t.Helper()

	asked := 0
	for msg := range ch {
		switch v := msg.(type) {
		case Conflict:
			asked++
			v.Reply <- answer
		case Result:
			// Keep draining until the channel closes so the goroutine finishes.
			res := v
			for range ch {
			}
			return res, asked
		}
	}
	t.Fatal("the job produced no result")
	return Result{}, asked
}

// seed writes a file with known contents and a known age.
func seed(t *testing.T, path, body string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// A copy onto an existing name asks, and "overwrite" replaces what was there.
func TestCopyConflictOverwrite(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	seed(t, filepath.Join(src, "notes.txt"), "new", 0)
	seed(t, filepath.Join(dst, "notes.txt"), "old", time.Hour)

	res, asked := answerWith(t,
		Run(context.Background(), Job{Op: OpCopy, Srcs: []string{filepath.Join(src, "notes.txt")}, Dest: dst}),
		Resolution{Action: ConflictOverwrite})

	if res.Err != nil {
		t.Fatalf("copy: %v", res.Err)
	}
	if asked != 1 {
		t.Errorf("asked %d times, want once", asked)
	}
	if got := read(t, filepath.Join(dst, "notes.txt")); got != "new" {
		t.Errorf("destination holds %q, want the new contents", got)
	}
	if len(res.Created) != 0 {
		t.Errorf("Created = %v, want empty — overwriting created nothing", res.Created)
	}
}

// "Skip" leaves the destination alone, and the job still succeeds.
func TestCopyConflictSkip(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	seed(t, filepath.Join(src, "notes.txt"), "new", 0)
	seed(t, filepath.Join(dst, "notes.txt"), "old", time.Hour)

	res, _ := answerWith(t,
		Run(context.Background(), Job{Op: OpCopy, Srcs: []string{filepath.Join(src, "notes.txt")}, Dest: dst}),
		Resolution{Action: ConflictSkip})

	if res.Err != nil {
		t.Fatalf("copy: %v", res.Err)
	}
	if got := read(t, filepath.Join(dst, "notes.txt")); got != "old" {
		t.Errorf("destination holds %q, want it untouched", got)
	}
}

// "Keep both" puts the incoming file beside the one already there.
func TestCopyConflictKeepBoth(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	seed(t, filepath.Join(src, "notes.txt"), "new", 0)
	seed(t, filepath.Join(dst, "notes.txt"), "old", time.Hour)

	res, _ := answerWith(t,
		Run(context.Background(), Job{Op: OpCopy, Srcs: []string{filepath.Join(src, "notes.txt")}, Dest: dst}),
		Resolution{Action: ConflictKeepBoth})

	if res.Err != nil {
		t.Fatalf("copy: %v", res.Err)
	}
	if got := read(t, filepath.Join(dst, "notes.txt")); got != "old" {
		t.Errorf("the original holds %q, want it untouched", got)
	}
	kept := filepath.Join(dst, "notes (2).txt")
	if got := read(t, kept); got != "new" {
		t.Errorf("%s holds %q, want the new contents", kept, got)
	}
	if len(res.Created) != 1 || res.Created[0] != kept {
		t.Errorf("Created = %v, want [%s]", res.Created, kept)
	}
}

// "Overwrite if newer" compares the timestamps rather than the answer.
func TestCopyConflictNewer(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	seed(t, filepath.Join(src, "old.txt"), "src", time.Hour) // older than the target
	seed(t, filepath.Join(dst, "old.txt"), "dst", 0)
	seed(t, filepath.Join(src, "new.txt"), "src", 0) // newer than the target
	seed(t, filepath.Join(dst, "new.txt"), "dst", time.Hour)

	res, _ := answerWith(t,
		Run(context.Background(), Job{Op: OpCopy, Dest: dst, Srcs: []string{
			filepath.Join(src, "old.txt"),
			filepath.Join(src, "new.txt"),
		}}),
		Resolution{Action: ConflictNewer, All: true})

	if res.Err != nil {
		t.Fatalf("copy: %v", res.Err)
	}
	if got := read(t, filepath.Join(dst, "old.txt")); got != "dst" {
		t.Errorf("older source overwrote the target: %q", got)
	}
	if got := read(t, filepath.Join(dst, "new.txt")); got != "src" {
		t.Errorf("newer source did not overwrite the target: %q", got)
	}
}

// An "all" answer is given once and applied to the rest.
func TestConflictAllAnswersOnce(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	var srcs []string
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		seed(t, filepath.Join(src, name), "new", 0)
		seed(t, filepath.Join(dst, name), "old", time.Hour)
		srcs = append(srcs, filepath.Join(src, name))
	}

	res, asked := answerWith(t,
		Run(context.Background(), Job{Op: OpCopy, Srcs: srcs, Dest: dst}),
		Resolution{Action: ConflictOverwrite, All: true})

	if res.Err != nil {
		t.Fatalf("copy: %v", res.Err)
	}
	if asked != 1 {
		t.Fatalf("asked %d times, want once for the whole batch", asked)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if got := read(t, filepath.Join(dst, name)); got != "new" {
			t.Errorf("%s holds %q, want the new contents", name, got)
		}
	}
}

// Cancelling stops the job, and says it was cancelled rather than that it broke.
func TestConflictCancel(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	seed(t, filepath.Join(src, "a.txt"), "new", 0)
	seed(t, filepath.Join(dst, "a.txt"), "old", time.Hour)
	seed(t, filepath.Join(src, "b.txt"), "new", 0)

	res, _ := answerWith(t,
		Run(context.Background(), Job{Op: OpCopy, Dest: dst, Srcs: []string{
			filepath.Join(src, "a.txt"),
			filepath.Join(src, "b.txt"),
		}}),
		Resolution{Action: ConflictCancel})

	if !errors.Is(res.Err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", res.Err)
	}
	if got := read(t, filepath.Join(dst, "a.txt")); got != "old" {
		t.Errorf("the cancelled copy still wrote: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "b.txt")); err == nil {
		t.Error("the job carried on past the cancel")
	}
}

// Two directories merge instead of colliding; only the files inside them ask.
func TestCopyDirectoriesMerge(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	seed(t, filepath.Join(src, "docs", "same.txt"), "new", 0)
	seed(t, filepath.Join(src, "docs", "only-src.txt"), "new", 0)
	seed(t, filepath.Join(dst, "docs", "same.txt"), "old", time.Hour)
	seed(t, filepath.Join(dst, "docs", "only-dst.txt"), "old", time.Hour)

	res, asked := answerWith(t,
		Run(context.Background(), Job{Op: OpCopy, Srcs: []string{filepath.Join(src, "docs")}, Dest: dst}),
		Resolution{Action: ConflictOverwrite})

	if res.Err != nil {
		t.Fatalf("copy: %v", res.Err)
	}
	if asked != 1 {
		t.Errorf("asked %d times, want once — only the shared file collides", asked)
	}
	for name, want := range map[string]string{
		"same.txt":     "new",
		"only-src.txt": "new",
		"only-dst.txt": "old",
	} {
		if got := read(t, filepath.Join(dst, "docs", name)); got != want {
			t.Errorf("docs/%s holds %q, want %q", name, got, want)
		}
	}
}

// A move records where each source went, which is what undoing it needs.
func TestMoveRecordsWhatItMoved(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	seed(t, filepath.Join(src, "one.txt"), "one", 0)

	res, _ := answerWith(t,
		Run(context.Background(), Job{Op: OpMove, Srcs: []string{filepath.Join(src, "one.txt")}, Dest: dst}),
		Resolution{Action: ConflictSkip})

	if res.Err != nil {
		t.Fatalf("move: %v", res.Err)
	}
	want := Pair{Src: filepath.Join(src, "one.txt"), Dst: filepath.Join(dst, "one.txt")}
	if len(res.Moved) != 1 || res.Moved[0] != want {
		t.Fatalf("Moved = %v, want [%v]", res.Moved, want)
	}
	if _, err := os.Stat(want.Src); err == nil {
		t.Error("the source is still there after a move")
	}
}

// A job started with a policy never asks.
func TestPolicyJobDoesNotAsk(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	seed(t, filepath.Join(src, "a.txt"), "new", 0)
	seed(t, filepath.Join(dst, "a.txt"), "old", time.Hour)

	job := Job{
		Op:         OpSync,
		Pairs:      []Pair{{Src: filepath.Join(src, "a.txt"), Dst: filepath.Join(dst, "a.txt")}},
		OnConflict: ConflictOverwrite,
	}
	res, asked := answerWith(t, Run(context.Background(), job), Resolution{Action: ConflictCancel})

	if res.Err != nil {
		t.Fatalf("sync: %v", res.Err)
	}
	if asked != 0 {
		t.Errorf("asked %d times, want never", asked)
	}
	if got := read(t, filepath.Join(dst, "a.txt")); got != "new" {
		t.Errorf("destination holds %q, want the synced contents", got)
	}
}

func TestFreeName(t *testing.T) {
	dir := t.TempDir()
	seed(t, filepath.Join(dir, "notes.txt"), "x", 0)

	got, err := freeName(filepath.Join(dir, "notes.txt"))
	if err != nil {
		t.Fatalf("freeName: %v", err)
	}
	if want := filepath.Join(dir, "notes (2).txt"); got != want {
		t.Fatalf("freeName = %q, want %q", got, want)
	}

	seed(t, got, "x", 0)
	got, _ = freeName(filepath.Join(dir, "notes.txt"))
	if want := filepath.Join(dir, "notes (3).txt"); got != want {
		t.Fatalf("freeName = %q, want %q", got, want)
	}
}
