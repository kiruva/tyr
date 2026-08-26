package compare

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// write puts a file with known contents and a known age into a tree.
func write(t *testing.T, root, rel, body string, age time.Duration) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(full, when, when); err != nil {
		t.Fatal(err)
	}
}

// byRel indexes the result for assertions.
func byRel(pairs []Pair) map[string]Pair {
	out := make(map[string]Pair, len(pairs))
	for _, p := range pairs {
		out[p.Rel] = p
	}
	return out
}

func TestWalkClassifies(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()

	write(t, left, "same.txt", "identical", time.Hour)
	write(t, right, "same.txt", "identical", time.Hour)

	write(t, left, "newer-left.txt", "left version", 0)
	write(t, right, "newer-left.txt", "right version is longer", time.Hour)

	write(t, left, "only-left.txt", "x", 0)
	write(t, right, "only-right.txt", "x", 0)

	pairs, truncated, err := Walk(context.Background(), left, right, Options{Recursive: true})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if truncated {
		t.Error("truncated on a four-file comparison")
	}

	got := byRel(pairs)
	if p := got["same.txt"]; p.Kind != Same {
		t.Errorf("same.txt = %v, want Same", p.Kind)
	}
	if p := got["newer-left.txt"]; p.Kind != Differs || p.Newer != Left {
		t.Errorf("newer-left.txt = %v newer:%v, want Differs newer:Left", p.Kind, p.Newer)
	}
	if p := got["only-left.txt"]; p.Kind != OnlyLeft || p.Right != nil {
		t.Errorf("only-left.txt = %v, want OnlyLeft with no right side", p.Kind)
	}
	if p := got["only-right.txt"]; p.Kind != OnlyRight || p.Left != nil {
		t.Errorf("only-right.txt = %v, want OnlyRight with no left side", p.Kind)
	}
}

// Same size and a timestamp within the tolerance is "the same file".
func TestWalkTreatsCloseTimesAsSame(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	write(t, left, "a.txt", "12345", 0)
	write(t, right, "a.txt", "12345", time.Second)

	pairs, _, err := Walk(context.Background(), left, right, Options{Recursive: true})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if p := byRel(pairs)["a.txt"]; p.Kind != Same {
		t.Errorf("a.txt = %v, want Same", p.Kind)
	}
}

// By content, two same-size files with different bytes differ however their
// timestamps line up — and two identical ones match however far apart they are.
func TestWalkByContent(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	write(t, left, "differs.txt", "aaaaa", 0)
	write(t, right, "differs.txt", "bbbbb", 0)
	write(t, left, "matches.txt", "hello", 0)
	write(t, right, "matches.txt", "hello", 48*time.Hour)

	pairs, _, err := Walk(context.Background(), left, right, Options{Recursive: true, ByContent: true})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	got := byRel(pairs)
	if p := got["differs.txt"]; p.Kind != Differs {
		t.Errorf("differs.txt = %v, want Differs", p.Kind)
	}
	if p := got["matches.txt"]; p.Kind != Same {
		t.Errorf("matches.txt = %v, want Same", p.Kind)
	}
}

func TestWalkRecursionAndHidden(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	write(t, left, "top.txt", "x", 0)
	write(t, left, "sub/deep.txt", "x", 0)
	write(t, left, ".hidden.txt", "x", 0)

	shallow, _, err := Walk(context.Background(), left, right, Options{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if _, found := byRel(shallow)["sub/deep.txt"]; found {
		t.Error("a non-recursive comparison descended anyway")
	}
	if _, found := byRel(shallow)[".hidden.txt"]; found {
		t.Error("a dotfile showed up with hidden files off")
	}

	deep, _, err := Walk(context.Background(), left, right, Options{Recursive: true, Hidden: true})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	got := byRel(deep)
	if _, found := got["sub/deep.txt"]; !found {
		t.Error("the recursive comparison missed a nested file")
	}
	if _, found := got[".hidden.txt"]; !found {
		t.Error("the dotfile is missing with hidden files on")
	}
}

func TestWalkCapReportsTruncation(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	for i := 0; i < 10; i++ {
		write(t, left, string(rune('a'+i))+".txt", "x", 0)
	}

	pairs, truncated, err := Walk(context.Background(), left, right, Options{Recursive: true, Max: 4})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(pairs) != 4 {
		t.Fatalf("pairs = %d, want the cap of 4", len(pairs))
	}
	if !truncated {
		t.Error("truncated = false once the cap is hit")
	}
}

func TestWalkCancelled(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	write(t, left, "a.txt", "x", 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := Walk(ctx, left, right, Options{Recursive: true}); err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestCounts(t *testing.T) {
	pairs := []Pair{{Kind: Same}, {Kind: Differs}, {Kind: Differs}, {Kind: OnlyLeft}, {Kind: OnlyRight}}
	same, differs, onlyLeft, onlyRight := Counts(pairs)
	if same != 1 || differs != 2 || onlyLeft != 1 || onlyRight != 1 {
		t.Fatalf("counts = %d/%d/%d/%d, want 1/2/1/1", same, differs, onlyLeft, onlyRight)
	}
}
