package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// leftovers is anything in dir besides the names given.
func leftovers(t *testing.T, dir string, want ...string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
outer:
	for _, e := range entries {
		for _, w := range want {
			if e.Name() == w {
				continue outer
			}
		}
		out = append(out, e.Name())
	}
	return out
}

func TestWriteFileReplacesAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("new"), 0o640); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := readFile(t, path); got != "new" {
		t.Errorf("contents = %q", got)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
			t.Errorf("mode = %v, want 0640", info.Mode().Perm())
		}
	}
	if extra := leftovers(t, dir, "f.txt"); len(extra) > 0 {
		t.Errorf("left behind: %v", extra)
	}
}

// A read-only result used to be impossible: the final mode went on the
// temporary file before it was opened for writing.
func TestWriteFileReadOnlyMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := WriteFile(path, []byte("x"), 0o444); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := readFile(t, path); got != "x" {
		t.Errorf("contents = %q", got)
	}
}

// A failure while writing leaves the original exactly as it was.
func TestReplaceFailureKeepsTheOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err := Replace(path, 0o644, func(f *os.File) error {
		_, _ = f.WriteString("half")
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if got := readFile(t, path); got != "original" {
		t.Errorf("contents = %q, want the original", got)
	}
	if extra := leftovers(t, dir, "f.txt"); len(extra) > 0 {
		t.Errorf("left behind: %v", extra)
	}
}

// Writing to a symlink writes the file it points at and keeps the link: a
// config file kept in a dotfiles repo stays where the link says it is.
func TestWriteFileFollowsALink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(real, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := WriteFile(link, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink == 0 {
		t.Error("the link was replaced by a file")
	}
	if got := readFile(t, real); got != "new" {
		t.Errorf("target = %q", got)
	}
}

func TestTempBesideAndCommit(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.bin")
	tmp, err := TempBeside(dst)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(tmp) != dir || !strings.Contains(filepath.Base(tmp), "out.bin") {
		t.Errorf("temp = %q, want it beside out.bin and recognisable", tmp)
	}
	if err := os.WriteFile(tmp, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Commit(tmp, dst, 0o444); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if got := readFile(t, dst); got != "data" {
		t.Errorf("contents = %q", got)
	}
	if extra := leftovers(t, dir, "out.bin"); len(extra) > 0 {
		t.Errorf("left behind: %v", extra)
	}
}

// A read-only file is replaced like any other; Windows needs coaxing for it.
func TestCommitOverAReadOnlyFile(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(dst, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(dst, []byte("new"), 0o444); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := readFile(t, dst); got != "new" {
		t.Errorf("contents = %q", got)
	}
}
