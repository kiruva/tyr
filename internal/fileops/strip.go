package fileops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kiruva/tyr/internal/fsutil"
	"github.com/kiruva/tyr/internal/metastrip"
)

// An image converts into a clean copy of itself: the same picture, byte for
// byte, with its metadata — EXIF and GPS, XMP, IPTC, comments, C2PA — left
// behind. It lands in the other pane under the same name, like every other
// conversion. When the other pane is the same directory the copy is the
// original, and the strip happens in place: the dialog asks about that once,
// up front, because nothing can put the metadata back afterwards.

// imageExts are the files a walk hands to metastrip. The contents decide in the
// end: a .jpg that is not a JPEG is skipped like any other stranger.
var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".jpe": true, ".png": true, ".apng": true,
	".gif": true, ".webp": true,
}

// IsImage reports whether name looks like an image tyr can strip.
func IsImage(name string) bool { return imageExts[strings.ToLower(filepath.Ext(name))] }

// ImageTarget is where an image lands when converted into dir: the same name.
func ImageTarget(src, dir string) string { return filepath.Join(dir, filepath.Base(src)) }

// stripTally counts what is worth saying once the job is done.
type stripTally struct {
	inPlace     int // originals rewritten without their metadata
	clean       int // already clean in place, left alone
	unsupported int // found in a folder, not an image metastrip handles
	rotated     int // lost an EXIF orientation other than "normal"
}

func (t stripTally) notes() []string {
	var out []string
	if t.inPlace > 0 {
		out = append(out, fmt.Sprintf("stripped %s in place", countImages(t.inPlace)))
	}
	if t.clean > 0 {
		out = append(out, fmt.Sprintf("%s already clean", countImages(t.clean)))
	}
	if t.unsupported > 0 {
		out = append(out, fmt.Sprintf("%s skipped, not a supported image", countImages(t.unsupported)))
	}
	if t.rotated > 0 {
		out = append(out, fmt.Sprintf("%s lost its orientation and may show rotated", countImages(t.rotated)))
	}
	return out
}

func countImages(n int) string {
	if n == 1 {
		return "1 image"
	}
	return fmt.Sprintf("%d images", n)
}

// stripOne writes the clean copy of one image, returning where it landed or ""
// when nothing new was made: an in-place strip, an image already clean, a skip.
func stripOne(it convertItem, job Job, rv *resolver, tally *stripTally) (string, error) {
	data, err := os.ReadFile(it.src)
	if err != nil {
		return "", err
	}
	res, err := metastrip.Strip(data, job.Convert.Strip)
	if err != nil {
		if errors.Is(err, metastrip.ErrUnsupported) && it.walked {
			tally.unsupported++
			return "", nil
		}
		return "", fmt.Errorf("%s: %w", it.label, err)
	}
	info, err := os.Stat(it.src)
	if err != nil {
		return "", err
	}
	mode := info.Mode().Perm()
	if res.Orientation > 1 && !job.Convert.Strip.KeepOrientation && len(res.Removed) > 0 {
		tally.rotated++
	}

	dst := it.dst
	if OverwritesSource(it.src, dst) {
		if len(res.Removed) == 0 {
			tally.clean++
			return "", nil
		}
		// A link is followed: the picture it points at is the one to clean.
		if real, err := filepath.EvalSymlinks(it.src); err == nil {
			dst = real
		}
		if err := fsutil.WriteFile(dst, res.Data, mode); err != nil {
			return "", err
		}
		tally.inPlace++
		return "", nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if exists(dst) {
		action, err := rv.resolve(it.src, dst)
		if err != nil {
			return "", err
		}
		switch action {
		case ConflictSkip:
			return "", nil
		case ConflictKeepBoth:
			if dst, err = freeName(dst); err != nil {
				return "", err
			}
		}
	}
	// A copy with nothing to remove is still a copy: the other pane asked for
	// the image, and gets it.
	if err := fsutil.WriteFile(dst, res.Data, mode); err != nil {
		return "", err
	}
	return dst, nil
}
