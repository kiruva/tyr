package fileops

import (
	"context"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kiruva/tyr/internal/fsutil"
	"github.com/kiruva/tyr/internal/metastrip"
)

// Rotating an image is a change to its EXIF orientation and nothing else: the
// file is rewritten with that one value different, so no quality is lost and
// the rest of its metadata stays as it was. The turn lands on top of the
// orientation the image already has — a photo shown upright by its camera's
// "rotate 90°" and turned right once more ends up upside down, as it looks.

// rotateItem is one image a rotate job turns.
type rotateItem struct {
	path   string
	label  string
	walked bool // found inside a selected folder, not selected by name
}

// rotateItems expands the selection: a file is taken as it is, a folder is
// walked for images, leaving out dotfiles as a convert does.
func rotateItems(ctx context.Context, srcs []string) ([]rotateItem, error) {
	var items []rotateItem
	for _, src := range srcs {
		info, err := os.Stat(src)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			items = append(items, rotateItem{path: src, label: filepath.Base(src)})
			continue
		}
		parent := filepath.Dir(src)
		err = filepath.WalkDir(src, func(path string, d iofs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if path != src && strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || !IsImage(path) {
				return nil
			}
			rel, err := filepath.Rel(parent, path)
			if err != nil {
				return err
			}
			items = append(items, rotateItem{path: path, label: filepath.ToSlash(rel), walked: true})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

// rotate turns every image in job.Srcs by job.Turn, in place. It returns the
// images it turned, for an undo, and what is worth saying once it is done.
func rotate(ctx context.Context, job Job, r *reporter) ([]string, []string, error) {
	items, err := rotateItems(ctx, job.Srcs)
	if err != nil {
		return nil, nil, err
	}
	if len(items) == 0 {
		return nil, nil, fmt.Errorf("no images in the selection")
	}
	r.total = len(items)

	var rotated []string
	skipped := 0
	finish := func(err error) ([]string, []string, error) {
		var notes []string
		if len(rotated) > 0 {
			notes = append(notes, fmt.Sprintf("%s: %s", job.Turn, countImages(len(rotated))))
		}
		if skipped > 0 {
			notes = append(notes, fmt.Sprintf("%s skipped, no orientation to change (GIF, or not an image)", countImages(skipped)))
		}
		return rotated, notes, err
	}

	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		if err := rotateOne(it.path, job.Turn); err != nil {
			var nt notTurnable
			if !it.walked || !errors.As(err, &nt) {
				return finish(fmt.Errorf("%s: %w", it.label, err))
			}
			skipped++
		} else {
			rotated = append(rotated, it.path)
		}
		r.step(it.label)
	}
	return finish(nil)
}

// rotateOne turns one image. A link is followed, so the picture it points at
// is the one turned and the link stays a link.
func rotateOne(path string, turn metastrip.Turn) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Strip finds the orientation in each format the same way SetOrientation
	// will write it; what it would strip is not used.
	now := 0
	if res, err := metastrip.Strip(data, metastrip.Options{}); err == nil {
		now = res.Orientation
	}
	out, err := metastrip.SetOrientation(data, metastrip.Turned(now, turn))
	if err != nil {
		return notTurnable{err}
	}
	return fsutil.WriteFile(path, out, info.Mode().Perm())
}

// notTurnable is an image that cannot carry an orientation — a GIF, or a
// file that is not the image its name says — as opposed to one that could
// not be read or written. In a folder the first kind is skipped; the second
// stops the job, like any other failure to write.
type notTurnable struct{ err error }

func (e notTurnable) Error() string { return e.err.Error() }
func (e notTurnable) Unwrap() error { return e.err }

// UndoTurn is the turn that puts back what t did.
func UndoTurn(t metastrip.Turn) metastrip.Turn {
	switch t {
	case metastrip.RotateRight:
		return metastrip.RotateLeft
	case metastrip.RotateLeft:
		return metastrip.RotateRight
	}
	return t // a half turn and the flips undo themselves
}
