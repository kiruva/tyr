package metastrip

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strconv"
	"strings"
	"testing"

	"github.com/kiruva/tyr/internal/termsafe"
)

const secret = "SECRET"

func sampleImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 5), uint8(x ^ y), 255})
		}
	}
	return img
}

// exifWithOrientation returns a TIFF blob with orientation v followed by a
// secret string, standing in for GPS and camera data.
func exifWithOrientation(v int) []byte {
	return append(orientationTIFF(v), secret+"-GPS"...)
}

func jpegSeg(m byte, payload []byte) []byte {
	n := len(payload) + 2
	return append([]byte{0xFF, m, byte(n >> 8), byte(n)}, payload...)
}

func samePixels(t *testing.T, a, b []byte) {
	t.Helper()
	ia, _, err := image.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatalf("decode original: %v", err)
	}
	ib, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode stripped: %v", err)
	}
	if ia.Bounds() != ib.Bounds() {
		t.Fatalf("bounds differ: %v vs %v", ia.Bounds(), ib.Bounds())
	}
	for y := ia.Bounds().Min.Y; y < ia.Bounds().Max.Y; y++ {
		for x := ia.Bounds().Min.X; x < ia.Bounds().Max.X; x++ {
			if ia.At(x, y) != ib.At(x, y) {
				t.Fatalf("pixel (%d,%d) differs", x, y)
			}
		}
	}
}

func mustStrip(t *testing.T, in []byte, o Options) *Result {
	t.Helper()
	r, err := strip(in, o)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	again, err := strip(r.Data, o)
	if err != nil {
		t.Fatalf("strip twice: %v", err)
	}
	if !bytes.Equal(again.Data, r.Data) {
		t.Fatalf("stripping is not idempotent")
	}
	return r
}

func dirtyJPEG(t testing.TB) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, sampleImage(), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	clean := buf.Bytes()
	var d []byte
	d = append(d, 0xFF, 0xD8)
	d = append(d, jpegSeg(0xE0, []byte("JFIF\x00\x01\x02\x00\x00\x01\x00\x01\x00\x00"))...)
	d = append(d, jpegSeg(0xE1, append([]byte("Exif\x00\x00"), exifWithOrientation(6)...))...)
	d = append(d, jpegSeg(0xE1, []byte("http://ns.adobe.com/xap/1.0/\x00"+secret+"-XMP"))...)
	d = append(d, jpegSeg(0xE2, []byte("ICC_PROFILE\x00\x01\x01"+secret+"-ICC"))...)
	d = append(d, jpegSeg(0xED, []byte("Photoshop 3.0\x00"+secret+"-IPTC"))...)
	d = append(d, jpegSeg(0xEE, []byte("Adobe\x00\x64\x00\x00\x00\x00\x01"))...)
	d = append(d, jpegSeg(0xFE, []byte(secret+"-COM"))...)
	d = append(d, clean[2:]...)
	d = append(d, secret+"-TRAILER"...)
	return d
}

func TestJPEG(t *testing.T) {
	in := dirtyJPEG(t)
	r := mustStrip(t, in, Options{})
	if bytes.Contains(r.Data, []byte(secret)) {
		t.Error("secret survived")
	}
	if r.Orientation != 6 {
		t.Errorf("orientation = %d, want 6", r.Orientation)
	}
	if !bytes.Contains(r.Data, []byte("Adobe")) {
		t.Error("Adobe APP14 color transform segment was dropped")
	}
	if len(r.Removed) != 7 {
		t.Errorf("removed %d items, want 7: %v", len(r.Removed), r.Removed)
	}
	samePixels(t, in, r.Data)
}

func TestJPEGKeep(t *testing.T) {
	in := dirtyJPEG(t)
	r := mustStrip(t, in, Options{KeepICC: true, KeepOrientation: true})
	if !bytes.Contains(r.Data, []byte(secret+"-ICC")) {
		t.Error("ICC profile dropped despite KeepICC")
	}
	if bytes.Contains(r.Data, []byte(secret+"-GPS")) {
		t.Error("EXIF payload survived KeepOrientation")
	}
	again, _ := strip(r.Data, Options{})
	if again.Orientation != 6 {
		t.Errorf("rewritten orientation = %d, want 6", again.Orientation)
	}
	samePixels(t, in, r.Data)
}

