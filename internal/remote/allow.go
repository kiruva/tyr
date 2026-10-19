package remote

import (
	"archive/tar"
	"fmt"
	"io"
	"path"
	"runtime"
	"strconv"
	"strings"
)

// A download asks the host for some names and extracts whatever tar stream
// comes back. The host is not to be trusted with that: a compromised or hostile
// one can answer a request for report.pdf with .ssh/authorized_keys, and the
// local tar would write it — tar refuses ../ and absolute paths, but a relative
// path that was simply not asked for is fine by it. So the stream passes
// through allowOnly first, which re-encodes it entry by entry and stops at the
// first entry that is not one of the requested names or inside one of them.
//
// This is done here rather than by naming the members on the local tar's
// command line because the two tars disagree about what a name there means:
// bsdtar (macOS, Windows) treats it as a glob, so a.txt would also pull in
// whatever [a].txt was sent, and GNU tar does not.

// allowOnly copies the tar stream r to w, refusing any entry whose path is not
// one of names or below one of them. A hard link must point inside them too;
// a symlink's target is left alone, since tar itself will not write through a
// symlink it has just extracted.
func allowOnly(r io.Reader, w io.Writer, names []string) error {
	allowed := make(map[string]bool, len(names))
	for _, n := range names {
		allowed[n] = true
	}

	tr := tar.NewReader(r)
	tw := tar.NewWriter(w)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading the transfer: %w", err)
		}
		root := memberRoot(hdr.Name)
		if !allowed[root] {
			if appleDouble(root, allowed) {
				continue // tr.Next skips its content
			}
			return &unrequestedError{strconv.Quote(hdr.Name)}
		}
		if hdr.Typeflag == tar.TypeLink && !allowed[memberRoot(hdr.Linkname)] {
			return &unrequestedError{"a link to " + strconv.Quote(hdr.Linkname)}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
	}
	return tw.Close()
}

// appleDouble reports whether root is the ._name file macOS's tar packs beside
// a requested name to carry its extended attributes. A Mac host sends one
// unless told not to (Download tells it, but an older tar may not listen).
// It is dropped rather than refused: it is that file's own metadata, not
// something else, and what Upload leaves out going the other way.
func appleDouble(root string, allowed map[string]bool) bool {
	name, ok := strings.CutPrefix(root, "._")
	return ok && allowed[name]
}

// unrequestedError is the host sending something it was not asked for.
type unrequestedError struct{ what string }

func (e *unrequestedError) Error() string {
	return fmt.Sprintf("the host sent %s, which was not asked for — transfer stopped", e.what)
}

// memberRoot is the first component of a member's path, which is the name it
// was requested under — or "" for a path that could land outside the
// destination at all (absolute, or climbing out with ..). A leading ./ is
// dropped, since that is how the names are passed to the remote tar.
func memberRoot(name string) string {
	if runtime.GOOS == "windows" {
		// The local tar treats a backslash as a separator here, so the check
		// has to see it as one too.
		name = strings.ReplaceAll(name, `\`, "/")
	}
	if path.IsAbs(name) {
		return ""
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	root, _, _ := strings.Cut(clean, "/")
	return root
}
