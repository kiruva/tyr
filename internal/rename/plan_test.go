package rename

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// planNames is the shorthand the table tests use: run a spec over bare names in
// an empty directory and return "old -> new" for each.
func planNames(t *testing.T, root string, s Spec, names ...string) []Change {
	t.Helper()
	cands := make([]Candidate, 0, len(names))
	for _, n := range names {
		cands = append(cands, Candidate{Rel: n, Mod: time.Date(2024, 3, 9, 14, 5, 6, 0, time.UTC)})
	}
	changes, err := Plan(root, cands, s)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return changes
}

func TestPlanRegexKeepsUnmatchedTail(t *testing.T) {
	s := DefaultSpec()
	s.Find, s.Replace = `IMG_(\d+)`, "holiday-$1"

	got := planNames(t, t.TempDir(), s, "IMG_0021.jpg", "img_0022.jpg", "notes.txt")

	// The pattern is case-sensitive by default, so the lowercase one is not a match.
	want := []string{"holiday-0021.jpg", "img_0022.jpg", "notes.txt"}
	for i, c := range got {
		if c.New != want[i] {
			t.Errorf("%s -> %s, want %s", c.Rel, c.New, want[i])
		}
	}
	if got[1].Changed || got[2].Changed {
		t.Error("unmatched names should be left alone")
	}
}

func TestPlanReplacementIsNotAppliedTwice(t *testing.T) {
	// "a" -> "ab" must not rewrite the "a" it just introduced.
	s := DefaultSpec()
	s.Mode, s.Find, s.Replace = ModeLiteral, "a", "ab"

	got := planNames(t, t.TempDir(), s, "a.txt")
	if got[0].New != "ab.txt" {
		t.Errorf("got %q, want %q", got[0].New, "ab.txt")
	}
}