func dirtyPNG(t testing.TB) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, sampleImage()); err != nil {
		t.Fatal(err)
	}
	clean := buf.Bytes()
	ihdrEnd := len(pngSig) + 25
	var d []byte
	d = append(d, clean[:ihdrEnd]...)
	d = append(d, pngChunk("tEXt", []byte("Author\x00"+secret))...)
	d = append(d, pngChunk("iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00"+secret))...)
	d = append(d, pngChunk("eXIf", exifWithOrientation(8))...)
	d = append(d, pngChunk("iCCP", []byte("prof\x00\x00"+secret+"-ICC"))...)
	d = append(d, pngChunk("tIME", []byte{0x07, 0xE9, 1, 2, 3, 4, 5})...)
	d = append(d, pngChunk("pHYs", []byte{0, 0, 0x0B, 0x13, 0, 0, 0x0B, 0x13, 1})...)
	d = append(d, clean[ihdrEnd:]...)
	d = append(d, secret+"-TRAILER"...)
	return d
}

func TestPNG(t *testing.T) {
	in := dirtyPNG(t)
	r := mustStrip(t, in, Options{})
	if bytes.Contains(r.Data, []byte(secret)) {
		t.Error("secret survived")
	}
	if r.Orientation != 8 {
		t.Errorf("orientation = %d, want 8", r.Orientation)
	}
	if len(r.Removed) != 7 {
		t.Errorf("removed %d items, want 7: %v", len(r.Removed), r.Removed)
	}
	samePixels(t, in, r.Data)
}

func TestPNGKeep(t *testing.T) {
	in := dirtyPNG(t)
	r := mustStrip(t, in, Options{KeepICC: true, KeepOrientation: true})
	if !bytes.Contains(r.Data, []byte(secret+"-ICC")) {
		t.Error("ICC profile dropped despite KeepICC")
	}
	again, _ := strip(r.Data, Options{})
	if again.Orientation != 8 {
		t.Errorf("rewritten orientation = %d, want 8", again.Orientation)
	}
	samePixels(t, in, r.Data) // Go's decoder checks chunk CRCs
}

