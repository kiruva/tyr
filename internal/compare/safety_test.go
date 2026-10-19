package compare

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// caseInsensitive reports whether dir's volume finds a file under another case
// of its name, which is what macOS and Windows do by default and Linux does not.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbe")
	if err := os.WriteFile(probe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(probe) }()
	_, err := os.Lstat(filepath.Join(dir, "caseprobe"))
	return err == nil
}

// A file on one side where the other has a folder is not two one-sided rows to
// copy across: the copy would replace the folder, and everything in it, with
// the file. Both the name and what is under it come back as conflicts.
func TestWalkFileAgainstFolder(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	write(t, left, "foo", "left file", 0)
	write(t, right, "foo/precious.txt", "right data", 0)

	pairs, _, err := Walk(context.Background(), left, right, Options{Recursive: true})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	got := byRel(pairs)
	if p := got["foo"]; p.Kind != Conflict || p.Note == "" {
		t.Errorf("foo = %v %q, want a conflict that says why", p.Kind, p.Note)
	}
	if p := got["foo/precious.txt"]; p.Kind != Conflict || p.Note == "" {
		t.Errorf("foo/precious.txt = %v %q, want a conflict that says why", p.Kind, p.Note)
	}
}

// The same holds without recursion, where the folder itself is never indexed:
// the check is against what is on disk, not against what was walked.
func TestWalkFileAgainstFolderShallow(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	write(t, left, "foo", "left file", 0)
	write(t, right, "foo/precious.txt", "right data", 0)

	pairs, _, err := Walk(context.Background(), left, right, Options{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if p := byRel(pairs)["foo"]; p.Kind != Conflict {
		t.Errorf("foo = %v, want a conflict", p.Kind)
	}
}

// Two names that differ only in case are one file on a case-insensitive
// volume. Listed as two one-sided rows, each copy would overwrite the other's
// file and the newer one could be lost; they come back as one conflict.
func TestWalkCaseOnlyNames(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	if !caseInsensitive(t, right) {
		t.Skip("the volume is case-sensitive: Foo.txt and foo.txt are two files here")
	}
	write(t, left, "Foo.txt", "old left", time.Hour)
	write(t, right, "foo.txt", "NEW RIGHT", 0)

	pairs, _, err := Walk(context.Background(), left, right, Options{Recursive: true})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("pairs = %+v, want the two names as one row", pairs)
	}
	p := pairs[0]
	if p.Kind != Conflict || p.Left == nil || p.Right == nil || !strings.Contains(p.Note, "foo.txt") {
		t.Errorf("pair = %+v, want a conflict with both sides that names the other spelling", p)
	}
}

// A root reached through a symlink is compared as the folder it points at.
// Walking the link itself would find nothing under it.
func TestWalkFollowsSymlinkedRoot(t *testing.T) {
	dir := t.TempDir()
	real, other := filepath.Join(dir, "real"), filepath.Join(dir, "other")
	write(t, real, "a.txt", "hello", time.Hour)
	write(t, other, "a.txt", "hello", time.Hour)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}

	pairs, _, err := Walk(context.Background(), link, other, Options{Recursive: true})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if p := byRel(pairs)["a.txt"]; len(pairs) != 1 || p.Kind != Same {
		t.Errorf("pairs = %+v, want a.txt the same on both sides", pairs)
	}
}

// A one-sided file whose copy would land under a symlinked folder on the other
// side would be written wherever that link points, outside the tree being
// synced. It is a conflict, not a copy.
func TestWalkRefusesSymlinkedParent(t *testing.T) {
	left, right, outside := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, left, "sub/a.txt", "x", 0)
	if err := os.Symlink(outside, filepath.Join(right, "sub")); err != nil {
		t.Skip("no symlinks here:", err)
	}

	pairs, _, err := Walk(context.Background(), left, right, Options{Recursive: true})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if p := byRel(pairs)["sub/a.txt"]; p.Kind != Conflict || !strings.Contains(p.Note, "link") {
		t.Errorf("sub/a.txt = %v %q, want a conflict about the link", p.Kind, p.Note)
	}
}

// A byte-for-byte comparison of two big files stops when the walk is cancelled
// rather than reading both to the end.
func TestSameContentCancelled(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("x", 4*compareChunk)
	write(t, dir, "a", body, 0)
	write(t, dir, "b", body, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sameContent(ctx, filepath.Join(dir, "a"), filepath.Join(dir, "b")); err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
