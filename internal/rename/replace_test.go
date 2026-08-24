package rename

import (
	"testing"
	"time"
)

// The replacement carries tokens as well as backreferences, so these two ways of
// building a name can be mixed in one form.
func TestReplacementTokens(t *testing.T) {
	mod := time.Date(2026, 8, 24, 9, 41, 12, 0, time.UTC)

	tests := []struct {
		name          string
		mode          Mode
		find, replace string
		nameMask      string
		counter       int
		files         []string
		want          []string
	}{
		{
			name: "a date beside a backreference",
			mode: ModeRegex, find: `IMG_(\d+)`, replace: "holiday_[d]_$1",
			files: []string{"IMG_0021.jpg"},
			want:  []string{"holiday_2026-08-24_0021.jpg"},
		},
		{
			name: "the time token",
			mode: ModeLiteral, find: "log", replace: "log_[t]",
			files: []string{"log.txt"},
			want:  []string{"log_09-41-12.txt"},
		},
		{
			name: "a counter in the replacement",
			mode: ModeLiteral, find: "draft", replace: "v[C3]",
			files: []string{"draft-a.txt", "draft-b.txt"},
			want:  []string{"v001-a.txt", "v002-b.txt"},
		},
		{
			// Both [C]s are the same candidate's number: the mask puts 5 in
			// front, and the replacement writes 5 where "img" was.
			name:     "the counter is the value the masks see",
			mode:     ModeLiteral,
			find:     "img",
			replace:  "[C]",
			nameMask: "[C]_[N]",
			counter:  5,
			files:    []string{"img7.png"},
			want:     []string{"5_57.png"},
		},
		{
			name: "the original name and its parts",
			mode: ModeRegex, find: `^`, replace: "[E]-",
			files: []string{"notes.txt"},
			want:  []string{"txt-notes.txt"},
		},
		{
			name: "an unknown token is left as typed",
			mode: ModeLiteral, find: "a", replace: "[Z]",
			files: []string{"a.txt"},
			want:  []string{"[Z].txt"},
		},
		{
			name: "no tokens at all is untouched",
			mode: ModeLiteral, find: "a", replace: "b",
			files: []string{"a.txt"},
			want:  []string{"b.txt"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := DefaultSpec()
			s.Mode, s.Find, s.Replace = tc.mode, tc.find, tc.replace
			if tc.nameMask != "" {
				s.Name = tc.nameMask
			}
			if tc.counter != 0 {
				s.Counter = tc.counter
			}

			cands := make([]Candidate, 0, len(tc.files))
			for _, f := range tc.files {
				cands = append(cands, Candidate{Rel: f, Mod: mod})
			}
			changes, err := Plan(t.TempDir(), cands, s)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			for i, c := range changes {
				if c.New != tc.want[i] {
					t.Errorf("%s -> %s, want %s", c.Rel, c.New, tc.want[i])
				}
			}
		})
	}
}

// find is matched against the name as it stands, which makes it a filter over
// the whole form: a candidate it does not match keeps its name, masks and all.
func TestFindFiltersTheWholeForm(t *testing.T) {
	s := DefaultSpec()
	s.Mode, s.Find, s.Replace = ModeLiteral, "keep", "kept"
	s.Name = "[C]-[N]" // would renumber everything, if it ran

	changes, err := Plan(t.TempDir(), []Candidate{{Rel: "keep.txt"}, {Rel: "other.txt"}}, s)
	if err != nil {
		t.Fatal(err)
	}
	if got := changes[0].New; got != "1-kept.txt" {
		t.Errorf("matching name -> %s, want %s", got, "1-kept.txt")
	}
	if got := changes[1].New; got != "other.txt" {
		t.Errorf("non-matching name -> %s, want the masks not to run either", got)
	}
}

// A dollar in a token's value is data, not a group reference. In the regexp-backed
// modes it has to be escaped on the way into the replacement template; in literal
// mode there is no template, so escaping it would be wrong.
func TestReplacementTokenWithADollarInIt(t *testing.T) {
	t.Run("regex mode escapes it", func(t *testing.T) {
		s := DefaultSpec()
		s.Mode, s.Find, s.Replace = ModeRegex, `a\$b`, "[N]!"

		changes, err := Plan(t.TempDir(), []Candidate{{Rel: "a$b.txt"}}, s)
		if err != nil {
			t.Fatal(err)
		}
		if got := changes[0].New; got != "a$b!.txt" {
			t.Errorf("got %q, want %q", got, "a$b!.txt")
		}
	})

	t.Run("literal mode does not", func(t *testing.T) {
		s := DefaultSpec()
		s.Mode, s.Find, s.Replace = ModeLiteral, "q", "[N]"

		changes, err := Plan(t.TempDir(), []Candidate{{Rel: "q$1.txt"}}, s)
		if err != nil {
			t.Fatal(err)
		}
		if got := changes[0].New; got != "q$1$1.txt" {
			t.Errorf("got %q, want %q", got, "q$1$1.txt")
		}
	})

	t.Run("a typed backreference still works", func(t *testing.T) {
		s := DefaultSpec()
		s.Mode, s.Find, s.Replace = ModeRegex, `(\d+)`, "[E]-$1"

		changes, err := Plan(t.TempDir(), []Candidate{{Rel: "img7.png"}}, s)
		if err != nil {
			t.Fatal(err)
		}
		if got := changes[0].New; got != "imgpng-7.png" {
			t.Errorf("got %q, want %q", got, "imgpng-7.png")
		}
	})
}
