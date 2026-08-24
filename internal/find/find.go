// Package find walks a directory tree looking for entries by name and, when
// asked, by what is inside them.
//
// A search is a plain synchronous walk: the caller runs it off the UI thread
// and cancels it through the context. Results are capped, because a pattern
// like "*" over a home directory has no useful end — the cap is reported back
// so the caller can say the list is not the whole story.
package find

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DefaultMax is the result cap applied when Options.Max is zero.
const DefaultMax = 500

// maxContentBytes is the largest file whose contents are searched. Anything
// bigger is matched by name only: reading it would stall the search for a hit
// nobody is waiting for.
const maxContentBytes = 32 << 20

// maxLineBytes is the longest line the content scanner will hold. A minified
// bundle is one enormous line, and refusing to buffer it is cheaper than
// growing without limit.
const maxLineBytes = 1 << 20

// Options describes one search.
type Options struct {
	Root          string // directory to walk
	Name          string // glob (with * ? [) or substring; "" matches every name
	Content       string // text that must appear inside the file; "" skips reading
	CaseSensitive bool   // applies to both Name and Content
	Hidden        bool   // descend into and match dotfiles
	Max           int    // result cap; 0 means DefaultMax
}

// Result is one match.
type Result struct {
	Path  string // absolute path
	Rel   string // path relative to the search root, for display
	IsDir bool
	Size  int64
	Line  int    // line the content matched on (0 for a name-only match)
	Text  string // that line, trimmed, for a preview
}

// ErrEmptyQuery is returned when a search asks for nothing at all.
var ErrEmptyQuery = errors.New("enter a name pattern or some text to look for")

// Run walks o.Root and returns the matches. The bool reports whether the cap
// was reached, meaning there may be more matches than the ones returned.
func Run(ctx context.Context, o Options) ([]Result, bool, error) {
	o.Name = strings.TrimSpace(o.Name)
	if o.Name == "" && o.Content == "" {
		return nil, false, ErrEmptyQuery
	}
	if o.Max <= 0 {
		o.Max = DefaultMax
	}
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return nil, false, err
	}

	var (
		out       []Result
		truncated bool
	)

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory is normal on a real filesystem; walk on.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if path == root {
			return nil
		}

		if !o.Hidden && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		res, ok := o.match(path, root, d)
		if !ok {
			return nil
		}
		out = append(out, res)
		if len(out) >= o.Max {
			truncated = true
			return fs.SkipAll
		}
		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		return out, truncated, walkErr
	}
	return out, truncated, ctx.Err()
}

// match decides whether one entry is a hit, and builds the result if it is.
func (o Options) match(path, root string, d fs.DirEntry) (Result, bool) {
	if !MatchName(d.Name(), o.Name, o.CaseSensitive) {
		return Result{}, false
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	res := Result{Path: path, Rel: rel, IsDir: d.IsDir()}

	if o.Content == "" {
		if info, err := d.Info(); err == nil {
			res.Size = info.Size()
		}
		return res, true
	}

	// Content searches are about files: a directory has nothing to read.
	if d.IsDir() {
		return Result{}, false
	}
	info, err := d.Info()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxContentBytes {
		return Result{}, false
	}
	res.Size = info.Size()

	line, text, ok := grep(path, o.Content, o.CaseSensitive)
	if !ok {
		return Result{}, false
	}
	res.Line, res.Text = line, text
	return res, true
}

// MatchName reports whether name satisfies pattern. A pattern with a wildcard
// in it is a glob over the whole name; anything else matches as a substring,
// which is what typing three letters into a search box usually means.
func MatchName(name, pattern string, caseSensitive bool) bool {
	if pattern == "" {
		return true
	}
	if !caseSensitive {
		name, pattern = strings.ToLower(name), strings.ToLower(pattern)
	}
	if strings.ContainsAny(pattern, "*?[") {
		ok, err := filepath.Match(pattern, name)
		return err == nil && ok
	}
	return strings.Contains(name, pattern)
}

// grep returns the first line of path containing needle. A file that looks
// binary is skipped: a match inside one is not something to preview.
func grep(path, needle string, caseSensitive bool) (int, string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", false
	}
	defer f.Close()

	head := make([]byte, 8000)
	n, _ := f.Read(head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return 0, "", false
	}
	if _, err := f.Seek(0, 0); err != nil {
		return 0, "", false
	}

	if !caseSensitive {
		needle = strings.ToLower(needle)
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for i := 1; sc.Scan(); i++ {
		line := sc.Text()
		hay := line
		if !caseSensitive {
			hay = strings.ToLower(hay)
		}
		if strings.Contains(hay, needle) {
			return i, strings.TrimSpace(line), true
		}
	}
	return 0, "", false
}