func dirtyGIF(t testing.TB) []byte {
	var frames []*image.Paletted
	for i := range 2 {
		p := image.NewPaletted(image.Rect(0, 0, 16, 16), palette.Plan9)
		for k := range p.Pix {
			p.Pix[k] = uint8(k + i*7)
		}
		frames = append(frames, p)
	}
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{Image: frames, Delay: []int{10, 10}, LoopCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	clean := buf.Bytes()
	// Go writes no global color table when frames carry their own palettes.
	pos := 13
	if clean[10]&0x80 != 0 {
		pos += 3 << (clean[10]&7 + 1)
	}
	var d []byte
	d = append(d, clean[:pos]...)
	d = append(d, 0x21, 0xFE, byte(len(secret)))
	d = append(d, secret+"\x00"...)
	d = append(d, 0x21, 0xFF, 11)
	d = append(d, "XMP DataXMP"...)
	d = append(d, byte(len(secret)+4))
	d = append(d, secret+"-XMP\x00"...)
	d = append(d, clean[pos:]...)
	d = append(d, secret+"-TRAILER"...)
	return d
}

func TestGIF(t *testing.T) {
	in := dirtyGIF(t)
	r := mustStrip(t, in, Options{})
	if bytes.Contains(r.Data, []byte(secret)) {
		t.Error("secret survived")
	}
	if len(r.Removed) != 3 {
		t.Errorf("removed %d items, want 3: %v", len(r.Removed), r.Removed)
	}
	a, err := gif.DecodeAll(bytes.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	b, err := gif.DecodeAll(bytes.NewReader(r.Data))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Image) != 2 || b.LoopCount != a.LoopCount {
		t.Errorf("frames=%d loop=%d, want 2 frames loop %d", len(b.Image), b.LoopCount, a.LoopCount)
	}
	for i := range a.Image {
		if !bytes.Equal(a.Image[i].Pix, b.Image[i].Pix) {
			t.Errorf("frame %d pixels differ", i)
		}
	}
}

func webpChunk(fcc string, data []byte) []byte {
	c := append([]byte(fcc), binary.LittleEndian.AppendUint32(nil, uint32(len(data)))...)
	c = append(c, data...)
	if len(data)&1 == 1 {
		c = append(c, 0)
	}
	return c
}

func dirtyWebP() []byte {
	vp8x := []byte{webpFlagICC | webpFlagEXIF | webpFlagXMP, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	var body []byte
	body = append(body, "WEBP"...)
	body = append(body, webpChunk("VP8X", vp8x)...)
	body = append(body, webpChunk("ICCP", []byte(secret+"-ICC"))...)
	body = append(body, webpChunk("VP8L", []byte{0x2F, 0, 0, 0, 0})...) // odd length: padded
	body = append(body, webpChunk("EXIF", append([]byte("Exif\x00\x00"), exifWithOrientation(3)...))...)
	body = append(body, webpChunk("XMP ", []byte(secret+"-XMP"))...)
	d := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	d = append(d, body...)
	return append(d, secret+"-TRAILER"...)
}

func webpChunks(t *testing.T, b []byte) (fccs []string, flags byte) {
	t.Helper()
	if int(binary.LittleEndian.Uint32(b[4:]))+8 != len(b) {
		t.Fatalf("RIFF size %d does not match file length %d", binary.LittleEndian.Uint32(b[4:]), len(b))
	}
	for i := 12; i < len(b); {
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		fcc := string(b[i : i+4])
		if fcc == "VP8X" {
			flags = b[i+8]
		}
		fccs = append(fccs, fcc)
		i += 8 + n + n&1
	}
	return fccs, flags
}

func TestWebP(t *testing.T) {
	r := mustStrip(t, dirtyWebP(), Options{})
	if bytes.Contains(r.Data, []byte(secret)) {
		t.Error("secret survived")
	}
	if r.Orientation != 3 {
		t.Errorf("orientation = %d, want 3", r.Orientation)
	}
	fccs, flags := webpChunks(t, r.Data)
	if got := fccsString(fccs); got != "VP8X VP8L" {
		t.Errorf("chunks = %s, want VP8X VP8L", got)
	}
	if flags != 0 {
		t.Errorf("VP8X flags = %#x, want 0", flags)
	}
}

func TestWebPKeep(t *testing.T) {
	r := mustStrip(t, dirtyWebP(), Options{KeepICC: true, KeepOrientation: true})
	fccs, flags := webpChunks(t, r.Data)
	if got := fccsString(fccs); got != "VP8X ICCP VP8L EXIF" {
		t.Errorf("chunks = %s, want VP8X ICCP VP8L EXIF", got)
	}
	if flags != webpFlagICC|webpFlagEXIF {
		t.Errorf("VP8X flags = %#x, want %#x", flags, webpFlagICC|webpFlagEXIF)
	}
	if bytes.Contains(r.Data, []byte(secret+"-GPS")) {
		t.Error("EXIF payload survived KeepOrientation")
	}
}

func fccsString(fccs []string) string {
	var b bytes.Buffer
	for i, f := range fccs {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(f)
	}
	return b.String()
}

func TestUnsupported(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("hello"), []byte("II*\x00rest"), []byte("\x00\x00\x00\x18ftypheic")} {
		if _, err := strip(in, Options{}); err == nil {
			t.Errorf("strip(%q) succeeded, want error", in)
		}
	}
}

func FuzzStrip(f *testing.F) {
	f.Add(dirtyJPEG(f))
	f.Add(dirtyPNG(f))
	f.Add(dirtyGIF(f))
	f.Add(dirtyWebP())
	f.Add(aiManifest())
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
		f.Fatal(err)
	}
	c := buf.Bytes()
	f.Add(append(append(append([]byte{}, c[:33]...), pngChunk("caBX", aiManifest())...), c[33:]...))
	f.Fuzz(func(t *testing.T, in []byte) {
		for _, o := range []Options{{}, {KeepICC: true, KeepOrientation: true}} {
			r, err := strip(in, o)
			if err != nil {
				continue
			}
			for _, it := range r.Removed {
				for _, f := range describe(it, newBudget()) {
					termsafe.Line(f.Value)
				}
			}
			if detect(r.Data) != detect(in) {
				t.Fatalf("format changed from %s to %s", detect(in), detect(r.Data))
			}
		}
	})
}

