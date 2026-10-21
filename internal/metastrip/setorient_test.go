package metastrip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/jpeg"
	"image/png"
	"testing"
)

// orientationOf reads an image's orientation back the way Strip finds it.
func orientationOf(t *testing.T, b []byte) int {
	t.Helper()
	r, err := Strip(b, Options{})
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	return r.Orientation
}

// differing counts the bytes two equal-length slices differ in.
func differing(a, b []byte) int {
	n := 0
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return n
}

// exifWithMake is a little-endian TIFF whose IFD0 has Make and Model but no
// orientation, with Make's text stored out of line — the offset an edit that
// moved things around would break.
func exifWithMake() []byte {
	b := []byte{'I', 'I', 42, 0, 8, 0, 0, 0}
	b = append(b, 2, 0) // two entries
	entry := func(tag, typ uint16, count, value uint32) {
		b = binary.LittleEndian.AppendUint16(b, tag)
		b = binary.LittleEndian.AppendUint16(b, typ)
		b = binary.LittleEndian.AppendUint32(b, count)
		b = binary.LittleEndian.AppendUint32(b, value)
	}
	entry(0x010F, 2, 10, 8+2+2*12+4) // Make, ASCII, out of line right after the IFD
	entry(0x0110, 2, 4, 'Z'|'1'<<8|'0'<<16)
	b = append(b, 0, 0, 0, 0) // no next IFD
	return append(b, "Brandname\x00"...)
}

func TestWithOrientationPatchesInPlace(t *testing.T) {
	for _, in := range [][]byte{
		orientationTIFF(6), // big-endian SHORT
		append([]byte("Exif\x00\x00"), orientationTIFF(6)...),
		{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 4, 0, 0, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0}, // LONG
		{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0},
	} {
		out, err := withOrientation(in, 3)
		if err != nil {
			t.Fatalf("% x: %v", in, err)
		}
		if len(out) != len(in) || differing(in, out) != 1 {
			t.Errorf("% x became % x: want the one value byte changed", in, out)
		}
		if got := exifOrientation(out); got != 3 {
			t.Errorf("% x: orientation read back %d, want 3", out, got)
		}
	}
}

// EXIF without an orientation entry gets one, and nothing else in it moves.
func TestWithOrientationAddsAnEntry(t *testing.T) {
	in := append([]byte("Exif\x00\x00"), exifWithMake()...)
	out, err := withOrientation(in, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, in[:len(exifHeader)+4]) || !bytes.Equal(out[len(exifHeader)+8:len(in)], in[len(exifHeader)+8:]) {
		t.Error("bytes other than the IFD0 offset changed")
	}
	if got := exifOrientation(out); got != 8 {
		t.Fatalf("orientation = %d, want 8", got)
	}
	f := fieldMap(parseEXIF(out))
	if f["Make"] != "Brandname" || f["Model"] != "Z10" {
		t.Errorf("Make %q Model %q after the edit, want them untouched", f["Make"], f["Model"])
	}
	// The entries stay in tag order, as TIFF requires.
	tiff, bo := tiffHeader(out)
	ifd := int(bo.Uint32(tiff[4:]))
	var tags []uint16
	for k := range int(bo.Uint16(tiff[ifd:])) {
		tags = append(tags, bo.Uint16(tiff[ifd+2+12*k:]))
	}
	if len(tags) != 3 || tags[0] != 0x010F || tags[1] != 0x0110 || tags[2] != 0x0112 {
		t.Errorf("IFD0 tags = %#x", tags)
	}
}

func TestWithOrientationRefusesNonsense(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("Exif\x00\x00"), []byte("XX\x00\x2a\x00\x00\x00\x08"), {'M', 'M', 0, 42, 0, 0, 0, 200}} {
		if _, err := withOrientation(in, 6); err == nil {
			t.Errorf("% x: accepted", in)
		}
	}
}

func TestSetOrientationJPEG(t *testing.T) {
	t.Run("patch", func(t *testing.T) {
		in := dirtyJPEG(t)
		out, err := SetOrientation(in, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != len(in) || differing(in, out) != 1 {
			t.Errorf("%d bytes changed, want 1", differing(in, out))
		}
		if got := orientationOf(t, out); got != 3 {
			t.Errorf("orientation = %d, want 3", got)
		}
	})
	t.Run("insert", func(t *testing.T) {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, sampleImage(), nil); err != nil {
			t.Fatal(err)
		}
		in := buf.Bytes()
		out, err := SetOrientation(in, 6)
		if err != nil {
			t.Fatal(err)
		}
		if got := orientationOf(t, out); got != 6 {
			t.Errorf("orientation = %d, want 6", got)
		}
		samePixels(t, in, out)
		// Go's encoder writes no JFIF segment, so the EXIF goes first.
		if out[2] != 0xFF || out[3] != 0xE1 {
			t.Errorf("first segment is %#x, want APP1", out[3])
		}
		if same, err := SetOrientation(in, 1); err != nil || !bytes.Equal(same, in) {
			t.Errorf("setting normal on an image without EXIF changed it (%v)", err)
		}
	})
	t.Run("add an entry", func(t *testing.T) {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, sampleImage(), nil); err != nil {
			t.Fatal(err)
		}
		in := append([]byte{0xFF, 0xD8}, jpegSeg(0xE1, append([]byte("Exif\x00\x00"), exifWithMake()...))...)
		in = append(in, buf.Bytes()[2:]...)
		out, err := SetOrientation(in, 6)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Strip(out, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Orientation != 6 {
			t.Errorf("orientation = %d, want 6", r.Orientation)
		}
		if f := fieldMap(parseEXIF(r.Removed[0].Data)); f["Make"] != "Brandname" {
			t.Errorf("Make = %q after the edit", f["Make"])
		}
		samePixels(t, in, out)
	})
	t.Run("after JFIF", func(t *testing.T) {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, sampleImage(), nil); err != nil {
			t.Fatal(err)
		}
		in := append([]byte{0xFF, 0xD8}, jpegSeg(0xE0, []byte("JFIF\x00\x01\x02\x00\x00\x01\x00\x01\x00\x00"))...)
		in = append(in, buf.Bytes()[2:]...)
		out, err := SetOrientation(in, 8)
		if err != nil {
			t.Fatal(err)
		}
		if out[3] != 0xE0 {
			t.Error("JFIF is no longer the first segment")
		}
		if got := orientationOf(t, out); got != 8 {
			t.Errorf("orientation = %d, want 8", got)
		}
		samePixels(t, in, out)
	})
}

