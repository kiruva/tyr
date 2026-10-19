package fileops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// run runs a job to its end, answering every conflict with answer.
func run(t *testing.T, job Job, answer ConflictAction) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, _ := answerWith(t, Run(ctx, job), Resolution{Action: answer})
	return res
}

func exist(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// A file reached through a symlinked folder is the same file as the one in the
// folder itself. Copying or moving it onto itself and answering "overwrite"
// deleted it; it is refused instead, and the file is still there.
func TestCopyOrMoveOntoItselfIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	for _, op := range []Op{OpCopy, OpMove} {
		dir := t.TempDir()
		real := filepath.Join(dir, "real")
		seed(t, filepath.Join(real, "f.txt"), "precious", 0)
		link := filepath.Join(dir, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}

		res := run(t, Job{Op: op, Srcs: []string{filepath.Join(link, "f.txt")}, Dest: real}, ConflictOverwrite)
		if !errors.Is(res.Err, ErrSameFile) {
			t.Errorf("%v: err = %v, want ErrSameFile", op, res.Err)
		}
		if got := read(t, filepath.Join(real, "f.txt")); got != "precious" {
			t.Errorf("%v: file now holds %q", op, got)
		}
	}
}

// A folder copied into its own subfolder would copy itself without end.
func TestCopyIntoItselfIsRefused(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "proj")
	seed(t, filepath.Join(src, "sub", "a.txt"), "x", 0)

	for _, dest := range []string{src, filepath.Join(src, "sub")} {
		res := run(t, Job{Op: OpCopy, Srcs: []string{src}, Dest: dest}, ConflictOverwrite)
		if res.Err == nil || !strings.Contains(res.Err.Error(), "inside itself") {
			t.Errorf("copy into %s: err = %v", dest, res.Err)
		}
	}
	if exist(filepath.Join(src, "sub", "proj")) {
		t.Error("the copy started anyway")
	}
}

// A read-only folder copies with its contents, and ends up read-only.
func TestCopyReadOnlyFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("folder modes are not enforced on Windows")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "ro")
	seed(t, filepath.Join(src, "a.txt"), "x", 0)
	if err := os.Chmod(src, 0o555); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(src, 0o755)
		_ = os.Chmod(filepath.Join(dest, "ro"), 0o755)
	})

	res := run(t, Job{Op: OpCopy, Srcs: []string{src}, Dest: dest}, ConflictOverwrite)
	if res.Err != nil {
		t.Fatalf("copy: %v", res.Err)
	}
	if read(t, filepath.Join(dest, "ro", "a.txt")) != "x" {
		t.Error("the contents did not arrive")
	}
	info, _ := os.Stat(filepath.Join(dest, "ro"))
	if info.Mode().Perm() != 0o555 {
		t.Errorf("mode = %v, want the source's 0555", info.Mode().Perm())
	}
}

// A copy keeps its original's modification time, for files and folders. One
// stamped with the moment it was made looks newer than its source, and the
// next sync would offer to copy everything back.
func TestCopyKeepsModTimes(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	seed(t, filepath.Join(src, "a.txt"), "x", 48*time.Hour)
	old := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	if res := run(t, Job{Op: OpCopy, Srcs: []string{src}, Dest: dest}, ConflictOverwrite); res.Err != nil {
		t.Fatalf("copy: %v", res.Err)
	}
	for _, rel := range []string{"src", filepath.Join("src", "a.txt")} {
		a, _ := os.Stat(filepath.Join(dir, rel))
		b, _ := os.Stat(filepath.Join(dest, rel))
		if !a.ModTime().Equal(b.ModTime()) {
			t.Errorf("%s: copy dated %v, original %v", rel, b.ModTime(), a.ModTime())
		}
	}
}

