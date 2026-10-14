package fileops

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// cleanJPEG is a small JPEG with no metadata at all.
func cleanJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegSegment(marker byte, payload []byte) []byte {
	n := len(payload) + 2
	return append([]byte{0xFF, marker, byte(n >> 8), byte(n)}, payload...)
}

// dirtyJPEG is cleanJPEG carrying a comment and, when orientation is set, an
// EXIF block holding only that.
func dirtyJPEG(t *testing.T, orientation int) []byte {
	t.Helper()
	clean := cleanJPEG(t)
	d := []byte{0xFF, 0xD8}
	if orientation > 0 {
		tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00")
		tiff[19] = byte(orientation)
		d = append(d, jpegSegment(0xE1, append([]byte("Exif\x00\x00"), tiff...))...)
	}
	d = append(d, jpegSegment(0xFE, []byte("secret comment"))...)
	return append(d, clean[2:]...)
}

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func imageJob(srcs []string, dest string) Job {
	return Job{Op: OpConvert, Srcs: srcs, Dest: dest, Convert: ConvertOpts{Images: true}}
}

// Into the other pane: the copy is clean, the original untouched, and the copy
// is what undo would remove.
func TestStripIntoOtherPane(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	dirty := dirtyJPEG(t, 0)
	in := filepath.Join(src, "photo.jpg")
	writeFile(t, in, dirty, 0o600)

	res := runConvert(t, &fakeTools{}, imageJob([]string{in}, dst), ConflictAsk)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	out := filepath.Join(dst, "photo.jpg")
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("secret comment")) {
		t.Error("the copy still carries the comment")
	}
	if orig, _ := os.ReadFile(in); !bytes.Equal(orig, dirty) {
		t.Error("the original was changed")
	}
	if info, _ := os.Stat(out); info.Mode().Perm() != 0o600 {
		t.Errorf("copy mode %v, want the original's 0600", info.Mode().Perm())
	}
	if !slices.Equal(res.Created, []string{out}) {
		t.Errorf("created %v, want %v", res.Created, []string{out})
	}
	if len(res.Notes) != 0 {
		t.Errorf("notes %v, want none", res.Notes)
	}
}

// A clean image still gets copied: the other pane asked for it.
func TestStripCopiesCleanImage(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	in := filepath.Join(src, "clean.jpg")
	writeFile(t, in, cleanJPEG(t), 0o644)
	if res := runConvert(t, &fakeTools{}, imageJob([]string{in}, dst), ConflictAsk); res.Err != nil {
		t.Fatal(res.Err)
	}
	if _, err := os.Stat(filepath.Join(dst, "clean.jpg")); err != nil {
		t.Error(err)
	}
}

// Same directory both sides: the original is rewritten, nothing is recorded
// for undo, a clean file is left alone, and the notes say what happened.
func TestStripInPlace(t *testing.T) {
	dir := t.TempDir()
	dirty := filepath.Join(dir, "dirty.jpg")
	clean := filepath.Join(dir, "clean.jpg")
	writeFile(t, dirty, dirtyJPEG(t, 6), 0o640)
	cleanBytes := cleanJPEG(t)
	writeFile(t, clean, cleanBytes, 0o644)
	before, _ := os.Stat(clean)

	res := runConvert(t, &fakeTools{}, imageJob([]string{dirty, clean}, dir), ConflictAsk)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Created) != 0 {
		t.Errorf("created %v, want nothing: an in-place strip cannot be undone", res.Created)
	}
	got, _ := os.ReadFile(dirty)
	if bytes.Contains(got, []byte("secret comment")) {
		t.Error("the comment survived")
	}
	if info, _ := os.Stat(dirty); info.Mode().Perm() != 0o640 {
		t.Errorf("mode %v, want 0640 kept", info.Mode().Perm())
	}
	if after, _ := os.Stat(clean); !after.ModTime().Equal(before.ModTime()) {
		t.Error("a clean file was rewritten")
	}
	notes := strings.Join(res.Notes, "; ")
	for _, want := range []string{"stripped 1 image in place", "1 image already clean", "orientation"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes %q lack %q", notes, want)
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".*tyr-*"))
	if len(leftovers) != 0 {
		t.Errorf("temporary files left: %v", leftovers)
	}
}

