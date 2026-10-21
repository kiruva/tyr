package metastrip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
)

// webpFlagAlpha is VP8X's "the image has transparency" flag.
const webpFlagAlpha = 0x10

// SetOrientation returns b with its EXIF orientation set to v (1–8), and
// every other byte of the image as it was: an orientation entry already there
// is overwritten where it stands, one that is missing is added to the EXIF
// the image has, and an image with no EXIF at all gets a minimal block saying
// only this. Pixels are never decoded. GIF has no EXIF to carry it and is
// refused; so is anything that is not an image this package reads.
//
// Setting "normal" on an image that has no EXIF is no change at all, and b
// comes back as it is.
func SetOrientation(b []byte, v int) ([]byte, error) {
	if v < 1 || v > 8 {
		return nil, fmt.Errorf("orientation %d is not one of 1–8", v)
	}
	switch Detect(b) {
	case "jpeg":
		return setJPEGOrientation(b, v)
	case "png":
		return setPNGOrientation(b, v)
	case "webp":
		return setWebPOrientation(b, v)
	case "gif":
		return nil, errors.New("a GIF has no EXIF to hold an orientation")
	}
	if kind := Unsupported(b); kind != "" {
		return nil, fmt.Errorf("%w (%s)", ErrUnsupported, kind)
	}
	return nil, ErrUnsupported
}

