package find

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A search rooted at a symlink searches the folder it points at, and reports
// paths under the link as it was typed.
func TestFindFollowsSymlinkedRoot(t *testing.T) {
	real := tree(t, map[string]string{"a.txt": "hello"})
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}

	res, _, err := Run(context.Background(), Options{Root: link, Name: "a"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("results = %v, want a.txt", rels(res))
	}
	if want := filepath.Join(link, "a.txt"); res[0].Path != want {
		t.Errorf("path = %q, want %q", res[0].Path, want)
	}
}

// A line longer than the scanner holds — a minified bundle — does not end the
// search of the file: a match on a later line is still found, and so is one in
// the long line's first megabyte.
func TestFindContentPastAVeryLongLine(t *testing.T) {
	long := strings.Repeat("x", maxLineBytes+10)
	root := tree(t, map[string]string{
		"later.js": long + "\nthe needle\n",
		"early.js": "needle" + long + "\n",
	})

	res, _, err := Run(context.Background(), Options{Root: root, Content: "needle"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	got := map[string]int{}
	for _, r := range res {
		got[filepath.ToSlash(r.Rel)] = r.Line
	}
	if got["later.js"] != 2 {
		t.Errorf("later.js line = %d, want 2 (results %v)", got["later.js"], got)
	}
	if got["early.js"] != 1 {
		t.Errorf("early.js line = %d, want 1 (results %v)", got["early.js"], got)
	}
}

// A malformed glob is an error to show, not a search that silently matches
// nothing.
func TestFindBadGlob(t *testing.T) {
	root := tree(t, map[string]string{"abc.txt": "x"})
	_, _, err := Run(context.Background(), Options{Root: root, Name: "[abc"})
	if !errors.Is(err, filepath.ErrBadPattern) {
		t.Fatalf("err = %v, want filepath.ErrBadPattern", err)
	}
}
