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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// DefaultMax is the result cap applied when Options.Max is zero.
const DefaultMax = 500

// maxContentBytes is the largest file whose contents are searched. Anything
// bigger is matched by name only: reading it would stall the search for a hit
// nobody is waiting for.
const maxContentBytes = 32 << 20

// maxLineBytes is how much of one line the content scanner holds. A minified
// bundle is one enormous line; its first megabyte is searched and the rest is
// read past, which is cheaper than growing without limit.
const maxLineBytes = 1 << 20

// previewBytes is how much of a matching line a result keeps. Up to 500 hits
// are held at once, and a hit in a minified bundle would otherwise carry the
// whole megabyte line just to show one row of it.
const previewBytes = 256

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
	// A malformed glob would match nothing, which looks like a search that
	// found nothing. It is said instead.
	if strings.ContainsAny(o.Name, "*?[") {
		if _, err := filepath.Match(o.Name, ""); err != nil {
			return nil, false, fmt.Errorf("name pattern %q: %w", o.Name, err)
		}
	}
	shown, err := filepath.Abs(o.Root)
	if err != nil {
		return nil, false, err
	}
	// A root reached through a symlink is searched as the folder it points
	// at, since WalkDir does not follow a link; the results keep the path
	// the search was started from.
	root, err := filepath.EvalSymlinks(shown)
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
		res.Path = filepath.Join(shown, res.Rel)
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

	br := bufio.NewReaderSize(f, 64*1024)
	for i := 1; ; i++ {
		line, err := readLine(br)
		hay := line
		if !caseSensitive {
			hay = strings.ToLower(hay)
		}
		if at := strings.Index(hay, needle); at >= 0 {
			return i, preview(line, at), true
		}
		if err != nil {
			return 0, "", false
		}
	}
}

// readLine returns the next line without its line ending, cut to maxLineBytes;
// the rest of a longer line is read and dropped so the next call starts on the
// line after it. The error is io.EOF after the last line, or a read failure.
func readLine(br *bufio.Reader) (string, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if room := maxLineBytes - len(line); room > 0 {
			line = append(line, chunk[:min(len(chunk), room)]...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		line = bytes.TrimSuffix(line, []byte("\n"))
		line = bytes.TrimSuffix(line, []byte("\r"))
		return string(line), err
	}
}

// preview cuts a matching line down to previewBytes, starting a little before
// the match so the match itself is in what is shown. at is where the needle was
// found in the (possibly lower-cased) haystack; lower-casing can change a
// line's length, so it is only a guide, clamped and moved back onto a rune
// boundary.
func preview(line string, at int) string {
	line = strings.TrimSpace(line)
	if len(line) <= previewBytes {
		return line
	}
	start := min(max(at-previewBytes/4, 0), len(line))
	for start > 0 && !utf8.RuneStart(line[start]) {
		start--
	}
	end := min(start+previewBytes, len(line))
	for end < len(line) && !utf8.RuneStart(line[end]) {
		end--
	}
	out := line[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(line) {
		out += "…"
	}
	return out
}
