package rename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// applyAndUndo runs a spec, then puts it straight back, returning the listing
// after each half so a test can assert on both.
func applyAndUndo(t *testing.T, root string, s Spec, cands []Candidate) (after, restored []string) {
	t.Helper()

	changes, err := Plan(root, cands, s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	applied, err := Apply(root, changes, func(string) {})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	after = listing(t, root)

	if _, err := Undo(root, PlanUndo(root, applied), func(string) {}); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	return after, listing(t, root)
}

func TestUndoPutsNamesBack(t *testing.T) {
	root := tree(t, "a.txt", "b.txt")

	s := DefaultSpec()
	s.Name = "x[N]"
	after, restored := applyAndUndo(t, root, s, []Candidate{{Rel: "a.txt"}, {Rel: "b.txt"}})

	if want := []string{"xa.txt", "xb.txt"}; !equal(after, want) {
		t.Fatalf("after = %v, want %v", after, want)
	}
	if want := []string{"a.txt", "b.txt"}; !equal(restored, want) {
		t.Errorf("restored = %v, want %v", restored, want)
	}
}

func TestUndoRestoresADirectoryBeforeWhatIsInside(t *testing.T) {
	root := tree(t, "v1/v1-notes.txt", "v1/sub/v1-deep.txt")

	s := DefaultSpec()
	s.Mode, s.Find, s.Replace = ModeLiteral, "v1", "v2"
	cands := []Candidate{
		{Rel: filepath.FromSlash("v1"), IsDir: true},
		{Rel: filepath.FromSlash("v1/v1-notes.txt")},
		{Rel: filepath.FromSlash("v1/sub"), IsDir: true},
		{Rel: filepath.FromSlash("v1/sub/v1-deep.txt")},
	}
	after, restored := applyAndUndo(t, root, s, cands)

	if want := []string{"v2", "v2/sub", "v2/sub/v2-deep.txt", "v2/v2-notes.txt"}; !equal(after, want) {
		t.Fatalf("after = %v, want %v", after, want)
	}
	want := []string{"v1", "v1/sub", "v1/sub/v1-deep.txt", "v1/v1-notes.txt"}
	if !equal(restored, want) {
		t.Errorf("restored = %v, want %v", restored, want)
	}
}

func TestUndoOfASwapAndAChain(t *testing.T) {
	t.Run("swap", func(t *testing.T) {
		root := tree(t, "a.txt", "b.txt")
		applied, err := Apply(root, []Change{
			{Rel: "a.txt", New: "b.txt", Changed: true},
			{Rel: "b.txt", New: "a.txt", Changed: true},
		}, func(string) {})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Undo(root, PlanUndo(root, applied), func(string) {}); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if got := read(t, root, "a.txt"); got != "a.txt" {
			t.Errorf("a.txt holds %q, want its own content back", got)
		}
		if left := staging(t, root); len(left) > 0 {
			t.Errorf("temporary names left behind: %v", left)
		}
	})

	t.Run("chain", func(t *testing.T) {
		root := tree(t, "x1.txt", "x2.txt")
		s := DefaultSpec()
		s.Name, s.Counter = "x[C]", 2

		_, restored := applyAndUndo(t, root, s, []Candidate{{Rel: "x1.txt"}, {Rel: "x2.txt"}})
		if want := []string{"x1.txt", "x2.txt"}; !equal(restored, want) {
			t.Errorf("restored = %v, want %v", restored, want)
		}
		if got := read(t, root, "x1.txt"); got != "x1.txt" {
			t.Errorf("x1.txt holds %q, want its own content back", got)
		}
	})
}

func TestUndoOfACaseOnlyChange(t *testing.T) {
	root := tree(t, "readme.md")

	s := DefaultSpec()
	s.Case = CaseUpper
	after, restored := applyAndUndo(t, root, s, []Candidate{{Rel: "readme.md"}})

	if want := []string{"README.md"}; !equal(after, want) {
		t.Fatalf("after = %v, want %v", after, want)
	}
	if want := []string{"readme.md"}; !equal(restored, want) {
		t.Errorf("restored = %v, want %v", restored, want)
	}
}

// A name that moved on since the rename cannot be put back, and says so instead
// of holding up the rest of the batch.
func TestPlanUndoMarksWhatCannotBeUndone(t *testing.T) {
	root := tree(t, "a.txt", "b.txt")

	applied, err := Apply(root, []Change{
		{Rel: "a.txt", New: "a2.txt", Changed: true},
		{Rel: "b.txt", New: "b2.txt", Changed: true},
	}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}

	// One renamed away by hand, and the other's old name taken again.
	if err := os.Rename(filepath.Join(root, "a2.txt"), filepath.Join(root, "elsewhere.txt")); err != nil {
		t.Fatal(err)
	}
	touch(t, root, "b.txt")

	plan := PlanUndo(root, applied)
	problems := map[string]string{}
	for _, c := range plan {
		problems[c.Rel] = c.Problem
	}
	if got := problems["a2.txt"]; got != "gone" {
		t.Errorf("a2.txt problem = %q, want %q", got, "gone")
	}
	if got := problems["b2.txt"]; got != "exists" {
		t.Errorf("b2.txt problem = %q, want %q", got, "exists")
	}
	if got := Summarize(plan); got.Renamed != 0 || got.Conflicts != 2 {
		t.Errorf("Summarize = %+v, want both held back", got)
	}
}

// A batch that only half applied is still undoable: the record is what happened,
// not what was planned.
func TestUndoOfAPartialApply(t *testing.T) {
	root := tree(t, "a.txt", "b.txt", "keep.txt")

	applied, err := Apply(root, []Change{
		{Rel: "a.txt", New: "a2.txt", Changed: true},
		{Rel: "b.txt", New: "keep.txt", Changed: true},
	}, func(string) {})
	if err == nil {
		t.Fatal("want the second rename to be refused")
	}
	if len(applied) != 1 || applied[0].Rel != "a.txt" {
		t.Fatalf("applied = %+v, want just the first rename", applied)
	}

	if _, err := Undo(root, PlanUndo(root, applied), func(string) {}); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if want := []string{"a.txt", "b.txt", "keep.txt"}; !equal(listing(t, root), want) {
		t.Errorf("listing = %v, want %v", listing(t, root), want)
	}
}

// Changes that never ran are not part of the record, so undoing cannot touch the
// files they were held back from.
func TestPlanUndoIgnoresWhatNeverRan(t *testing.T) {
	root := tree(t, "a.txt")

	plan := PlanUndo(root, []Change{
		{Rel: "a.txt", New: "a.txt"},                                       // unchanged
		{Rel: "a.txt", New: "taken.txt", Changed: true, Problem: "exists"}, // blocked
	})
	if len(plan) != 0 {
		t.Errorf("plan = %+v, want nothing to undo", plan)
	}
}

func TestUndoLabel(t *testing.T) {
	tests := []struct {
		name    string
		changes []Change
		want    string
	}{
		{"one file", []Change{{Rel: "a"}}, "1 file"},
		{"files", []Change{{Rel: "a"}, {Rel: "b"}}, "2 files"},
		{"a folder", []Change{{Rel: "d", IsDir: true}}, "1 folder"},
		{"both", []Change{{Rel: "d", IsDir: true}, {Rel: "a"}, {Rel: "b"}}, "1 folder and 2 files"},
		{"nothing", nil, "nothing"},
	}
	for _, tc := range tests {
		if got := UndoLabel(tc.changes); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestUnder(t *testing.T) {
	dirs := map[string]bool{filepath.FromSlash("v1"): true, filepath.FromSlash("v1/sub"): true}

	for _, tc := range []struct {
		rel  string
		want bool
	}{
		{"v1/notes.txt", true},
		{"v1/sub/deep.txt", true},
		{"v1", false}, // the directory itself is not under itself
		{"other/notes.txt", false},
		{"notes.txt", false},
	} {
		if got := under(filepath.FromSlash(tc.rel), dirs); got != tc.want {
			t.Errorf("under(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}

func TestUndoIsIdempotentlySafeWhenNothingIsLeft(t *testing.T) {
	root := tree(t, "a.txt")
	applied, err := Apply(root, []Change{{Rel: "a.txt", New: "b.txt", Changed: true}}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Undo(root, PlanUndo(root, applied), func(string) {}); err != nil {
		t.Fatal(err)
	}

	// Undoing the same record again finds nothing to work with, and says so
	// rather than clobbering the restored file.
	plan := PlanUndo(root, applied)
	if got := Summarize(plan); got.Conflicts != 1 || got.Renamed != 0 {
		t.Errorf("Summarize = %+v, want the entry held back", got)
	}
	done, err := Undo(root, plan, func(string) {})
	if err != nil || len(done) != 0 {
		t.Errorf("Undo = %v, %v — want a no-op", done, err)
	}
	if !strings.HasSuffix(read(t, root, "a.txt"), "a.txt") {
		t.Error("a.txt was disturbed")
	}
}
