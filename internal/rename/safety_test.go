package rename

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// Case-insensitive literal replacement over a name whose lower-case form has a
// different byte length used to slice the original at the lower-case offsets:
// "Ⱥ" lower-cases from two bytes to three, so the index ran past the end and
// the panic took the whole TUI down — Plan runs on every keystroke.
func TestPlanIgnoreCaseLiteralOverChangingWidths(t *testing.T) {
	tests := []struct{ name, find, repl, want string }{
		{"ȺȺȺȺȺȺx", "x", "y", "ȺȺȺȺȺȺy"},
		{"İstanbul-x", "x", "y", "İstanbul-y"},
		{"İİx", "İ", "i", "iix"},
		{"Report.TXT", "txt", "$1", "Report.$1"}, // literal: no backreferences
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := DefaultSpec()
			s.Mode, s.Ignore, s.Find, s.Replace = ModeLiteral, true, tc.find, tc.repl
			got := planNames(t, t.TempDir(), s, tc.name)[0]
			if got.New != tc.want || !utf8.ValidString(got.New) {
				t.Errorf("New = %q, want %q", got.New, tc.want)
			}
		})
	}
}

// A chain whose last link is blocked blocks the links that depended on it:
// 1→2 is only possible because 2→3 moves 2 away, and 2→3 is not possible
// because 3 is there and not part of the batch. The preview has to say so,
// rather than show 1→2 as fine and have Apply refuse it.
func TestPlanBlockedChainBlocksWhatDependsOnIt(t *testing.T) {
	root := tree(t, "1.txt", "2.txt", "3.txt")
	s := DefaultSpec()
	s.Name, s.Counter = "[C]", 2

	changes := planNames(t, root, s, "1.txt", "2.txt")
	for _, c := range changes {
		if c.Problem == "" {
			t.Errorf("%s → %s has no problem, want it held back", c.Rel, c.New)
		}
	}
	if _, err := Apply(context.Background(), root, changes, func(string) {}); err != nil {
		t.Errorf("Apply: %v", err)
	}
	if want := []string{"1.txt", "2.txt", "3.txt"}; !equal(listing(t, root), want) {
		t.Errorf("listing = %v, want it untouched", listing(t, root))
	}
}

// When a staged rename fails part-way through its group, the rollback puts back
// only what has not landed, and never onto a name something else now holds.
// Here a → b lands, c → a lands, then b → a fails: putting b's file back at b
// used to overwrite the a file that had just moved there.
func TestApplyRollbackNeverOverwrites(t *testing.T) {
	root := tree(t, "a", "b", "c")
	changes := []Change{
		{Rel: "a", New: "b", Changed: true},
		{Rel: "c", New: "a", Changed: true},
		{Rel: "b", New: "a", Changed: true},
	}
	_, err := Apply(context.Background(), root, changes, func(string) {})
	if err == nil {
		t.Fatal("want an error: two renames onto a")
	}

	// Every file's contents survive somewhere, staging name or not.
	var bodies []string
	for _, rel := range listing(t, root) {
		bodies = append(bodies, read(t, root, rel))
	}
	sort.Strings(bodies)
	if want := []string{"a", "b", "c"}; !equal(bodies, want) {
		t.Errorf("contents = %v, want all of %v", bodies, want)
	}
	if left := staging(t, root); len(left) > 0 && !strings.Contains(err.Error(), filepath.Base(left[0])) {
		t.Errorf("error %q does not name the leftover %v", err, left)
	}
}

// Cancelling stops a big directory part-way, not only between directories.
func TestApplyCancelsWithinADirectory(t *testing.T) {
	root := tree(t, "a", "b", "c", "d")
	s := DefaultSpec()
	s.Name = "x-[N]"
	changes := planNames(t, root, s, "a", "b", "c", "d")

	ctx, cancel := context.WithCancel(context.Background())
	done, err := Apply(ctx, root, changes, func(string) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(done) != 1 {
		t.Errorf("done = %v, want only the one rename before the cancel", done)
	}
	if left := staging(t, root); len(left) > 0 {
		t.Errorf("temporary names left behind: %v", left)
	}
}

// A negated glob class matches what is not in it, the way a shell reads it.
func TestGlobNegatedClass(t *testing.T) {
	s := DefaultSpec()
	s.Mode, s.Find, s.Replace = ModeGlob, "[!a]*.txt", "x"
	changes := planNames(t, t.TempDir(), s, "a1.txt", "b1.txt")
	if changes[0].Changed {
		t.Errorf("a1.txt → %s, want it left alone", changes[0].New)
	}
	if !changes[1].Changed {
		t.Error("b1.txt did not match [!a]*.txt")
	}

	// Regex metacharacters inside a class are just characters.
	s.Find = `[\]*`
	if _, err := Plan(t.TempDir(), []Candidate{{Rel: `a\b`}}, s); err != nil {
		t.Errorf("Plan: %v", err)
	}
}

// Windows refuses some names outright; a batch that produces one is held back
// in the preview instead of failing half-way through.
func TestValidateWindowsNames(t *testing.T) {
	bad := []string{"a<b", "a>b", "a:b", `a"b`, "a|b", "a?b", "a*b", `a\b`, "a\x01b",
		"CON", "con.txt", "NUL", "com1", "LPT9.log", "name.", "name "}
	for _, name := range bad {
		if validateFor(name, "windows") == "" {
			t.Errorf("%q passed for Windows", name)
		}
		if p := validateFor(name, "linux"); p != "" && name != `a\b` {
			t.Errorf("%q refused on Linux: %s", name, p)
		}
	}
	for _, name := range []string{"console.txt", "COM10", "a.b", "nul-x"} {
		if p := validateFor(name, "windows"); p != "" {
			t.Errorf("%q refused for Windows: %s", name, p)
		}
	}
}
