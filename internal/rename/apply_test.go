package rename

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// tree makes files (and the directories on the way) under a fresh root.
func tree(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		touch(t, root, filepath.FromSlash(p))
	}
	return root
}

// listing is every path under root, relative and slash-separated, for asserting
// on the whole result at once.
func listing(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil || path == root {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func applyPlan(t *testing.T, root string, s Spec, cands []Candidate) []string {
	t.Helper()
	changes, err := Plan(root, cands, s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	var steps []string
	if _, err := Apply(context.Background(), root, changes, func(rel string) { steps = append(steps, rel) }); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return steps
}

func TestApplyRenamesAndReportsEachStep(t *testing.T) {
	root := tree(t, "a.txt", "b.txt")

	s := DefaultSpec()
	s.Name = "x[N]"
	steps := applyPlan(t, root, s, []Candidate{{Rel: "a.txt"}, {Rel: "b.txt"}})

	if want := []string{"xa.txt", "xb.txt"}; !equal(listing(t, root), want) {
		t.Errorf("listing = %v, want %v", listing(t, root), want)
	}
	if len(steps) != 2 {
		t.Errorf("progress reported %d steps, want 2", len(steps))
	}
}

func TestApplySwap(t *testing.T) {
	root := tree(t, "a.txt", "b.txt")

	// a -> b and b -> a in one batch.
	changes := []Change{
		{Rel: "a.txt", New: "b.txt", Changed: true},
		{Rel: "b.txt", New: "a.txt", Changed: true},
	}
	if _, err := Apply(context.Background(), root, changes, func(string) {}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := read(t, root, "a.txt"); got != "b.txt" {
		t.Errorf("a.txt holds %q, want the old b.txt content", got)
	}
	if got := read(t, root, "b.txt"); got != "a.txt" {
		t.Errorf("b.txt holds %q, want the old a.txt content", got)
	}
	if left := staging(t, root); len(left) > 0 {
		t.Errorf("temporary names left behind: %v", left)
	}
}

func TestApplyChain(t *testing.T) {
	root := tree(t, "x1.txt", "x2.txt")

	s := DefaultSpec()
	s.Name, s.Counter = "x[C]", 2
	applyPlan(t, root, s, []Candidate{{Rel: "x1.txt"}, {Rel: "x2.txt"}})

	if want := []string{"x2.txt", "x3.txt"}; !equal(listing(t, root), want) {
		t.Fatalf("listing = %v, want %v", listing(t, root), want)
	}
	if got := read(t, root, "x2.txt"); got != "x1.txt" {
		t.Errorf("x2.txt holds %q, want the old x1.txt content", got)
	}
}

func TestApplyCaseOnlyChange(t *testing.T) {
	root := tree(t, "readme.md")

	s := DefaultSpec()
	s.Case = CaseUpper
	applyPlan(t, root, s, []Candidate{{Rel: "readme.md"}})

	// Staged through a temporary name, so this holds on a case-insensitive
	// filesystem too.
	if want := []string{"README.md"}; !equal(listing(t, root), want) {
		t.Errorf("listing = %v, want %v", listing(t, root), want)
	}
}

func TestApplyRenamesChildrenBeforeTheirDirectory(t *testing.T) {
	root := tree(t, "v1/v1-notes.txt", "v1/sub/v1-deep.txt")

	s := DefaultSpec()
	s.Mode, s.Find, s.Replace = ModeLiteral, "v1", "v2"

	cands := []Candidate{
		{Rel: filepath.FromSlash("v1"), IsDir: true},
		{Rel: filepath.FromSlash("v1/v1-notes.txt")},
		{Rel: filepath.FromSlash("v1/sub"), IsDir: true},
		{Rel: filepath.FromSlash("v1/sub/v1-deep.txt")},
	}
	applyPlan(t, root, s, cands)

	want := []string{"v2", "v2/sub", "v2/sub/v2-deep.txt", "v2/v2-notes.txt"}
	if got := listing(t, root); !equal(got, want) {
		t.Errorf("listing = %v, want %v", got, want)
	}
}

func TestApplyRefusesToOverwriteAndRollsBackStaging(t *testing.T) {
	root := tree(t, "a.txt", "b.txt", "keep.txt")

	// Hand-built plan: a -> b is staged (b is a source), and b -> keep.txt is
	// impossible. The batch must stop and leave nothing under a temporary name.
	changes := []Change{
		{Rel: "a.txt", New: "b.txt", Changed: true},
		{Rel: "b.txt", New: "keep.txt", Changed: true},
	}
	_, err := Apply(context.Background(), root, changes, func(string) {})
	if err == nil {
		t.Fatal("want an error rather than an overwritten keep.txt")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %v, want it to say the name is taken", err)
	}
	if got := read(t, root, "keep.txt"); got != "keep.txt" {
		t.Errorf("keep.txt was overwritten: %q", got)
	}
	if left := staging(t, root); len(left) > 0 {
		t.Errorf("temporary names left behind: %v", left)
	}
	if got := read(t, root, "a.txt"); got != "a.txt" {
		t.Errorf("a.txt should be back where it started, holds %q", got)
	}
}

func TestApplySkipsUnchangedAndConflicting(t *testing.T) {
	root := tree(t, "a.txt", "taken.txt")

	changes := []Change{
		{Rel: "a.txt", New: "taken.txt", Changed: true, Problem: "exists"},
		{Rel: "taken.txt", New: "taken.txt"},
	}
	if _, err := Apply(context.Background(), root, changes, func(string) {}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"a.txt", "taken.txt"}; !equal(listing(t, root), want) {
		t.Errorf("listing = %v, want it untouched", listing(t, root))
	}
}

func TestIsStagingName(t *testing.T) {
	if !IsStagingName(tempPrefix + "0-a.txt") {
		t.Error("a staging leftover should be recognised")
	}
	if IsStagingName("a.txt") {
		t.Error("an ordinary name is not a staging name")
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// staging returns any leftover temporary names under root.
func staging(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, p := range listing(t, root) {
		if IsStagingName(filepath.Base(p)) {
			out = append(out, p)
		}
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
