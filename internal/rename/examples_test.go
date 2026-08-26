package rename

import (
	"path/filepath"
	"testing"
	"time"
)

// These are the worked examples in MAN.md, one test case each. If a change
// here makes one of them fail, the documented recipe is what is wrong — fix the
// manual with it.

func TestDocumentedExamples(t *testing.T) {
	mod := time.Date(2026, 8, 24, 9, 41, 12, 0, time.UTC)

	tests := []struct {
		recipe string   // what the manual calls it
		files  []string // in, in listing order
		want   []string // out
		spec   func(*Spec)
	}{
		{
			recipe: "number a batch, keeping the extension",
			files:  []string{"DSC_4821.JPG", "DSC_4830.JPG", "DSC_4901.JPG"},
			want:   []string{"holiday-001.JPG", "holiday-002.JPG", "holiday-003.JPG"},
			spec:   func(s *Spec) { s.Name = "holiday-[C3]" },
		},
		{
			recipe: "number from 10, in tens",
			files:  []string{"a.txt", "b.txt", "c.txt"},
			want:   []string{"10.txt", "20.txt", "30.txt"},
			spec:   func(s *Spec) { s.Name, s.Counter, s.Step = "[C]", 10, 10 },
		},
		{
			recipe: "count down",
			files:  []string{"a.txt", "b.txt", "c.txt"},
			want:   []string{"3.txt", "2.txt", "1.txt"},
			spec:   func(s *Spec) { s.Name, s.Counter, s.Step = "[C]", 3, -1 },
		},
		{
			recipe: "a padded counter that runs past zero keeps its sign readable",
			files:  []string{"a.txt", "b.txt"},
			want:   []string{"00.txt", "-01.txt"},
			spec:   func(s *Spec) { s.Name, s.Counter, s.Step = "[C2]", 0, -1 },
		},
		{
			recipe: "keep the name, add a number",
			files:  []string{"intro.md", "setup.md"},
			want:   []string{"01-intro.md", "02-setup.md"},
			spec:   func(s *Spec) { s.Name = "[C2]-[N]" },
		},
		{
			recipe: "strip a leading track number",
			files:  []string{"01 - Intro.mp3", "02 - Verse.mp3"},
			want:   []string{"Intro.mp3", "Verse.mp3"},
			spec:   func(s *Spec) { s.Find = `^\d+ - ` },
		},
		{
			recipe: "swap the two halves of a name",
			files:  []string{"Miles Davis - So What.mp3"},
			want:   []string{"So What - Miles Davis.mp3"},
			spec:   func(s *Spec) { s.Find, s.Replace = `^(.+) - (.+)\.mp3$`, "$2 - $1.mp3" },
		},
		{
			recipe: "literal mode, for a pattern full of dots",
			files:  []string{"app v1.2.zip"},
			want:   []string{"app v2.0.zip"},
			spec:   func(s *Spec) { s.Mode, s.Find, s.Replace = ModeLiteral, "v1.2", "v2.0" },
		},
		{
			recipe: "glob mode, matching the whole name",
			files:  []string{"draft-notes.txt", "final-notes.txt"},
			want:   []string{"2026-notes.txt", "final-notes.txt"},
			spec:   func(s *Spec) { s.Mode, s.Find, s.Replace = ModeGlob, "draft-*", "2026-$1" },
		},
		{
			recipe: "case-insensitive match",
			files:  []string{"IMG_1.jpg", "img_2.jpg"},
			want:   []string{"photo_1.jpg", "photo_2.jpg"},
			spec: func(s *Spec) {
				s.Mode, s.Find, s.Replace, s.Ignore = ModeLiteral, "img", "photo", true
			},
		},
		{
			recipe: "underscores to dashes",
			files:  []string{"my_long_name.txt"},
			want:   []string{"my-long-name.txt"},
			spec:   func(s *Spec) { s.Mode, s.Find, s.Replace = ModeLiteral, "_", "-" },
		},
		{
			recipe: "change the extension",
			files:  []string{"notes.txt", "todo.txt"},
			want:   []string{"notes.md", "todo.md"},
			spec:   func(s *Spec) { s.Ext = "md" },
		},
		{
			recipe: "lowercase a shouting extension",
			files:  []string{"DSC_1.JPG"},
			want:   []string{"DSC_1.jpg"},
			spec:   func(s *Spec) { s.Ext = "jpg" },
		},
		{
			recipe: "drop the extension",
			files:  []string{"README.md"},
			want:   []string{"README"},
			spec:   func(s *Spec) { s.Ext = "" },
		},
		{
			recipe: "tidy up a messy name",
			files:  []string{"  my   HOLIDAY  photo .jpg"},
			want:   []string{"My Holiday Photo.jpg"},
			spec:   func(s *Spec) { s.Case, s.Trim = CaseTitle, true },
		},
		{
			recipe: "shorten to the first 8 characters",
			files:  []string{"a-very-long-filename.log"},
			want:   []string{"a-very-l.log"},
			spec:   func(s *Spec) { s.Name = "[N1-8]" },
		},
		{
			recipe: "drop a fixed-width prefix",
			files:  []string{"2026-08-24 meeting.md"},
			want:   []string{"meeting.md"},
			spec:   func(s *Spec) { s.Name = "[N12-]" },
		},
		{
			recipe: "stamp the file's own date on it",
			files:  []string{"report.pdf"},
			want:   []string{"2026-08-24 report.pdf"},
			spec:   func(s *Spec) { s.Name = "[d] [N]" },
		},
		{
			recipe: "stamp the file's date before the extension",
			files:  []string{"notes.md", "todo.md"},
			want:   []string{"notes_2026-08-24.md", "todo_2026-08-24.md"},
			spec:   func(s *Spec) { s.Name = "[N]_[d]" },
		},
		{
			recipe: "stamp the date, but only on the names that match",
			files:  []string{"report-q3.pdf", "notes.pdf"},
			want:   []string{"report-q3_2026-08-24.pdf", "notes.pdf"},
			// ${1} rather than $1: "$1_2026" would read as a group named "1_2026".
			spec: func(s *Spec) { s.Find, s.Replace = `^(report[^.]*)`, "${1}_[d]" },
		},
		{
			recipe: "a token and a backreference in one replacement",
			files:  []string{"IMG_0021.jpg"},
			want:   []string{"holiday_2026-08-24_0021.jpg"},
			spec:   func(s *Spec) { s.Find, s.Replace = `IMG_(\d+)`, "holiday_[d]_$1" },
		},
		{
			recipe: "version-number what the pattern found",
			files:  []string{"draft-a.txt", "draft-b.txt"},
			want:   []string{"v001-a.txt", "v002-b.txt"},
			spec: func(s *Spec) {
				s.Mode, s.Find, s.Replace = ModeLiteral, "draft", "v[C3]"
			},
		},
		{
			recipe: "only the matching files are touched",
			files:  []string{"IMG_11.jpg", "scan.jpg", "IMG_12.jpg"},
			want:   []string{"photo-1.jpg", "scan.jpg", "photo-2.jpg"},
			spec: func(s *Spec) {
				s.Mode, s.Find = ModeLiteral, "IMG"
				s.Replace, s.Name = "IMG", "photo-[C]"
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.recipe, func(t *testing.T) {
			s := DefaultSpec()
			tc.spec(&s)

			cands := make([]Candidate, 0, len(tc.files))
			for _, f := range tc.files {
				cands = append(cands, Candidate{Rel: f, Mod: mod})
			}

			changes, err := Plan(t.TempDir(), cands, s)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			for i, c := range changes {
				if c.Problem != "" {
					t.Errorf("%s: unexpected problem %q", c.Rel, c.Problem)
				}
				if c.New != tc.want[i] {
					t.Errorf("%s -> %s, want %s", c.Rel, c.New, tc.want[i])
				}
			}
		})
	}
}

// The parent-directory token earns its own case: it needs candidates that live
// somewhere, which the table above does not have.
func TestDocumentedExampleParentToken(t *testing.T) {
	s := DefaultSpec()
	s.Name = "[P]-[N]"

	cands := []Candidate{
		{Rel: filepath.FromSlash("holiday/1.jpg")},
		{Rel: filepath.FromSlash("work/1.jpg")},
	}
	changes, err := Plan(t.TempDir(), cands, s)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"holiday-1.jpg", "work-1.jpg"}
	for i, c := range changes {
		if c.New != want[i] {
			t.Errorf("%s -> %s, want %s", c.Rel, c.New, want[i])
		}
	}
}

// The manual's worked trace of how one name is built, step by step.
func TestDocumentedPipelineTrace(t *testing.T) {
	s := DefaultSpec()
	s.Name, s.Ext = "[N]", "[E]"
	s.Find, s.Replace = `IMG_(\d+)`, "holiday-$1"
	s.Case = CaseLower

	changes, err := Plan(t.TempDir(), []Candidate{{Rel: "IMG_0021.JPG"}}, s)
	if err != nil {
		t.Fatal(err)
	}
	// masks reproduce "IMG_0021" + "JPG", substitution rewrites the stem it
	// matched, and the case op leaves the extension alone.
	if got := changes[0].New; got != "holiday-0021.JPG" {
		t.Errorf("got %q, want %q", got, "holiday-0021.JPG")
	}
}