// orientationEntry finds tag 0x0112 in IFD0. It returns the TIFF data with
// its byte order, where IFD0 is, and the offset of the entry within tiff, or
// -1 when IFD0 has none. ok is false for EXIF too broken to edit.
func orientationEntry(b []byte) (tiff []byte, bo binary.ByteOrder, ifd, entry int, ok bool) {
	tiff, bo = tiffHeader(b)
	if bo == nil || bo.Uint16(tiff[2:]) != 42 {
		return nil, nil, 0, 0, false
	}
	ifd = int(bo.Uint32(tiff[4:]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return nil, nil, 0, 0, false
	}
	n := int(bo.Uint16(tiff[ifd:]))
	if ifd+2+12*n+4 > len(tiff) {
		return nil, nil, 0, 0, false
	}
	for k := range n {
		if e := ifd + 2 + 12*k; bo.Uint16(tiff[e:]) == 0x0112 {
			return tiff, bo, ifd, e, true
		}
	}
	return tiff, bo, ifd, -1, true
}

// withOrientation returns an EXIF block — TIFF data, with or without the
// "Exif\0\0" in front — whose orientation is v.
//
// An entry already there is overwritten in place, which changes nothing else.
// A missing one cannot be squeezed into IFD0 without moving everything after
// it, and every offset in the block with it. So a copy of IFD0 with the entry
// added goes on the end, and the header points at the copy instead: the old
// IFD0 is left where it was, unused, and nothing that pointed anywhere points
// anywhere else.
func withOrientation(b []byte, v int) ([]byte, error) {
	tiff, bo, ifd, entry, ok := orientationEntry(b)
	if !ok {
		return nil, errors.New("the image's EXIF cannot be read")
	}
	out := slices.Clone(b)
	start := len(b) - len(tiff) // the "Exif\0\0", if there was one
	t := out[start:]

	if entry >= 0 {
		switch bo.Uint16(tiff[entry+2:]) {
		case 3: // SHORT
			bo.PutUint16(t[entry+8:], uint16(v))
		case 4: // LONG
			bo.PutUint32(t[entry+8:], uint32(v))
		default:
			return nil, errors.New("the image's orientation entry is not a number")
		}
		return out, nil
	}

	n := int(bo.Uint16(tiff[ifd:]))
	entries := tiff[ifd+2 : ifd+2+12*n]
	next := tiff[ifd+2+12*n : ifd+2+12*n+4]

	add := make([]byte, 12)
	bo.PutUint16(add, 0x0112)
	bo.PutUint16(add[2:], 3) // SHORT
	bo.PutUint32(add[4:], 1) // one value
	bo.PutUint16(add[8:], uint16(v))

	// TIFF keeps an IFD's entries in tag order.
	at := n
	for k := range n {
		if bo.Uint16(entries[12*k:]) > 0x0112 {
			at = k
			break
		}
	}
	if len(t)&1 == 1 {
		out = append(out, 0) // an IFD starts on a word boundary
	}
	newIFD := len(out) - start
	if uint64(newIFD) > 0xFFFFFFFF {
		return nil, errors.New("the image's EXIF is too large to add to")
	}
	count := make([]byte, 2)
	bo.PutUint16(count, uint16(n+1))
	out = append(out, count...)
	out = append(out, entries[:12*at]...)
	out = append(out, add...)
	out = append(out, entries[12*at:]...)
	out = append(out, next...)
	bo.PutUint32(out[start+4:], uint32(newIFD))
	return out, nil
}

// setJPEGOrientation edits the first Exif APP1 segment, or adds one.
func setJPEGOrientation(b []byte, v int) ([]byte, error) {
	insertAt := 2 // right after SOI, or after a JFIF APP0 that must stay first
	for i, first := 2, true; i+4 <= len(b); first = false {
		if b[i] != 0xFF {
			return nil, fmt.Errorf("jpeg: expected marker at offset %d", i)
		}
		m := b[i+1]
		if m == 0xFF { // fill byte
			i++
			continue
		}
		if m == 0xDA || m == 0xD9 { // start of scan or end of image: no EXIF before it
			break
		}
		if (m >= 0xD0 && m <= 0xD7) || m == 0x01 {
			i += 2
			continue
		}
		n := int(b[i+2])<<8 | int(b[i+3])
		if n < 2 || i+2+n > len(b) {
			return nil, fmt.Errorf("jpeg: bad segment length at offset %d", i)
		}
		payload := b[i+4 : i+2+n]
		if m == 0xE1 && bytes.HasPrefix(payload, exifHeader) {
			exif, err := withOrientation(payload, v)
			if err != nil {
				return nil, err
			}
			if len(exif)+2 > 0xFFFF {
				return nil, errors.New("jpeg: the EXIF segment would grow past its 64 KB limit")
			}
			out := slices.Clone(b[:i])
			out = append(out, 0xFF, 0xE1, byte((len(exif)+2)>>8), byte(len(exif)+2))
			out = append(out, exif...)
			return append(out, b[i+2+n:]...), nil
		}
		if first && m == 0xE0 && bytes.HasPrefix(payload, []byte("JFIF\x00")) {
			insertAt = i + 2 + n
		}
		i += 2 + n
	}

	if v == 1 {
		return b, nil
	}
	tiff := orientationTIFF(v)
	n := 2 + len(exifHeader) + len(tiff)
	app1 := []byte{0xFF, 0xE1, byte(n >> 8), byte(n)}
	app1 = append(app1, exifHeader...)
	app1 = append(app1, tiff...)
	return slices.Insert(slices.Clone(b), insertAt, app1...), nil
}

// setPNGOrientation edits the eXIf chunk, or adds one after IHDR.
func setPNGOrientation(b []byte, v int) ([]byte, error) {
	ihdrEnd := -1
	for i := len(pngSig); i+12 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[i:]))
		typ := string(b[i+4 : i+8])
		end := i + 12 + n
		if end > len(b) || end < i {
			return nil, fmt.Errorf("png: bad chunk length at offset %d", i)
		}
		switch typ {
		case "IHDR":
			ihdrEnd = end
		case "eXIf":
			exif, err := withOrientation(b[i+8:i+8+n], v)
			if err != nil {
				return nil, err
			}
			out := slices.Clone(b[:i])
			out = append(out, pngChunk("eXIf", exif)...)
			return append(out, b[end:]...), nil
		case "IDAT", "IEND":
			i = len(b) // eXIf must come before the image data
			continue
		}
		i = end
	}
	if ihdrEnd < 0 {
		return nil, errors.New("png: no IHDR chunk")
	}
	if v == 1 {
		return b, nil
	}
	return slices.Insert(slices.Clone(b), ihdrEnd, pngChunk("eXIf", orientationTIFF(v))...), nil
}

