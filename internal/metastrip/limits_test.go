package metastrip

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"runtime"
	"strings"
	"testing"
)

// minimalPNG is a 1×1 PNG with the given chunks between its header and data.
func minimalPNG(chunks ...[]byte) []byte {
	b := append([]byte{}, pngSig...)
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr, 1)
	binary.BigEndian.PutUint32(ihdr[4:], 1)
	ihdr[8] = 8
	b = append(b, pngChunk("IHDR", ihdr)...)
	for _, c := range chunks {
		b = append(b, c...)
	}
	b = append(b, pngChunk("IDAT", []byte{0x78, 1, 1, 0, 0, 0xff, 0xff, 0, 0, 0, 1})...)
	return append(b, pngChunk("IEND", nil)...)
}

// allocated is how many bytes f allocates, all told.
func allocated(f func()) uint64 {
	runtime.GC()
	var a, z runtime.MemStats
	runtime.ReadMemStats(&a)
	f()
	runtime.ReadMemStats(&z)
	return z.TotalAlloc - a.TotalAlloc
}

// stripInspect is what the app does with an image: strip it, then decode
// what came out.
func stripInspect(t *testing.T, in []byte) Report {
	t.Helper()
	res, err := Strip(in, Options{})
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	return Inspect(res)
}

func valueBytes(rep Report) int {
	n := 0
	for _, s := range rep.Sections {
		for _, f := range s.Fields {
			n += len(f.Key) + len(f.Value)
		}
	}
	return n
}

// Each of these small images used to cost gigabytes to inspect — a few
// hundred kilobytes in, and the viewer's `i` froze the app or killed it.
// What they cost now is bounded, and so is what the report holds.
func TestInspectIsBounded(t *testing.T) {
	const maxAlloc = 128 << 20

	// EXIF: thousands of entries all pointing at one 200 KB string.
	exif := func() []byte {
		const n, blob = 5000, 200_000
		tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8}
		tiff = binary.BigEndian.AppendUint16(tiff, n)
		off := 8 + 2 + 12*n + 4
		for range n {
			tiff = binary.BigEndian.AppendUint16(tiff, 0x010F)
			tiff = binary.BigEndian.AppendUint16(tiff, 2)
			tiff = binary.BigEndian.AppendUint32(tiff, blob)
			tiff = binary.BigEndian.AppendUint32(tiff, uint32(off))
		}
		tiff = append(tiff, 0, 0, 0, 0)
		tiff = append(tiff, bytes.Repeat([]byte("A"), blob)...)
		return minimalPNG(pngChunk("eXIf", tiff))
	}

	// zTXt: twenty chunks, each 16 MB of zeros compressed.
	ztxt := func() []byte {
		var zb bytes.Buffer
		w, _ := zlib.NewWriterLevel(&zb, 9)
		_, _ = w.Write(make([]byte, 16<<20))
		_ = w.Close()
		var chunks [][]byte
		for range 20 {
			chunks = append(chunks, pngChunk("zTXt", append([]byte("k\x00\x00"), zb.Bytes()...)))
		}
		return minimalPNG(chunks...)
	}

	// XMP nested twelve thousand deep.
	xmp := func() []byte {
		x := strings.Repeat("<a>t", 12000)
		return minimalPNG(pngChunk("iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00"+x)))
	}

	// A C2PA assertion holding a three-million-element CBOR array.
	cbor := func() []byte {
		arr := []byte{0x9f}
		arr = append(arr, bytes.Repeat([]byte{0x19, 0x01, 0x00}, 3_000_000)...)
		arr = append(arr, 0xff)
		m := jSuper("c2pa", jSuper("urn:x", jSuper("c2pa.assertions", jSuper("my.assert", jBox("cbor", arr)))))
		return minimalPNG(pngChunk("caBX", m))
	}

	for _, tc := range []struct {
		name string
		in   func() []byte
	}{{"exif", exif}, {"ztxt", ztxt}, {"xmp", xmp}, {"cbor", cbor}} {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in()
			var rep Report
			got := allocated(func() { rep = stripInspect(t, in) })
			if got > maxAlloc {
				t.Errorf("allocated %d MB for a %d KB image, want under %d MB", got>>20, len(in)>>10, maxAlloc>>20)
			}
			if n := valueBytes(rep); n > maxReportBytes+3*maxValueBytes {
				t.Errorf("report holds %d KB, want it capped near %d KB", n>>10, maxReportBytes>>10)
			}
		})
	}
}

