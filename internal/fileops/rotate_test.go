package fileops

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kiruva/tyr/internal/metastrip"
)

func orientationOn(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := metastrip.Strip(data, metastrip.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r.Orientation
}

func rotateJob(srcs []string, turn metastrip.Turn) Job {
	return Job{Op: OpRotate, Srcs: srcs, Turn: turn}
}

// A turn lands on top of the orientation the image already has, in place,
// and keeps the file's mode; the rest of its metadata is not touched.
func TestRotateInPlace(t *testing.T) {
	dir := t.TempDir()
	tagged := filepath.Join(dir, "tagged.jpg")
	plain := filepath.Join(dir, "plain.jpg")
	writeFile(t, tagged, dirtyJPEG(t, 6), 0o640)
	writeFile(t, plain, dirtyJPEG(t, 0), 0o644)
	mode := modeOf(t, tagged) // 0640 here, whatever Windows makes of it there

	res := runJob(t, rotateJob([]string{tagged, plain}, metastrip.RotateRight))
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if got := orientationOn(t, tagged); got != 3 {
		t.Errorf("tagged: orientation %d, want 3 (90° + 90°)", got)
	}
	if got := orientationOn(t, plain); got != 6 {
		t.Errorf("plain: orientation %d, want 6", got)
	}
	if m := modeOf(t, tagged); m != mode {
		t.Errorf("mode %v, want %v as before", m, mode)
	}
	data, _ := os.ReadFile(plain)
	if !bytes.Contains(data, []byte("secret comment")) {
		t.Error("the comment went missing: rotating is not stripping")
	}
	if len(res.Rotated) != 2 {
		t.Errorf("Rotated = %v, want both files for the undo", res.Rotated)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "rotate right: 2 images") {
		t.Errorf("notes %v", res.Notes)
	}
}

// A folder is walked for images; what is not one — a GIF has no EXIF, a .jpg
// can be anything — is counted and skipped, and dotfiles are left alone.
func TestRotateFolder(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "album")
	writeFile(t, filepath.Join(root, "a.jpg"), dirtyJPEG(t, 1), 0o644)
	writeFile(t, filepath.Join(root, "sub", "b.jpg"), dirtyJPEG(t, 0), 0o644)
	writeFile(t, filepath.Join(root, "fake.jpg"), []byte("nope"), 0o644)
	writeFile(t, filepath.Join(root, "anim.gif"), []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), 0o644)
	writeFile(t, filepath.Join(root, "._a.jpg"), []byte("resource fork"), 0o644)
	writeFile(t, filepath.Join(root, "notes.txt"), []byte("text"), 0o644)

	res := runJob(t, rotateJob([]string{root}, metastrip.FlipHorizontal))
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	for _, p := range []string{"a.jpg", "sub/b.jpg"} {
		if got := orientationOn(t, filepath.Join(root, filepath.FromSlash(p))); got != 2 {
			t.Errorf("%s: orientation %d, want 2", p, got)
		}
	}
	notes := strings.Join(res.Notes, " · ")
	if !strings.Contains(notes, "flip horizontally: 2 images") || !strings.Contains(notes, "2 images skipped") {
		t.Errorf("notes %q", notes)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "._a.jpg")); string(data) != "resource fork" {
		t.Error("a dotfile was touched")
	}
}

// Picked by name, something that cannot be turned is an error that says why.
func TestRotateNamedGIF(t *testing.T) {
	dir := t.TempDir()
	gif := filepath.Join(dir, "anim.gif")
	writeFile(t, gif, []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), 0o644)
	res := runJob(t, rotateJob([]string{gif}, metastrip.RotateRight))
	if res.Err == nil || !strings.Contains(res.Err.Error(), "GIF") {
		t.Errorf("err = %v, want one naming the GIF", res.Err)
	}
}

// A link is followed: the picture it points at is the one turned, and the
// link stays a link.
func TestRotateFollowsALink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.jpg")
	writeFile(t, real, dirtyJPEG(t, 0), 0o644)
	link := filepath.Join(dir, "link.jpg")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if res := runJob(t, rotateJob([]string{link}, metastrip.Rotate180)); res.Err != nil {
		t.Fatal(res.Err)
	}
	if got := orientationOn(t, real); got != 3 {
		t.Errorf("orientation %d, want 3", got)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Error("the link was replaced by a file")
	}
}

// The opposite turn puts every image back as it was — which is what undo
// runs.
func TestRotateUndoTurn(t *testing.T) {
	for _, tr := range []metastrip.Turn{metastrip.RotateRight, metastrip.RotateLeft, metastrip.Rotate180, metastrip.FlipHorizontal, metastrip.FlipVertical} {
		for v := 1; v <= 8; v++ {
			if back := metastrip.Turned(metastrip.Turned(v, tr), UndoTurn(tr)); back != v {
				t.Errorf("%v from %d, then UndoTurn: %d", tr, v, back)
			}
		}
	}
}

func TestRotateCancelled(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jpg")
	writeFile(t, p, dirtyJPEG(t, 0), 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var res Result
	for msg := range Run(ctx, rotateJob([]string{p}, metastrip.RotateRight)) {
		if r, ok := msg.(Result); ok {
			res = r
		}
	}
	if res.Err == nil {
		t.Error("a cancelled job reported success")
	}
	if got := orientationOn(t, p); got != 0 {
		t.Errorf("orientation %d after a cancel before the start", got)
	}
}

// In a folder, an image that cannot be written is a failure, not a skip.
func TestRotateFolderUnwritableFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a folder this user cannot write to")
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "album")
	writeFile(t, filepath.Join(root, "a.jpg"), dirtyJPEG(t, 0), 0o644)
	if err := os.Chmod(root, 0o555); err != nil { // no temp file beside it
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if res := runJob(t, rotateJob([]string{root}, metastrip.RotateRight)); res.Err == nil {
		t.Error("an image that could not be written was counted as skipped")
	}
}
