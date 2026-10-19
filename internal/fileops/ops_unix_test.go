//go:build !windows

package fileops

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A named pipe has nothing to copy, and opening one to read it waits for a
// writer that never comes: the job hung past Esc. It is left out and said so.
func TestCopySkipsSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	seed(t, filepath.Join(src, "a.txt"), "x", 0)
	if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644); err != nil {
		t.Skip(err)
	}
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	res := run(t, Job{Op: OpCopy, Srcs: []string{src}, Dest: dest}, ConflictOverwrite)
	if res.Err != nil {
		t.Fatalf("copy: %v", res.Err)
	}
	if !exist(filepath.Join(dest, "src", "a.txt")) || exist(filepath.Join(dest, "src", "pipe")) {
		t.Error("want a.txt copied and the pipe left out")
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "pipe") {
		t.Errorf("notes = %q, want the pipe named", res.Notes)
	}
}

// A recursive change is chmod -R with X: folders get execute wherever they get
// read, so they can still be opened, and a file keeps execute only if it was a
// program already. "644 on the tree" used to close every folder in it — and
// the walk then could not read the folder it had just closed.
func TestChmodRecursiveKeepsFoldersOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a closed folder anyway")
	}
	dir := t.TempDir()
	top := filepath.Join(dir, "tree")
	seed(t, filepath.Join(top, "inner", "notes.txt"), "x", 0)
	seed(t, filepath.Join(top, "inner", "run.sh"), "x", 0)
	if err := os.Chmod(filepath.Join(top, "inner", "run.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { // should a failure leave a folder closed
		_ = os.Chmod(top, 0o755)
		_ = os.Chmod(filepath.Join(top, "inner"), 0o755)
	})

	tests := []struct {
		mode                 os.FileMode
		dirs, files, scripts os.FileMode
	}{
		{0o644, 0o755, 0o644, 0o755},
		{0o755, 0o755, 0o644, 0o755},
		{0o600, 0o700, 0o600, 0o700},
		{0o640, 0o750, 0o640, 0o750},
	}
	for _, tt := range tests {
		res := run(t, Job{Op: OpChmod, Srcs: []string{top}, Mode: tt.mode, Recursive: true}, ConflictSkip)
		if res.Err != nil {
			t.Fatalf("%o: %v", tt.mode, res.Err)
		}
		for path, want := range map[string]os.FileMode{
			top:                                      tt.dirs,
			filepath.Join(top, "inner"):              tt.dirs,
			filepath.Join(top, "inner", "notes.txt"): tt.files,
			filepath.Join(top, "inner", "run.sh"):    tt.scripts,
		} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != want {
				t.Errorf("%o: %s = %o, want %o", tt.mode, filepath.Base(path), got, want)
			}
		}
	}
}

// A change that closes the folders still reaches everything inside them: the
// walk reads each folder before it closes it.
func TestChmodRecursiveCanCloseFolders(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a closed folder anyway")
	}
	dir := t.TempDir()
	top := filepath.Join(dir, "tree")
	nested := filepath.Join(top, "inner", "notes.txt")
	seed(t, nested, "x", 0)
	t.Cleanup(func() {
		_ = os.Chmod(top, 0o755)
		_ = os.Chmod(filepath.Join(top, "inner"), 0o755)
	})

	res := run(t, Job{Op: OpChmod, Srcs: []string{top}, Mode: 0o200, Recursive: true}, ConflictSkip)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	_ = os.Chmod(top, 0o755)
	_ = os.Chmod(filepath.Join(top, "inner"), 0o755)
	info, err := os.Stat(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o200 {
		t.Errorf("nested file = %o, want 200", got)
	}
}