// Keeping orientation keeps the note away.
func TestStripKeepOrientation(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "p.jpg")
	writeFile(t, in, dirtyJPEG(t, 6), 0o644)
	job := imageJob([]string{in}, t.TempDir())
	job.Convert.Strip.KeepOrientation = true
	res := runConvert(t, &fakeTools{}, job, ConflictAsk)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Notes) != 0 {
		t.Errorf("notes %v, want none", res.Notes)
	}
}

// A folder of audio and images converts both kinds; something named like an
// image that is not one is skipped and counted, not a failure.
func TestConvertMixedFolder(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	album := filepath.Join(src, "album")
	writeFile(t, filepath.Join(album, "01.mp3"), []byte("x"), 0o644)
	writeFile(t, filepath.Join(album, "cover.jpg"), dirtyJPEG(t, 0), 0o644)
	writeFile(t, filepath.Join(album, "fake.png"), []byte("not a png"), 0o644)
	writeFile(t, filepath.Join(album, "notes.txt"), []byte("x"), 0o644)

	job := Job{Op: OpConvert, Srcs: []string{album}, Dest: dst, Convert: ConvertOpts{
		Format: AudioOgg, Quality: 1, Encoder: "libvorbis", Images: true,
	}}
	f := &fakeTools{}
	res := runConvert(t, f, job, ConflictAsk)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	for _, want := range []string{"album/01.ogg", "album/cover.jpg"} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Errorf("%s missing: %v", want, err)
		}
	}
	for _, not := range []string{"album/fake.png", "album/notes.txt"} {
		if _, err := os.Stat(filepath.Join(dst, not)); err == nil {
			t.Errorf("%s should not have been written", not)
		}
	}
	if n := len(ffmpegRuns(f)); n != 1 {
		t.Errorf("ran ffmpeg %d times, want 1", n)
	}
	if !strings.Contains(strings.Join(res.Notes, ";"), "1 image skipped") {
		t.Errorf("notes %v lack the skipped fake.png", res.Notes)
	}
}

// Images alone need no ffmpeg; audio picked alongside without an encoder is
// passed over.
func TestConvertImagesWithoutEncoder(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	img, song := filepath.Join(src, "a.jpg"), filepath.Join(src, "b.mp3")
	writeFile(t, img, dirtyJPEG(t, 0), 0o644)
	writeFile(t, song, []byte("x"), 0o644)
	f := &fakeTools{}
	res := runConvert(t, f, imageJob([]string{img, song}, dst), ConflictAsk)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if n := len(ffmpegRuns(f)); n != 0 {
		t.Errorf("ran ffmpeg %d times with no encoder", n)
	}
	if _, err := os.Stat(filepath.Join(dst, "a.jpg")); err != nil {
		t.Error(err)
	}
}

// An unsupported image picked by name is an error, not a silent skip.
func TestStripNamedUnsupported(t *testing.T) {
	src := t.TempDir()
	in := filepath.Join(src, "x.jpg")
	writeFile(t, in, []byte("nope"), 0o644)
	if res := runConvert(t, &fakeTools{}, imageJob([]string{in}, t.TempDir()), ConflictAsk); res.Err == nil {
		t.Error("a non-image named .jpg converted without complaint")
	}
}

// A name already taken in the other pane is asked about like a copy's.
func TestStripConflict(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	in := filepath.Join(src, "p.jpg")
	writeFile(t, in, dirtyJPEG(t, 0), 0o644)
	writeFile(t, filepath.Join(dst, "p.jpg"), []byte("keep me"), 0o644)

	res := runConvert(t, &fakeTools{}, imageJob([]string{in}, dst), ConflictSkip)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "p.jpg")); string(got) != "keep me" {
		t.Error("skip overwrote the file")
	}

	res = runConvert(t, &fakeTools{}, imageJob([]string{in}, dst), ConflictKeepBoth)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Created) != 1 || res.Created[0] == filepath.Join(dst, "p.jpg") {
		t.Errorf("keep both created %v, want a fresh name", res.Created)
	}
}

func TestIsImage(t *testing.T) {
	for name, want := range map[string]bool{
		"a.JPG": true, "b.jpeg": true, "c.png": true, "d.gif": true, "e.webp": true,
		"f.heic": false, "g.tiff": false, "h.mp3": false, "jpg": false,
	} {
		if got := IsImage(name); got != want {
			t.Errorf("IsImage(%q) = %v, want %v", name, got, want)
		}
	}
}
