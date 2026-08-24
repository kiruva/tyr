package rename

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Candidate is one item a rename could touch. Rel is its path relative to the
// root the tool was opened on, so a recursive batch and a flat one look the same
// to Plan.
type Candidate struct {
	Rel   string
	IsDir bool
	Mod   time.Time
}

// Change is what Plan decided for one candidate. New is a bare name, never a
// path: a rename never moves anything between directories.
type Change struct {
	Rel     string // source, relative to the root
	New     string // new base name
	IsDir   bool   // the source is a directory
	Changed bool   // New differs from the current name
	Problem string // why this one cannot be applied ("" = fine)
}

// NewRel is the change's destination, relative to the root.
func (c Change) NewRel() string {
	if dir := filepath.Dir(c.Rel); dir != "." {
		return filepath.Join(dir, c.New)
	}
	return c.New
}

// Plan computes the new name for every candidate and marks the ones that cannot
// be applied. It reads the filesystem only to look for names already in the way,
// so it is cheap enough to re-run on every keystroke.
//
// The counter advances for each candidate the pattern matches, in the order they
// are given, whether or not the resulting name differs.
func Plan(root string, cands []Candidate, s Spec) ([]Change, error) {
	mt, err := newMatcher(s)
	if err != nil {
		return nil, err
	}
	if s.Step == 0 {
		s.Step = 1
	}

	changes := make([]Change, 0, len(cands))
	count := s.Counter

	for _, c := range cands {
		name := filepath.Base(c.Rel)
		ch := Change{Rel: c.Rel, New: name, IsDir: c.IsDir}

		if !mt.matches(name) {
			changes = append(changes, ch)
			continue
		}

		ch.New, ch.Problem = apply1(name, c, count, s, mt)
		ch.Changed = ch.New != name
		count += s.Step
		changes = append(changes, ch)
	}

	markConflicts(root, changes)
	return changes, nil
}

// apply1 builds one new name: masks first, then substitution over the assembled
// result, then case and trim. Substituting last is what makes a pattern like
// `IMG_(\d+)` into `holiday-$1` keep the extension it never matched.
func apply1(name string, c Candidate, count int, s Spec, mt *matcher) (string, string) {
	stem, ext := splitName(name, c.IsDir)
	in := maskInput{
		stem:    stem,
		ext:     ext,
		parent:  parentName(c.Rel),
		counter: count,
		mod:     c.Mod,
	}

	// The replacement carries tokens too, resolved against the same candidate as
	// the masks — so [d] in replace is the same date [d] in name would be.
	repl := expandReplacement(s.Replace, in, s.Mode)

	// One substitution pass, over the whole assembled name: applying it to the
	// stem and the extension separately would run twice over a replacement that
	// re-introduces the pattern.
	assembled := mt.sub(join(expand(s.Name, in), expand(s.Ext, in)), repl)

	// The masks or the replacement may have moved the dot, so re-split before
	// case and trim, which promise to leave the extension alone.
	newStem, newExt := splitName(assembled, c.IsDir)
	newStem = convertCase(newStem, s.Case)
	if s.Trim {
		newStem = trim(newStem)
	}

	out := join(newStem, newExt)
	return out, validate(out)
}

// join puts a name back together, dropping the dot when there is no extension.
func join(stem, ext string) string {
	if ext == "" {
		return stem
	}
	return stem + "." + ext
}

// splitName separates the extension. A leading dot belongs to the name — the
// extension of ".bashrc" is nothing — and a directory has no extension at all.
func splitName(name string, isDir bool) (stem, ext string) {
	if isDir {
		return name, ""
	}
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}

// parentName is the name of the directory holding rel, for [P].
func parentName(rel string) string {
	dir := filepath.Dir(rel)
	if dir == "." || dir == string(filepath.Separator) {
		return ""
	}
	return filepath.Base(dir)
}

func convertCase(s string, op CaseOp) string {
	switch op {
	case CaseLower:
		return strings.ToLower(s)
	case CaseUpper:
		return strings.ToUpper(s)
	case CaseTitle:
		return titleCase(s)
	default:
		return s
	}
}

// titleCase uppercases the first letter of each word, treating anything that is
// not a letter or digit as a word break.
func titleCase(s string) string {
	out := []rune(strings.ToLower(s))
	start := true
	for i, r := range out {
		switch {
		case start && unicode.IsLetter(r):
			out[i] = unicode.ToUpper(r)
			start = false
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			start = true
		default:
			start = false
		}
	}
	return string(out)
}

// trim collapses runs of whitespace and strips the ends, plus the trailing dots
// and spaces that some filesystems refuse outright.
func trim(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimRight(s, ". ")
}

// ValidateName reports why a name cannot be a single directory entry, which the
// single-entry rename prompt asks before it touches anything.
func ValidateName(name string) error {
	if p := validate(name); p != "" {
		return errors.New(p)
	}
	return nil
}

// validate rejects a name that cannot be a single directory entry.
func validate(name string) string {
	switch {
	case name == "":
		return "empty name"
	case name == "." || name == "..":
		return "reserved name"
	case strings.ContainsRune(name, '/'), strings.ContainsRune(name, filepath.Separator):
		return "name contains a separator"
	case strings.ContainsRune(name, 0):
		return "name contains a NUL"
	}
	return ""
}

// markConflicts flags the changes that would collide: two candidates aiming at
// the same name, or a name already taken by something the batch does not move
// out of the way. Comparison is case-insensitive because the filesystems this
// runs on often are, and "already there" is the safer answer to be wrong about.
func markConflicts(root string, changes []Change) {
	// Every source, and whether the batch frees that name up.
	sources := make(map[string]bool, len(changes))
	for _, c := range changes {
		sources[fold(c.Rel)] = c.Changed && c.Problem == ""
	}

	seen := make(map[string]int, len(changes))
	for i := range changes {
		c := &changes[i]
		if !c.Changed || c.Problem != "" {
			continue
		}
		key := fold(c.NewRel())

		if j, dup := seen[key]; dup {
			c.Problem = "same name as " + filepath.Base(changes[j].Rel)
			continue
		}
		seen[key] = i

		if _, err := os.Lstat(filepath.Join(root, c.NewRel())); err != nil {
			continue // nothing there
		}
		if key == fold(c.Rel) {
			continue // only the case changed: renaming onto itself is fine
		}
		if sources[key] {
			continue // another change moves that name away first
		}
		c.Problem = "exists"
	}
}

func fold(s string) string { return strings.ToLower(s) }

// Summary counts what a plan would do, for the tool's header and the confirm
// prompt.
type Summary struct {
	Total     int // candidates considered
	Renamed   int // changes that will be applied
	Conflicts int // changes held back by a problem
}

// Summarize counts a plan.
func Summarize(changes []Change) Summary {
	var s Summary
	s.Total = len(changes)
	for _, c := range changes {
		switch {
		case c.Problem != "":
			s.Conflicts++
		case c.Changed:
			s.Renamed++
		}
	}
	return s
}

// Applicable returns the changes that will actually run, deepest path first so a
// renamed directory never invalidates the paths of the entries inside it.
func Applicable(changes []Change) []Change {
	out := make([]Change, 0, len(changes))
	for _, c := range changes {
		if c.Changed && c.Problem == "" {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return depth(out[i].Rel) > depth(out[j].Rel)
	})
	return out
}

// depth counts the path segments in rel, so that "." (the root itself) sorts
// shallower than anything under it. Apply relies on the ordering to rename a
// directory only after everything inside it.
func depth(rel string) int {
	if rel == "" || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}