func TestSetOrientationPNG(t *testing.T) {
	t.Run("patch", func(t *testing.T) {
		in := dirtyPNG(t)
		out, err := SetOrientation(in, 5)
		if err != nil {
			t.Fatal(err)
		}
		if got := orientationOf(t, out); got != 5 {
			t.Errorf("orientation = %d, want 5", got)
		}
		samePixels(t, in, out) // png.Decode checks every CRC on the way
	})
	t.Run("insert", func(t *testing.T) {
		var buf bytes.Buffer
		if err := png.Encode(&buf, sampleImage()); err != nil {
			t.Fatal(err)
		}
		in := buf.Bytes()
		out, err := SetOrientation(in, 7)
		if err != nil {
			t.Fatal(err)
		}
		if got := orientationOf(t, out); got != 7 {
			t.Errorf("orientation = %d, want 7", got)
		}
		samePixels(t, in, out)
	})
}

// simpleWebP is a WebP with no VP8X: one image chunk and nothing else.
func simpleWebP(fcc string, data []byte) []byte {
	body := append([]byte("WEBP"), webpChunk(fcc, data)...)
	d := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(d, body...)
}

func TestSetOrientationWebP(t *testing.T) {
	t.Run("patch", func(t *testing.T) {
		in := dirtyWebP()
		out, err := SetOrientation(in, 6)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != len(in) || differing(in, out) != 1 {
			t.Errorf("%d bytes changed, want 1", differing(in, out))
		}
		if got := orientationOf(t, out); got != 6 {
			t.Errorf("orientation = %d, want 6", got)
		}
	})

	// A simple file has nowhere to say it has EXIF: a VP8X chunk is made for
	// it, with the canvas size read from the image chunk.
	cases := []struct {
		name          string
		fcc           string
		data          []byte
		width, height int
		alpha         bool
	}{
		// VP8L: signature, then 14 bits width-1, 14 bits height-1, alpha bit.
		{"lossless", "VP8L", []byte{0x2F, 0x02, 0x40, 0x00, 0x10, 0x00}, 3, 2, true},
		// VP8: key frame tag, start code, 14-bit width and height.
		{"lossy", "VP8 ", []byte{0x10, 0x02, 0x00, 0x9D, 0x01, 0x2A, 0x40, 0x01, 0xF0, 0x00, 0, 0}, 320, 240, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := SetOrientation(simpleWebP(c.fcc, c.data), 8)
			if err != nil {
				t.Fatal(err)
			}
			fccs, flags := webpChunks(t, out)
			if fccsString(fccs) != "VP8X "+c.fcc+" EXIF" {
				t.Fatalf("chunks = %s", fccsString(fccs))
			}
			if flags&webpFlagEXIF == 0 || (flags&webpFlagAlpha != 0) != c.alpha {
				t.Errorf("VP8X flags = %#x", flags)
			}
			x := out[12+8:]
			w := int(x[4]) | int(x[5])<<8 | int(x[6])<<16
			h := int(x[7]) | int(x[8])<<8 | int(x[9])<<16
			if w+1 != c.width || h+1 != c.height {
				t.Errorf("canvas %d×%d, want %d×%d", w+1, h+1, c.width, c.height)
			}
			if got := orientationOf(t, out); got != 8 {
				t.Errorf("orientation = %d, want 8", got)
			}
		})
	}
}

func TestSetOrientationRefuses(t *testing.T) {
	if _, err := SetOrientation(dirtyGIF(t), 6); err == nil {
		t.Error("GIF accepted")
	}
	if _, err := SetOrientation([]byte("not an image"), 6); !errors.Is(err, ErrUnsupported) {
		t.Errorf("text: %v, want ErrUnsupported", err)
	}
	if _, err := SetOrientation(dirtyJPEG(t), 9); err == nil {
		t.Error("orientation 9 accepted")
	}
}

// Whatever it is given, SetOrientation does not panic, and what it returns
// is still an image of the same kind whose orientation reads back.
func FuzzSetOrientation(f *testing.F) {
	f.Add(dirtyJPEG(f), 6)
	f.Add(dirtyPNG(f), 3)
	f.Add(dirtyWebP(), 8)
	f.Add(simpleWebP("VP8L", []byte{0x2F, 0x02, 0x40, 0x00, 0x10, 0x00}), 5)
	f.Add(append([]byte{0xFF, 0xD8}, jpegSeg(0xE1, append([]byte("Exif\x00\x00"), exifWithMake()...))...), 2)
	f.Fuzz(func(t *testing.T, in []byte, v int) {
		v = 1 + (v%8+8)%8
		out, err := SetOrientation(in, v)
		if err != nil {
			return
		}
		if Detect(out) != Detect(in) {
			t.Fatalf("format changed from %s to %s", Detect(in), Detect(out))
		}
		if r, err := Strip(out, Options{}); err == nil && r.Orientation != v && (v != 1 || r.Orientation != 0) {
			t.Fatalf("set %d, read back %d", v, r.Orientation)
		}
	})
}