// setWebPOrientation edits the EXIF chunk, or adds one at the end. Only an
// extended file can say it carries EXIF, so a simple one is given the VP8X
// chunk that says so, sized from its image.
func setWebPOrientation(b []byte, v int) ([]byte, error) {
	size := uint64(binary.LittleEndian.Uint32(b[4:]))
	if 8+size > uint64(len(b)) {
		return nil, errors.New("webp: truncated file")
	}
	end := 8 + int(size)
	var chunks []webpSpan
	for i := 12; i < end; {
		if i+8 > end {
			return nil, fmt.Errorf("webp: truncated chunk at offset %d", i)
		}
		n64 := uint64(binary.LittleEndian.Uint32(b[i+4:]))
		if uint64(i)+8+n64 > uint64(end) {
			return nil, fmt.Errorf("webp: bad chunk length at offset %d", i)
		}
		chunkEnd := min(i+8+int(n64)+int(n64&1), end)
		chunks = append(chunks, webpSpan{string(b[i : i+4]), i, chunkEnd})
		i = chunkEnd
	}

	var body [][]byte // the chunks as they will be written
	vp8x, exif := -1, -1
	for _, c := range chunks {
		switch c.fcc {
		case "VP8X":
			vp8x = len(body)
		case "EXIF":
			exif = len(body)
		}
		body = append(body, b[c.start:c.end])
	}

	if exif < 0 && v == 1 {
		return b, nil
	}
	if vp8x < 0 {
		x, err := vp8xFor(chunks, b)
		switch {
		case err == nil:
			body = append([][]byte{x}, body...)
			vp8x = 0
			if exif >= 0 {
				exif++
			}
		case exif < 0:
			return nil, err
		}
		// An EXIF chunk in a file without VP8X breaks the format already;
		// it is edited all the same, and left as unflagged as it was.
	}
	if exif >= 0 {
		data := body[exif][8 : 8+binary.LittleEndian.Uint32(body[exif][4:])]
		edited, err := withOrientation(data, v)
		if err != nil {
			return nil, err
		}
		body[exif] = webpChunkBytes("EXIF", edited)
	} else {
		body = append(body, webpChunkBytes("EXIF", orientationTIFF(v)))
	}
	if vp8x >= 0 {
		x := slices.Clone(body[vp8x])
		x[8] |= webpFlagEXIF
		body[vp8x] = x
	}

	out := append(make([]byte, 0, len(b)+64), "RIFF\x00\x00\x00\x00WEBP"...)
	for _, c := range body {
		out = append(out, c...)
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return append(out, b[end:]...), nil
}

// webpSpan is where one chunk of a WebP file is: header and padding included.
type webpSpan struct {
	fcc        string
	start, end int
}

// webpChunkBytes is one RIFF chunk, padded to an even length.
func webpChunkBytes(fcc string, data []byte) []byte {
	c := append([]byte(fcc), binary.LittleEndian.AppendUint32(nil, uint32(len(data)))...)
	c = append(c, data...)
	if len(data)&1 == 1 {
		c = append(c, 0)
	}
	return c
}

// vp8xFor makes the VP8X chunk a simple WebP lacks, with the canvas size its
// one image chunk declares.
func vp8xFor(chunks []webpSpan, b []byte) ([]byte, error) {
	for _, c := range chunks {
		d := b[c.start+8 : c.end]
		var w, h int
		var flags byte
		switch {
		case c.fcc == "VP8L" && len(d) >= 5 && d[0] == 0x2F:
			bits := binary.LittleEndian.Uint32(d[1:])
			w, h = int(bits&0x3FFF)+1, int(bits>>14&0x3FFF)+1
			if bits>>28&1 == 1 {
				flags |= webpFlagAlpha
			}
		case c.fcc == "VP8 " && len(d) >= 10 && d[0]&1 == 0 && d[3] == 0x9D && d[4] == 0x01 && d[5] == 0x2A:
			w = int(binary.LittleEndian.Uint16(d[6:]) & 0x3FFF)
			h = int(binary.LittleEndian.Uint16(d[8:]) & 0x3FFF)
		default:
			continue
		}
		if w < 1 || h < 1 {
			break
		}
		x := []byte{flags, 0, 0, 0, byte(w - 1), byte((w - 1) >> 8), byte((w - 1) >> 16), byte(h - 1), byte((h - 1) >> 8), byte((h - 1) >> 16)}
		return webpChunkBytes("VP8X", x), nil
	}
	return nil, errors.New("webp: cannot read the image size to add EXIF")
}
