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
)

func (k Kind) String() string {
	switch k {
	case Same:
		return "same"
	case Differs:
		return "differs"
	case OnlyLeft:
		return "left only"
	default:
		return "right only"
	}
}

// Side names one of the two trees.
type Side int

const (
	Neither Side = iota
	Left
	Right
)

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
}

// Options tunes a comparison.
type Options struct {
	Recursive bool // descend into subdirectories
	Hidden    bool // include dotfiles and dot-directories
	ByContent bool // read same-size files rather than trusting timestamps
	Max       int  // cap on reported pairs; 0 means DefaultMax
}

// Walk compares the two trees and returns the pairs, sorted by path. The bool
// reports that the cap was reached and the answer is partial.
func Walk(ctx context.Context, leftRoot, rightRoot string, o Options) ([]Pair, bool, error) {
	if o.Max <= 0 {
		o.Max = DefaultMax
	}

	left, err := collect(ctx, leftRoot, o)
	if err != nil {
		return nil, false, err
	}
	right, err := collect(ctx, rightRoot, o)
	if err != nil {
		return nil, false, err
	}

	rels := make([]string, 0, len(left)+len(right))
	for rel := range left {
		rels = append(rels, rel)
	}
	for rel := range right {
		if _, both := left[rel]; !both {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)

	truncated := false
	if len(rels) > o.Max {
		rels, truncated = rels[:o.Max], true
	}

	pairs := make([]Pair, 0, len(rels))
	for _, rel := range rels {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		pairs = append(pairs, classify(rel, left[rel], right[rel], leftRoot, rightRoot, o))
	}
	return pairs, truncated, nil
}

// classify decides what one path's two sides amount to.
func classify(rel string, l, r *Info, leftRoot, rightRoot string, o Options) Pair {
	p := Pair{Rel: rel, Left: l, Right: r}

	switch {
	case l != nil && r == nil:
		p.Kind, p.Newer = OnlyLeft, Left
		return p
	case l == nil && r != nil:
		p.Kind, p.Newer = OnlyRight, Right
		return p
	}

	if l.Size == r.Size {
		if o.ByContent {
			equal, err := sameContent(filepath.Join(leftRoot, rel), filepath.Join(rightRoot, rel))
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

// sameContent reports whether two files hold the same bytes.
func sameContent(a, b string) (bool, error) {
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
func Counts(pairs []Pair) (same, differs, onlyLeft, onlyRight int) {
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
		}
	}
	return same, differs, onlyLeft, onlyRight
}