// A JPEG whose ICC profile is spread over a thousand segments reassembles in
// one pass: each segment used to copy everything before it.
func TestStripJoinsSegmentsOnce(t *testing.T) {
	b := []byte{0xFF, 0xD8}
	seg := append([]byte("ICC_PROFILE\x00\x01\x01"), make([]byte, 65000)...)
	for range 1000 {
		b = append(b, jpegSeg(0xE2, seg)...)
	}
	b = append(b, 0xFF, 0xD9)

	got := allocated(func() {
		if _, err := Strip(b, Options{}); err != nil {
			t.Fatal(err)
		}
	})
	if limit := uint64(4 * len(b)); got > limit {
		t.Errorf("allocated %d MB for a %d MB image, want under %d MB", got>>20, len(b)>>20, limit>>20)
	}
}

// A value is shown up to maxValueBytes and says how long it really was.
func TestLongValuesAreClipped(t *testing.T) {
	long := strings.Repeat("x", 3*maxValueBytes)
	rep := stripInspect(t, minimalPNG(pngChunk("tEXt", []byte("Comment\x00"+long))))
	v := rep.Sections[0].Fields[0].Value
	if len(v) > maxValueBytes+64 || !strings.Contains(v, "12288 bytes") {
		t.Errorf("value is %d bytes ending %q, want it clipped and saying how long it was", len(v), v[max(len(v)-40, 0):])
	}
}

// "imagen" is Spanish for "image", and "grok" an English word: in a caption
// they are not a generator's fingerprint. In the field naming the software
// that made the file, they are.
func TestGeneratorNamesNeedTheRightField(t *testing.T) {
	caption := minimalPNG(pngChunk("tEXt", []byte("Description\x00Una imagen de la playa, grok it")))
	if m := stripInspect(t, caption).Markers; len(m) != 0 {
		t.Errorf("markers = %v for a caption, want none", m)
	}
	software := minimalPNG(pngChunk("tEXt", []byte("Software\x00Imagen 3")))
	if m := stripInspect(t, software).Markers; len(m) != 1 {
		t.Errorf("markers = %v, want the generator named in Software", m)
	}
}

// The location comes from the EXIF GPS block only. A PNG text chunk named
// GPSPosition is just text that anyone can write.
func TestLocationOnlyFromEXIF(t *testing.T) {
	in := minimalPNG(pngChunk("tEXt", []byte("GPSPosition\x0051.5, -0.1")))
	if loc := stripInspect(t, in).Location; loc != "" {
		t.Errorf("Location = %q from a text chunk, want none", loc)
	}
}

// An iTXt keyword is Latin-1, like every PNG keyword.
func TestITXtKeywordIsLatin1(t *testing.T) {
	got := parsePNGText("iTXt", []byte("caf\xe9\x00\x00\x00\x00\x00text"), &budget{inflate: maxInflate})
	if len(got) != 1 || got[0].Key != "café" {
		t.Errorf("fields = %+v, want key café", got)
	}
}

// span refuses an offset and size that add past the data, including the ones
// that wrap around when added in a 32-bit int.
func TestSpan(t *testing.T) {
	b := make([]byte, 16)
	for _, tc := range []struct {
		off, size uint64
		ok        bool
	}{
		{0, 16, true}, {8, 8, true}, {8, 9, false},
		{0xFFFFFFFF, 2, false}, {2, 0xFFFFFFFF, false}, {0xFFFFFFF0, 0x20, false},
	} {
		if _, ok := span(b, tc.off, tc.size); ok != tc.ok {
			t.Errorf("span(%#x, %#x) ok = %v, want %v", tc.off, tc.size, ok, tc.ok)
		}
	}
}

func FuzzStripInspect(f *testing.F) {
	f.Add(dirtyJPEG(f))
	f.Add(dirtyPNG(f))
	f.Add(dirtyGIF(f))
	f.Add(dirtyWebP())
	f.Fuzz(func(t *testing.T, b []byte) {
		res, err := Strip(b, Options{KeepOrientation: true})
		if err != nil {
			return
		}
		_ = Inspect(res)
		// Stripping the output again succeeds and removes nothing new.
		res2, err := Strip(res.Data, Options{KeepOrientation: true})
		if err != nil {
			t.Fatalf("restrip failed: %v", err)
		}
		if len(res2.Removed) != 0 && !bytes.Equal(res2.Data, res.Data) {
			t.Fatalf("restrip changed output; removed %v", res2.Removed[0].Name)
		}
	})
}

func FuzzCBOR(f *testing.F) {
	f.Add(aiManifest())
	f.Fuzz(func(t *testing.T, b []byte) {
		v, _ := cborDecode(b)
		_ = render(v)
		_ = describeJUMBF(b)
	})
}

func FuzzParsers(f *testing.F) {
	f.Add(exifWithOrientation(6))
	f.Fuzz(func(t *testing.T, b []byte) {
		_ = parseEXIF(b)
		_ = exifOrientation(b)
		_ = parsePhotoshop(b)
		_ = parseICC(b)
		_ = parseXMP(b)
		for _, k := range []string{"tEXt", "zTXt", "iTXt"} {
			_ = parsePNGText(k, b, &budget{inflate: maxInflate})
		}
	})
}