func fieldMap(fs []field) map[string]string {
	m := map[string]string{}
	for _, f := range fs {
		m[f.Key] = f.Value
	}
	return m
}

func TestDescribeEXIF(t *testing.T) {
	// Big-endian TIFF: IFD0 {Make, GPS pointer}, GPS IFD {LatRef, Lat, LonRef, Lon}.
	b := []byte{'M', 'M', 0, 42, 0, 0, 0, 8}
	be := func(v ...uint32) {
		for _, x := range v {
			b = binary.BigEndian.AppendUint32(b, x)
		}
	}
	entry := func(tag, typ uint16, cnt, val uint32) {
		b = binary.BigEndian.AppendUint16(b, tag)
		b = binary.BigEndian.AppendUint16(b, typ)
		be(cnt, val)
	}
	b = append(b, 0, 2)
	entry(0x010F, 2, 4, 0x41434D00) // "ACM\0" inline
	entry(0x8825, 4, 1, 38)         // GPS IFD at 38
	be(0)                           // no IFD1
	// GPS IFD at 38: 4 entries, rationals start at 38+2+48+4 = 92
	b = append(b, 0, 4)
	entry(1, 2, 2, 0x53000000) // "S"
	entry(2, 5, 3, 92)
	entry(3, 2, 2, 0x57000000) // "W"
	entry(4, 5, 3, 116)
	be(0)
	be(33, 1, 51, 1, 3600, 100) // 33 51 36
	be(151, 1, 12, 1, 3600, 100)
	m := fieldMap(parseEXIF(b))
	if m["Make"] != "ACM" {
		t.Errorf("Make = %q", m["Make"])
	}
	if m["GPSPosition"] != "-33.860000, -151.210000" {
		t.Errorf("GPSPosition = %q", m["GPSPosition"])
	}
}

func TestDescribeDirtyFiles(t *testing.T) {
	r := mustStrip(t, dirtyPNG(t), Options{})
	m := map[string]string{}
	for _, it := range r.Removed {
		for k, v := range fieldMap(describe(it, newBudget())) {
			m[k] = v
		}
	}
	if m["Author"] != secret || m["Orientation"] != "8 (rotate 270 CW)" {
		t.Errorf("unexpected fields: %v", m)
	}
}

// Tiny CBOR encoder for building test manifests: string, []any, []cborKV.
func cborEnc(v any) []byte {
	head := func(major byte, n int) []byte {
		switch {
		case n < 24:
			return []byte{major<<5 | byte(n)}
		case n < 256:
			return []byte{major<<5 | 24, byte(n)}
		}
		return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
	}
	switch t := v.(type) {
	case string:
		return append(head(3, len(t)), t...)
	case []any:
		b := head(4, len(t))
		for _, e := range t {
			b = append(b, cborEnc(e)...)
		}
		return b
	case []cborKV:
		b := head(5, len(t))
		for _, kv := range t {
			b = append(b, cborEnc(kv.K)...)
			b = append(b, cborEnc(kv.V)...)
		}
		return b
	}
	panic("unsupported")
}

func jBox(typ string, data ...[]byte) []byte {
	body := bytes.Join(data, nil)
	return append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), append([]byte(typ), body...)...)
}

func jSuper(label string, kids ...[]byte) []byte {
	jumd := append(make([]byte, 16), 0x03)
	jumd = append(jumd, label+"\x00"...)
	return jBox("jumb", append([][]byte{jBox("jumd", jumd)}, kids...)...)
}

