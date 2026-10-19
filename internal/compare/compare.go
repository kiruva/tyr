// Package compare pairs up the contents of two directory trees: what is only on
// one side, what is on both but different, and what matches.
//
// It is what makes a dual-pane file manager more than two lists side by side.
// The comparison is over files — a directory is a path, and a directory that
// exists on one side only shows up as the files inside it, which is what has to
// be copied to make the two agree.
package compare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultMax caps how many pairs a comparison reports.
const DefaultMax = 5000

// sameWindow is how far two modification times may be apart and still count as
// the same moment. Filesystems disagree about sub-second precision, and a file
// copied by tyr itself keeps only what the destination filesystem can hold.
const sameWindow = 2 * time.Second

// compareChunk is the read size for a byte-for-byte comparison.
const compareChunk = 64 * 1024

// Kind is how one path differs between the two sides.
type Kind int

const (
	Same      Kind = iota // present on both, and equal
	Differs               // present on both, and not equal
	OnlyLeft              // present on the left only
	OnlyRight             // present on the right only
	Conflict              // cannot be copied either way without losing something; Note says why
)

func (k Kind) String() string {
	switch k {
	case Same:
		return "same"
	case Differs:
		return "differs"
	case OnlyLeft:
		return "left only"
	case OnlyRight:
		return "right only"
	default:
		return "conflict"
	}
}

// Side names one of the two trees.
type Side int

const (
	Neither Side = iota
	Left
	Right
)

func (s Side) String() string {
	switch s {
	case Left:
		return "left"
	case Right:
		return "right"
	default:
		return "neither"
	}
}

// Info is what a comparison knows about one file.
type Info struct {
	Size    int64
	ModTime time.Time
}

// Pair is one path, as it stands on both sides.
type Pair struct {
	Rel   string // path relative to each root
	Kind  Kind
	Newer Side  // which side is the more recent, when they differ
	Left  *Info // nil when the left side has nothing there
	Right *Info
	Note  string // for a Conflict, what is in the way
}

// Options tunes a comparison.
type Options struct {
	Recursive bool // descend into subdirectories
	Hidden    bool // include dotfiles and dot-directories
	ByContent bool // read same-size files rather than trusting timestamps
	Max       int  // cap on reported pairs; 0 means DefaultMax
}

// tree is one side of a comparison: where it is and the files found in it.
type tree struct {
	side  Side
	root  string
	files map[string]*Info
	fold  map[string][]string // lower-cased path → the paths that spell it
	ways  map[string]string   // folder path → what blocks copying into it ("" for nothing)
}

func newTree(ctx context.Context, side Side, root string, o Options) (*tree, error) {
	// A root reached through a symlink is compared as the folder it points
	// at: WalkDir does not follow a link, so the walk would find nothing.
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	files, err := collect(ctx, root, o)
	if err != nil {
		return nil, err
	}
	t := &tree{side: side, root: root, files: files, fold: map[string][]string{}, ways: map[string]string{}}
	for rel := range files {
		low := strings.ToLower(rel)
		t.fold[low] = append(t.fold[low], rel)
	}
	return t, nil
}

// Walk compares the two trees and returns the pairs, sorted by path. The bool
// reports that the cap was reached and the answer is partial.
func Walk(ctx context.Context, leftRoot, rightRoot string, o Options) ([]Pair, bool, error) {
	if o.Max <= 0 {
		o.Max = DefaultMax
	}

	left, err := newTree(ctx, Left, leftRoot, o)
	if err != nil {
		return nil, false, err
	}
	right, err := newTree(ctx, Right, rightRoot, o)
	if err != nil {
		return nil, false, err
	}

	rels := make([]string, 0, len(left.files)+len(right.files))
	for rel := range left.files {
		rels = append(rels, rel)
	}
	for rel := range right.files {
		if _, both := left.files[rel]; !both {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)

	var (
		pairs     = make([]Pair, 0, min(len(rels), o.Max))
		taken     = map[string]bool{} // one-sided paths already folded into another row
		truncated bool
	)
	for _, rel := range rels {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if taken[rel] {
			continue
		}
		if len(pairs) == o.Max {
			truncated = true
			break
		}

		l, r := left.files[rel], right.files[rel]
		switch {
		case l != nil && r != nil:
			pairs = append(pairs, classify(ctx, rel, l, r, left.root, right.root, o))
		case l != nil:
			pairs = append(pairs, oneSided(rel, left, right, taken))
		default:
			pairs = append(pairs, oneSided(rel, right, left, taken))
		}
	}
	return pairs, truncated, nil
}

// oneSided builds the row for a file that only from has, which is a copy to
// the other side — unless something on the other side is in the way of that
// copy. Only the disk can say: a folder the walk skipped, a link it did not
// follow, a name a case-insensitive volume already holds under another case.
func oneSided(rel string, from, to *tree, taken map[string]bool) Pair {
	p := Pair{Rel: rel, Kind: OnlyLeft, Newer: from.side}
	if from.side == Right {
		p.Kind = OnlyRight
	}
	p.setInfo(from.side, from.files[rel])

	note := to.blocked(rel)
	if note == "" {
		return p
	}
	p.Kind, p.Newer, p.Note = Conflict, Neither, note

	// Foo.txt here and foo.txt there are one file on that volume. When the
	// other spelling is a one-sided row of its own, it becomes this row's
	// other half rather than a second copy that would overwrite this one.
	for _, other := range to.fold[strings.ToLower(rel)] {
		if other == rel || from.files[other] != nil {
			continue
		}
		p.setInfo(to.side, to.files[other])
		p.Note = fmt.Sprintf("the %s side has this as %s — rename one of them", to.side, other)
		taken[other] = true
		break
	}
	return p
}

func (p *Pair) setInfo(side Side, info *Info) {
	if side == Left {
		p.Left = info
	} else {
		p.Right = info
	}
}

// blocked says what in this tree is in the way of a copy of rel landing in it,
// or "" when nothing is: every folder on the way is a real folder or not there
// yet, and nothing is there by that name.
func (t *tree) blocked(rel string) string {
	if dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel))); dir != "." {
		if note := t.way(dir); note != "" {
			return note
		}
	}
	info, err := os.Lstat(filepath.Join(t.root, filepath.FromSlash(rel)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ""
	case err != nil:
		return fmt.Sprintf("cannot look on the %s side: %v", t.side, err)
	case info.IsDir():
		return fmt.Sprintf("the %s side has a folder by this name", t.side)
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Sprintf("the %s side has a link by this name", t.side)
	default:
		return fmt.Sprintf("the %s side already has something by this name", t.side)
	}
}