func TestPlanModes(t *testing.T) {
	tests := []struct {
		name          string
		mode          Mode
		find, replace string
		in, want      string
	}{
		{"regex backref", ModeRegex, `(\w+)-(\w+)`, "$2-$1", "left-right.md", "right-left.md"},
		{"regex anchors", ModeRegex, `^v\d+_`, "", "v2_report.pdf", "report.pdf"},
		{"literal keeps metacharacters", ModeLiteral, "a.b", "x", "a.b.txt", "x.txt"},
		{"literal is not a regex", ModeLiteral, "a.b", "x", "axb.txt", "axb.txt"},
		{"glob is anchored", ModeGlob, "draft*", "final", "draft-1.txt", "final"},
		{"glob does not match a tail", ModeGlob, "raft*", "final", "draft-1.txt", "draft-1.txt"},
		{"glob group backref", ModeGlob, "*.log", "archive-$1.log", "nginx.log", "archive-nginx.log"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := DefaultSpec()
			s.Mode, s.Find, s.Replace = tc.mode, tc.find, tc.replace
			if got := planNames(t, t.TempDir(), s, tc.in)[0].New; got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPlanIgnoreCase(t *testing.T) {
	s := DefaultSpec()
	s.Mode, s.Find, s.Replace, s.Ignore = ModeLiteral, "img", "photo", true

	if got := planNames(t, t.TempDir(), s, "IMG_1.jpg")[0].New; got != "photo_1.jpg" {
		t.Errorf("got %q, want %q", got, "photo_1.jpg")
	}
}

func TestPlanBadRegexIsReported(t *testing.T) {
	s := DefaultSpec()
	s.Find = "([unclosed"
	if _, err := Plan(t.TempDir(), []Candidate{{Rel: "a"}}, s); err == nil {
		t.Fatal("want an error for an unparsable regex")
	}
}

func TestPlanMasksAndCounter(t *testing.T) {
	tests := []struct {
		name      string
		nameMask  string
		extMask   string
		in        []string
		want      []string
		counter   int
		step      int
		asSubdirs bool
	}{
		{
			name: "counter with padding", nameMask: "shot-[C3]", extMask: "[E]", counter: 1, step: 1,
			in:   []string{"a.png", "b.png", "c.png"},
			want: []string{"shot-001.png", "shot-002.png", "shot-003.png"},
		},
		{
			name: "counter start and step", nameMask: "[C]", extMask: "[E]", counter: 10, step: 5,
			in:   []string{"a.txt", "b.txt"},
			want: []string{"10.txt", "15.txt"},
		},
		{
			name: "slices of the name", nameMask: "[N1-3]_[N5-]", extMask: "[E]",
			in:   []string{"abcdefgh.log"},
			want: []string{"abc_efgh.log"},
		},
		{
			name: "single character and clamped range", nameMask: "[N1][N4-99]", extMask: "[E]",
			in:   []string{"abc.log"},
			want: []string{"a.log"},
		},
		{
			name: "extension rewritten", nameMask: "[N]", extMask: "bak",
			in:   []string{"notes.txt"},
			want: []string{"notes.bak"},
		},
		{
			name: "extension dropped", nameMask: "[N]", extMask: "",
			in:   []string{"notes.txt"},
			want: []string{"notes"},
		},
		{
			name: "date and time of the file", nameMask: "[d]_[t]_[N]", extMask: "[E]",
			in:   []string{"log.txt"},
			want: []string{"2024-03-09_14-05-06_log.txt"},
		},
		{
			name: "parent directory name", nameMask: "[P]-[N]", extMask: "[E]", asSubdirs: true,
			in:   []string{"holiday/1.jpg", "work/1.jpg"},
			want: []string{"holiday-1.jpg", "work-1.jpg"},
		},
		{
			name: "unknown token is literal", nameMask: "[Z]-[N]", extMask: "[E]",
			in:   []string{"a.txt"},
			want: []string{"[Z]-a.txt"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := DefaultSpec()
			s.Name, s.Ext = tc.nameMask, tc.extMask
			if tc.counter != 0 {
				s.Counter, s.Step = tc.counter, tc.step
			}
			got := planNames(t, t.TempDir(), s, tc.in...)
			for i := range tc.want {
				if got[i].New != tc.want[i] {
					t.Errorf("%s -> %s, want %s", got[i].Rel, got[i].New, tc.want[i])
				}
			}
		})
	}
}

func TestPlanCounterOnlyAdvancesOnMatches(t *testing.T) {
	s := DefaultSpec()
	s.Mode, s.Find, s.Replace = ModeLiteral, "keep", "keep"
	s.Name = "[C]-[N]"

	got := planNames(t, t.TempDir(), s, "keep-a.txt", "skip-b.txt", "keep-c.txt")

	if got[0].New != "1-keep-a.txt" {
		t.Errorf("first match got %q", got[0].New)
	}
	if got[1].Changed {
		t.Errorf("non-matching name got %q", got[1].New)
	}
	if got[2].New != "2-keep-c.txt" {
		t.Errorf("second match got %q, want the counter not to have burned a value", got[2].New)
	}
}

func TestPlanCaseAndTrim(t *testing.T) {
	tests := []struct {
		name     string
		op       CaseOp
		trim     bool
		in, want string
	}{
		{"lower leaves the extension", CaseLower, false, "MyFile.TXT", "myfile.TXT"},
		{"upper leaves the extension", CaseUpper, false, "MyFile.txt", "MYFILE.txt"},
		{"title case", CaseTitle, false, "my long_file name.txt", "My Long_File Name.txt"},
		{"trim collapses whitespace", CaseNone, true, "  a   b  .txt", "a b.txt"},
		{"trim strips trailing dots", CaseNone, true, "name...txt", "name.txt"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := DefaultSpec()
			s.Case, s.Trim = tc.op, tc.trim
			if got := planNames(t, t.TempDir(), s, tc.in)[0].New; got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPlanDotfileHasNoExtension(t *testing.T) {
	s := DefaultSpec()
	s.Name = "x[N]"

	if got := planNames(t, t.TempDir(), s, ".bashrc")[0].New; got != "x.bashrc" {
		t.Errorf("got %q, want %q", got, "x.bashrc")
	}
}

func TestPlanDirectoryKeepsItsWholeName(t *testing.T) {
	s := DefaultSpec()
	s.Name, s.Ext = "[N]", "[E]"
	s.Mode, s.Find, s.Replace = ModeLiteral, "v1", "v2"

	changes, err := Plan(t.TempDir(), []Candidate{{Rel: "site.v1", IsDir: true}}, s)
	if err != nil {
		t.Fatal(err)
	}
	if changes[0].New != "site.v2" {
		t.Errorf("got %q, want %q — a directory has no extension to protect", changes[0].New, "site.v2")
	}
}

func TestPlanRejectsImpossibleNames(t *testing.T) {
	tests := []struct{ name, mask, want string }{
		{"empty", "", "empty name"},
		{"separator", "sub/[N]", "name contains a separator"},
		{"reserved", "..", "reserved name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := DefaultSpec()
			s.Name, s.Ext = tc.mask, ""
			got := planNames(t, t.TempDir(), s, "a.txt")[0]
			if got.Problem != tc.want {
				t.Errorf("Problem = %q, want %q", got.Problem, tc.want)
			}
		})
	}
}

func TestPlanConflicts(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "taken.txt")

	t.Run("two candidates want one name", func(t *testing.T) {
		s := DefaultSpec()
		s.Name, s.Ext = "same", "txt"
		got := planNames(t, root, s, "a.txt", "b.txt")
		if got[0].Problem != "" {
			t.Errorf("first should be fine, got %q", got[0].Problem)
		}
		if got[1].Problem == "" {
			t.Error("second should be flagged as a duplicate")
		}
	})

	t.Run("name already on disk", func(t *testing.T) {
		s := DefaultSpec()
		s.Mode, s.Find, s.Replace = ModeLiteral, "a", "taken"
		got := planNames(t, root, s, "a.txt")
		if got[0].Problem != "exists" {
			t.Errorf("Problem = %q, want %q", got[0].Problem, "exists")
		}
	})

	t.Run("the batch frees the name first", func(t *testing.T) {
		// x1 -> x2 while x2 -> x3: x2 exists, but the batch moves it away, so
		// neither is a conflict. Apply stages this through a temporary name.
		dir := t.TempDir()
		touch(t, dir, "x1.txt")
		touch(t, dir, "x2.txt")

		s := DefaultSpec()
		s.Name, s.Counter = "x[C]", 2

		changes, err := Plan(dir, []Candidate{{Rel: "x1.txt"}, {Rel: "x2.txt"}}, s)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range changes {
			if c.Problem != "" {
				t.Errorf("%s -> %s: unexpected problem %q", c.Rel, c.New, c.Problem)
			}
		}
	})

	t.Run("case-only change is not a conflict", func(t *testing.T) {
		s := DefaultSpec()
		s.Case = CaseUpper
		got := planNames(t, root, s, "taken.txt")
		if got[0].Problem != "" {
			t.Errorf("Problem = %q, want none", got[0].Problem)
		}
	})
}

func TestSummarizeAndApplicableOrder(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "blocked")

	changes := []Change{
		{Rel: "a/b/deep.txt", New: "deep2.txt", Changed: true},
		{Rel: "top.txt", New: "top2.txt", Changed: true},
		{Rel: "a/mid.txt", New: "mid2.txt", Changed: true},
		{Rel: "keep.txt", New: "keep.txt"},
		{Rel: "bad.txt", New: "blocked", Changed: true, Problem: "exists"},
	}

	if got := Summarize(changes); got.Total != 5 || got.Renamed != 3 || got.Conflicts != 1 {
		t.Errorf("Summarize = %+v", got)
	}

	app := Applicable(changes)
	want := []string{"a/b/deep.txt", "a/mid.txt", "top.txt"}
	for i, c := range app {
		if c.Rel != filepath.FromSlash(want[i]) {
			t.Errorf("position %d = %s, want %s (deepest first)", i, c.Rel, want[i])
		}
	}
}

func touch(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
}