func aiManifest() []byte {
	actions := []cborKV{{"actions", []any{[]cborKV{
		{"action", "c2pa.created"},
		{"softwareAgent", []cborKV{{"name", "Example Image Model"}, {"version", "2"}}},
		{"digitalSourceType", "http://cv.iptc.org/newscodes/digitalsourcetype/trainedAlgorithmicMedia"},
	}}}}
	return jSuper("c2pa", jSuper("urn:c2pa:test",
		jSuper("c2pa.assertions",
			jSuper("c2pa.actions.v2", jBox("cbor", cborEnc(actions))),
			jSuper("c2pa.soft-binding", jBox("cbor", cborEnc([]cborKV{{"alg", "com.example.watermark"}})))),
		jSuper("c2pa.claim.v2", jBox("cbor", cborEnc([]cborKV{
			{"claim_generator_info", []any{[]cborKV{{"name", "TestGen"}, {"version", "1.0"}}}},
			{"dc:title", "castle.png"}}))),
	))
}

func TestC2PAInPNG(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, sampleImage()); err != nil {
		t.Fatal(err)
	}
	clean := buf.Bytes()
	in := append(append(append([]byte{}, clean[:33]...), pngChunk("caBX", aiManifest())...), clean[33:]...)
	r := mustStrip(t, in, Options{})
	if len(r.Removed) != 1 || r.Removed[0].Kind != "jumbf" {
		t.Fatalf("removed = %v", r.Removed)
	}
	fields := describe(r.Removed[0], newBudget())
	m := fieldMap(fields)
	if m["ClaimGenerator"] != "TestGen 1.0" || m["Action"] != "c2pa.created" || m["SoftwareAgent"] != "Example Image Model 2" {
		t.Errorf("fields = %v", fields)
	}
	markers := strings.Join(findMarkers([]itemFields{{r.Removed[0], fields}}), "\n")
	for _, want := range []string{"C2PA Content Credentials present", "digital source type: AI-generated", "com.example.watermark"} {
		if !strings.Contains(markers, want) {
			t.Errorf("markers missing %q:\n%s", want, markers)
		}
	}
	samePixels(t, in, r.Data)
}

func TestC2PAInJPEGSegments(t *testing.T) {
	// Split one manifest across three APP11 segments, each repeating the box header.
	man := aiManifest()
	hdr, body := man[:8], man[8:]
	var d []byte
	d = append(d, 0xFF, 0xD8)
	for seq, k := 1, 0; k < len(body); seq++ {
		end := min(k+len(body)/3+1, len(body))
		p := append([]byte("JP\x00\x01"), binary.BigEndian.AppendUint32(nil, uint32(seq))...)
		p = append(append(p, hdr...), body[k:end]...)
		d = append(d, jpegSeg(0xEB, p)...)
		k = end
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, sampleImage(), nil); err != nil {
		t.Fatal(err)
	}
	d = append(d, buf.Bytes()[2:]...)
	r := mustStrip(t, d, Options{})
	if len(r.Removed) != 3 || r.Removed[1].Kind != "cont" {
		t.Fatalf("removed = %v", r.Removed)
	}
	if m := fieldMap(describe(r.Removed[0], newBudget())); m["Title"] != "castle.png" {
		t.Errorf("reassembled manifest not decoded: %v", m)
	}
}

func TestCBOR(t *testing.T) {
	for _, c := range []struct {
		hex  string
		want string
	}{
		{"f93c00", "1"},     // half float
		{"3903e7", "-1000"}, // negative int
		{"7f657374726561646d696e67ff", "streaming"}, // indefinite text
		{"9f01820203ff", "[1, [2, 3]]"},             // indefinite array
		{"a2616101616282020 3", `{"a": 1, "b": [2, 3]}`},
		{"c11a514b67b0", "1363896240"}, // tag 1
	} {
		b, err := hexDecode(strings.ReplaceAll(c.hex, " ", ""))
		if err != nil {
			t.Fatal(err)
		}
		v, err := cborDecode(b)
		if err != nil {
			t.Errorf("%s: %v", c.hex, err)
			continue
		}
		if got := render(v); got != c.want {
			t.Errorf("%s: got %s, want %s", c.hex, got, c.want)
		}
	}
	if _, err := cborDecode([]byte{0x9b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}); err == nil {
		t.Error("huge array length accepted")
	}
}

func hexDecode(s string) ([]byte, error) {
	b := make([]byte, len(s)/2)
	for i := range b {
		v, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		b[i] = byte(v)
	}
	return b, nil
}
