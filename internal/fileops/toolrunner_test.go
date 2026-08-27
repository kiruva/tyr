package fileops

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeTools stands in for the archive programs. It records what it was asked to
// run and answers from a script, which is what makes the orchestration testable
// on a machine with none of the real tools installed.
type fakeTools struct {
	ran []toolCmd

	// err, when set, is what every run returns.
	err error
	// emit is written as the tool's progress, one step per entry.
	emit []string
}

func (f *fakeTools) runTool(_ context.Context, c toolCmd, r *reporter, _ func(string) string) error {
	f.ran = append(f.ran, c)
	for _, name := range f.emit {
		r.step(name)
	}
	return f.err
}

func (f *fakeTools) runPiped(_ context.Context, src, filter toolCmd, _ io.Writer, r *reporter, _ func(string) string) error {
	f.ran = append(f.ran, src, filter)
	for _, name := range f.emit {
		r.step(name)
	}
	return f.err
}

// withTools runs a job against fake tools and returns the Result.
func withTools(t *testing.T, f *fakeTools, job Job) Result {
	t.Helper()
	ch := make(chan Event)
	ctx := context.Background()
	r := &reporter{ctx: ctx, ch: ch, tools: f}

	done := make(chan Result, 1)
	go func() {
		var err error
		switch job.Op {
		case OpPack:
			err = pack(ctx, job, r)
		case OpUnpack, OpUnwrap:
			err = extractAll(ctx, job, r)
		default:
			t.Errorf("withTools does not drive %v", job.Op)
		}
		done <- Result{Op: job.Op, Err: err}
		close(ch)
	}()

	for range ch { // drain progress so the job is never blocked on a send
	}
	select {
	case res := <-done:
		return res
	case <-time.After(10 * time.Second):
		t.Fatal("job never finished")
		return Result{}
	}
}

// argsOf renders a recorded command the way a shell would show it, for
// readable failures.
func argsOf(c toolCmd) string { return c.bin + " " + strings.Join(c.args, " ") }

// Unpacking several archives runs the tool once per archive, in order.
func TestExtractAllRunsOncePerArchive(t *testing.T) {
	dir := t.TempDir()
	f := &fakeTools{}

	res := withTools(t, f, Job{
		Op:   OpUnpack,
		Srcs: []string{"/a/one.tar", "/a/two.tar", "/a/three.tar"},
		Dest: filepath.Join(dir, "out"),
	})
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if len(f.ran) != 3 {
		t.Fatalf("ran %d tools, want 3", len(f.ran))
	}
	for i, want := range []string{"one.tar", "two.tar", "three.tar"} {
		if !strings.Contains(argsOf(f.ran[i]), want) {
			t.Errorf("run %d = %q, want it to name %s", i, argsOf(f.ran[i]), want)
		}
	}
}

// The first archive that fails stops the batch: the rest are not attempted.
func TestExtractAllStopsAtTheFirstFailure(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("tar: not a tar archive")
	f := &fakeTools{err: boom}

	res := withTools(t, f, Job{
		Op:   OpUnpack,
		Srcs: []string{"/a/one.tar", "/a/two.tar", "/a/three.tar"},
		Dest: filepath.Join(dir, "out"),
	})
	if !errors.Is(res.Err, boom) {
		t.Fatalf("Err = %v, want the tool's error", res.Err)
	}
	if len(f.ran) != 1 {
		t.Errorf("ran %d tools, want 1: the batch should stop at the first failure", len(f.ran))
	}
}

// An encrypted archive comes back as ErrNeedPassword so the app knows to ask,
// rather than as the tool's own wording.
func TestExtractPropagatesErrNeedPassword(t *testing.T) {
	dir := t.TempDir()
	f := &fakeTools{err: ErrNeedPassword}

	res := withTools(t, f, Job{
		Op:   OpUnpack,
		Srcs: []string{"/a/secret.tar"},
		Dest: filepath.Join(dir, "out"),
	})
	if !errors.Is(res.Err, ErrNeedPassword) {
		t.Fatalf("Err = %v, want ErrNeedPassword", res.Err)
	}
}