// way is blocked for the folders a copy of a file goes through, outermost
// first, remembered because a tree's files share them. A symlinked folder is
// in the way too: the copy would be written wherever it points, outside the
// tree being synced.
func (t *tree) way(dir string) string {
	if note, seen := t.ways[dir]; seen {
		return note
	}
	note := ""
	if parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(dir))); parent != "." {
		note = t.way(parent)
	}
	if note == "" {
		info, err := os.Lstat(filepath.Join(t.root, filepath.FromSlash(dir)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			note = fmt.Sprintf("cannot look on the %s side: %v", t.side, err)
		case info.Mode()&os.ModeSymlink != 0:
			note = fmt.Sprintf("%s on the %s side is a link", dir, t.side)
		case !info.IsDir():
			note = fmt.Sprintf("%s on the %s side is a file, not a folder", dir, t.side)
		}
	}
	t.ways[dir] = note
	return note
}

// classify decides what one path's two sides amount to.
func classify(ctx context.Context, rel string, l, r *Info, leftRoot, rightRoot string, o Options) Pair {
	p := Pair{Rel: rel, Left: l, Right: r}

	if l.Size == r.Size {
		if o.ByContent {
			equal, err := sameContent(ctx, filepath.Join(leftRoot, rel), filepath.Join(rightRoot, rel))
			if err == nil && equal {
				p.Kind = Same
				return p
			}
			if err == nil {
				p.Kind, p.Newer = Differs, newerOf(l, r)
				return p
			}
			// Unreadable: fall through to the timestamp comparison rather than
			// calling two files different because one of them could not be read.
		}
		if within(l.ModTime, r.ModTime, sameWindow) {
			p.Kind = Same
			return p
		}
	}

	p.Kind, p.Newer = Differs, newerOf(l, r)
	return p
}

func newerOf(l, r *Info) Side {
	switch {
	case l.ModTime.After(r.ModTime):
		return Left
	case r.ModTime.After(l.ModTime):
		return Right
	default:
		return Neither
	}
}

func within(a, b time.Time, d time.Duration) bool {
	diff := a.Sub(b)
	if diff < 0 {
		diff = -diff
	}
	return diff <= d
}

// collect indexes the regular files under root by their path relative to it.
func collect(ctx context.Context, root string, o Options) (map[string]*Info, error) {
	out := map[string]*Info{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable corner is skipped: half a comparison beats none.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}

		name := d.Name()
		if !o.Hidden && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !o.Recursive {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // symlinks and devices are not something to sync
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		out[filepath.ToSlash(rel)] = &Info{Size: info.Size(), ModTime: info.ModTime()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// sameContent reports whether two files hold the same bytes. It gives up when
// ctx is cancelled, between chunks: two big files are a long read.
func sameContent(ctx context.Context, a, b string) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()

	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()

	bufA := make([]byte, compareChunk)
	bufB := make([]byte, compareChunk)
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		na, errA := io.ReadFull(fa, bufA)
		nb, errB := io.ReadFull(fb, bufB)
		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		if errA != nil || errB != nil {
			doneA := errA == io.EOF || errA == io.ErrUnexpectedEOF
			doneB := errB == io.EOF || errB == io.ErrUnexpectedEOF
			if doneA && doneB {
				return true, nil
			}
			if errA != nil && !doneA {
				return false, errA
			}
			if errB != nil && !doneB {
				return false, errB
			}
			return false, nil
		}
	}
}

// Counts summarizes a set of pairs for a header line.
func Counts(pairs []Pair) (same, differs, onlyLeft, onlyRight, conflicts int) {
	for _, p := range pairs {
		switch p.Kind {
		case Same:
			same++
		case Differs:
			differs++
		case OnlyLeft:
			onlyLeft++
		case OnlyRight:
			onlyRight++
		case Conflict:
			conflicts++
		}
	}
	return same, differs, onlyLeft, onlyRight, conflicts
}
