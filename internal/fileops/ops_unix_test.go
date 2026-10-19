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