// A tar.gz is written by piping tar through the compressor, and the level the
// dialog chose is what the compressor is actually given.
func TestPackTarGzPipesThroughTheCompressorAtTheChosenLevel(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &fakeTools{}

	res := withTools(t, f, Job{
		Op: OpPack, Srcs: []string{src}, Out: filepath.Join(dir, "out.tar.gz"),
		Pack: PackOpts{Format: PackTarGz, Level: 9},
	})
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if len(f.ran) != 2 {
		t.Fatalf("ran %d commands, want 2 (tar and the compressor)", len(f.ran))
	}
	if f.ran[0].bin != "tar" {
		t.Errorf("first command is %q, want tar", f.ran[0].bin)
	}
	if f.ran[1].bin != "gzip" {
		t.Errorf("compressor is %q, want gzip", f.ran[1].bin)
	}
	if !strings.Contains(argsOf(f.ran[1]), "-9") {
		t.Errorf("compressor args = %q, want level 9", argsOf(f.ran[1]))
	}
}

// Plain tar has no compressor, so there is nothing to pipe through.
func TestPackPlainTarRunsOneCommand(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &fakeTools{}

	if res := withTools(t, f, Job{
		Op: OpPack, Srcs: []string{src}, Out: filepath.Join(dir, "out.tar"),
		Pack: PackOpts{Format: PackTar},
	}); res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if len(f.ran) != 1 || f.ran[0].bin != "tar" {
		t.Fatalf("ran %d commands (%v), want one tar", len(f.ran), f.ran)
	}
}

// A failed pack leaves a truncated archive behind, which is worse than no
// archive at all: it must be removed.
func TestFailedPackRemovesTheTruncatedArchive(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.tar")
	f := &fakeTools{err: errors.New("tar: disk full")}

	if res := withTools(t, f, Job{
		Op: OpPack, Srcs: []string{src}, Out: out,
		Pack: PackOpts{Format: PackTar},
	}); res.Err == nil {
		t.Fatal("Err = nil, want the tool's error")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("the truncated archive was left behind")
	}
}

// ...but an archive that was already there is not deleted by a failed attempt
// to replace it. Losing the old one would be worse than the failure.
func TestFailedPackKeepsAnArchiveThatWasAlreadyThere(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.tar")
	if err := os.WriteFile(out, []byte("the previous archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeTools{err: errors.New("tar: disk full")}

	if res := withTools(t, f, Job{
		Op: OpPack, Srcs: []string{src}, Out: out,
		Pack: PackOpts{Format: PackTar},
	}); res.Err == nil {
		t.Fatal("Err = nil, want the tool's error")
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("the archive that was already there was removed: %v", err)
	}
}

// Packing a format that cannot be encrypted with a password set is refused
// before any tool runs, rather than quietly producing an unprotected archive.
func TestPackRefusesAPasswordTheFormatCannotHonour(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &fakeTools{}

	res := withTools(t, f, Job{
		Op: OpPack, Srcs: []string{src}, Out: filepath.Join(dir, "out.tar.gz"),
		Pack: PackOpts{Format: PackTarGz, Level: 6, Password: "hunter2"},
	})
	if res.Err == nil {
		t.Fatal("Err = nil, want a refusal")
	}
	if len(f.ran) != 0 {
		t.Errorf("ran %d commands, want none: the refusal comes first", len(f.ran))
	}
}

// The progress a tool reports reaches the job's channel.
func TestToolProgressReachesTheChannel(t *testing.T) {
	dir := t.TempDir()
	f := &fakeTools{emit: []string{"a.txt", "b.txt", "c.txt"}}

	ch := make(chan Event, 16)
	ctx := context.Background()
	r := &reporter{ctx: ctx, ch: ch, tools: f}
	if err := extractAll(ctx, Job{Op: OpUnpack, Srcs: []string{"/a/one.tar"}, Dest: filepath.Join(dir, "out")}, r); err != nil {
		t.Fatal(err)
	}
	close(ch)

	var names []string
	for ev := range ch {
		if p, ok := ev.(Progress); ok {
			names = append(names, p.Current)
		}
	}
	if strings.Join(names, ",") != "a.txt,b.txt,c.txt" {
		t.Errorf("progress = %v, want the three names the tool reported", names)
	}
}

// execTools is what production uses; the interface must not drift from it.
var _ toolRunner = execTools{}
var _ toolRunner = (*fakeTools)(nil)