// Moving a folder into one of the same name merges them, and the undo record
// is what arrived — not the folder, which would take what was already there
// back with it.
func TestMergeMoveRecordsWhatArrived(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "A", "photos"), filepath.Join(dir, "B", "photos")
	seed(t, filepath.Join(a, "new.jpg"), "new", 0)
	seed(t, filepath.Join(b, "old.jpg"), "old", 0)

	res := run(t, Job{Op: OpMove, Srcs: []string{a}, Dest: filepath.Join(dir, "B")}, ConflictOverwrite)
	if res.Err != nil {
		t.Fatalf("move: %v", res.Err)
	}
	want := Pair{Src: filepath.Join(a, "new.jpg"), Dst: filepath.Join(b, "new.jpg")}
	if len(res.Moved) != 1 || res.Moved[0] != want {
		t.Fatalf("moved = %v, want only %v", res.Moved, want)
	}

	// The undo, as the app builds it from Moved.
	undo := Job{Op: OpMovePairs, Pairs: []Pair{{Src: want.Dst, Dst: want.Src}}}
	if res := run(t, undo, ConflictAsk); res.Err != nil {
		t.Fatalf("undo: %v", res.Err)
	}
	if !exist(filepath.Join(b, "old.jpg")) || exist(filepath.Join(a, "old.jpg")) {
		t.Error("undo moved old.jpg, which was in B before the move")
	}
	if !exist(filepath.Join(a, "new.jpg")) {
		t.Error("undo did not put new.jpg back")
	}
}

// Copying into a folder that is already there records what the copy added, so
// an undo can take it away; before, nothing at all was recorded.
func TestMergeCopyRecordsWhatArrived(t *testing.T) {
	dir := t.TempDir()
	seed(t, filepath.Join(dir, "A", "photos", "new.jpg"), "new", 0)
	seed(t, filepath.Join(dir, "B", "photos", "old.jpg"), "old", 0)

	res := run(t, Job{Op: OpCopy, Srcs: []string{filepath.Join(dir, "A", "photos")}, Dest: filepath.Join(dir, "B")}, ConflictOverwrite)
	if res.Err != nil {
		t.Fatalf("copy: %v", res.Err)
	}
	want := filepath.Join(dir, "B", "photos", "new.jpg")
	if len(res.Created) != 1 || res.Created[0] != want {
		t.Fatalf("created = %v, want only %s", res.Created, want)
	}
}

// A folder copied whole is one entry in the record, not one per file in it.
func TestCopyRecordsTheTopOfANewTree(t *testing.T) {
	dir := t.TempDir()
	seed(t, filepath.Join(dir, "src", "a", "b.txt"), "x", 0)
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	res := run(t, Job{Op: OpCopy, Srcs: []string{filepath.Join(dir, "src")}, Dest: dest}, ConflictOverwrite)
	if res.Err != nil || len(res.Created) != 1 || res.Created[0] != filepath.Join(dest, "src") {
		t.Fatalf("created = %v (err %v), want just the folder", res.Created, res.Err)
	}
}

// Unpacking onto a name already taken asks, like a copy does. Every extractor
// overwrites unasked, so an edited notes.txt was replaced by the archived one.
func TestUnpackAsksBeforeReplacing(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("no tar on PATH")
	}
	dir := t.TempDir()
	content := filepath.Join(dir, "content")
	seed(t, filepath.Join(content, "notes.txt"), "archived", 0)
	seed(t, filepath.Join(content, "new.txt"), "new", 0)
	arc := filepath.Join(dir, "a.tar")
	if out, err := exec.Command("tar", "-C", content, "-cf", arc, "notes.txt", "new.txt").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}

	for _, tt := range []struct {
		answer ConflictAction
		want   string
	}{
		{ConflictSkip, "edited"},
		{ConflictOverwrite, "archived"},
	} {
		dest := filepath.Join(dir, "dest-"+tt.want)
		seed(t, filepath.Join(dest, "notes.txt"), "edited", 0)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		res, asked := answerWith(t, Run(ctx, Job{Op: OpUnpack, Srcs: []string{arc}, Dest: dest}), Resolution{Action: tt.answer})
		cancel()
		if res.Err != nil {
			t.Fatalf("unpack: %v", res.Err)
		}
		if asked != 1 {
			t.Errorf("asked %d times, want once for notes.txt", asked)
		}
		if got := read(t, filepath.Join(dest, "notes.txt")); got != tt.want {
			t.Errorf("answered %v: notes.txt holds %q, want %q", tt.answer, got, tt.want)
		}
		if read(t, filepath.Join(dest, "new.txt")) != "new" {
			t.Error("the name that was free did not arrive")
		}
		entries, _ := os.ReadDir(dest)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".tyr-unpack-") {
				t.Errorf("the staging folder %s was left behind", e.Name())
			}
		}
	}
}
