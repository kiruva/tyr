package metastrip

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// VP8X feature flags.
const (
	webpFlagICC  = 0x20
	webpFlagEXIF = 0x08
	webpFlagXMP  = 0x04
)

func stripWebP(b []byte, o Options) (*Result, error) {
	r := &Result{}
	end := 8 + int(binary.LittleEndian.Uint32(b[4:]))
	if end > len(b) {
		return nil, errors.New("webp: truncated file")
	}
	if extra := len(b) - end; extra > 0 {
		r.add("data after RIFF", extra, "trailer", b[end:])
	}

	var kept [][]byte
	vp8x, hasICC := -1, false
	for i := 12; i < end; {
		if i+8 > end {
			return nil, fmt.Errorf("webp: truncated chunk at offset %d", i)
		}
		fcc := string(b[i : i+4])
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		dataEnd := i + 8 + n
		if dataEnd > end || dataEnd < i {
			return nil, fmt.Errorf("webp: bad chunk length at offset %d", i)
		}
		chunkEnd := min(dataEnd+n&1, end) // tolerate a missing final pad byte
		chunk := b[i:chunkEnd]
		i = chunkEnd

		switch fcc {
		case "VP8 ", "VP8L", "ALPH", "ANIM", "ANMF":
			kept = append(kept, chunk)
		case "VP8X":
			if n < 10 {
				return nil, errors.New("webp: short VP8X chunk")
			}
			vp8x = len(kept)
			kept = append(kept, chunk)
		case "ICCP":
			if o.KeepICC {
				hasICC = true
				kept = append(kept, chunk)
			} else {
				r.add("ICCP ICC profile", len(chunk), "icc", chunk[8:8+n])
			}
		case "EXIF":
			if v := exifOrientation(chunk[8 : 8+n]); v != 0 {
				r.Orientation = v
			}
			r.add("EXIF Exif", len(chunk), "exif", chunk[8:8+n])
		case "C2PA":
			r.add("C2PA manifest", len(chunk), "jumbf", chunk[8:8+n])
		case "XMP ":
			r.add("XMP XMP", len(chunk), "xmp", chunk[8:8+n])
		default:
			r.add(fmt.Sprintf("%q chunk", fcc), len(chunk), "", nil)
		}
	}

	// Orientation can only be expressed in extended (VP8X) files.
	writeOrient := o.KeepOrientation && r.Orientation > 1 && vp8x >= 0
	if vp8x >= 0 {
		c := append([]byte(nil), kept[vp8x]...)
		c[8] &^= webpFlagICC | webpFlagEXIF | webpFlagXMP
		if hasICC {
			c[8] |= webpFlagICC
		}
		if writeOrient {
			c[8] |= webpFlagEXIF
		}
		kept[vp8x] = c
	}
	if writeOrient {
		tiff := orientationTIFF(r.Orientation)
		c := append([]byte("EXIF"), binary.LittleEndian.AppendUint32(nil, uint32(len(tiff)))...)
		kept = append(kept, append(c, tiff...))
	}

	out := append(make([]byte, 0, len(b)), "RIFF\x00\x00\x00\x00WEBP"...)
	for _, c := range kept {
		out = append(out, c...)
		if len(c)&1 == 1 {
			out = append(out, 0)
		}
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	r.Data = out
	return r, nil
}
