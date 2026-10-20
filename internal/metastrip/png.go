package metastrip

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"slices"
)

// Ancillary chunks needed to render the image correctly. Everything else
// ancillary (text, time, EXIF, physical size, suggested palettes...) is dropped.
var pngKeep = map[string]bool{
	"tRNS": true, "gAMA": true, "cHRM": true, "sRGB": true, "sBIT": true,
	"cICP": true, "mDCv": true, "cLLi": true, // HDR / color signalling
	"acTL": true, "fcTL": true, "fdAT": true, // APNG animation
}

func stripPNG(b []byte, o Options) (*Result, error) {
	r := &Result{}
	out := append(make([]byte, 0, len(b)), pngSig...)
	ihdrEnd := -1
	i := len(pngSig)
	for {
		if i+12 > len(b) {
			return nil, fmt.Errorf("png: truncated chunk at offset %d", i)
		}
		n := int(binary.BigEndian.Uint32(b[i:]))
		typ := string(b[i+4 : i+8])
		end := i + 12 + n
		if end > len(b) || end < i {
			return nil, fmt.Errorf("png: bad chunk length at offset %d", i)
		}
		chunk := b[i:end]
		data := b[i+8 : i+8+n]
		i = end

		critical := typ[0]&0x20 == 0
		if critical || pngKeep[typ] || (typ == "iCCP" && o.KeepICC) {
			out = append(out, chunk...)
			if typ == "IHDR" {
				ihdrEnd = len(out)
			}
		} else {
			if typ == "eXIf" {
				if v := exifOrientation(data); v != 0 {
					r.Orientation = v
				}
			}
			kind := "png:" + typ
			switch typ {
			case "eXIf":
				kind = "exif"
			case "caBX":
				kind = "jumbf"
			}
			r.add(pngChunkName(typ, data), len(chunk), kind, data)
		}

		if typ == "IEND" {
			if extra := len(b) - i; extra > 0 {
				r.add("data after IEND", extra, "trailer", b[i:])
			}
			break
		}
	}

	if o.KeepOrientation && r.Orientation > 1 && ihdrEnd > 0 {
		c := pngChunk("eXIf", orientationTIFF(r.Orientation))
		out = slices.Insert(out, ihdrEnd, c...)
	}
	r.Data = out
	return r, nil
}

func pngChunk(typ string, data []byte) []byte {
	c := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	c = append(c, typ...)
	c = append(c, data...)
	return binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(c[4:]))
}

func pngChunkName(typ string, data []byte) string {
	switch typ {
	case "tEXt", "zTXt", "iTXt":
		if k, _, ok := bytes.Cut(data, []byte{0}); ok && len(k) > 0 && len(k) <= 79 {
			return fmt.Sprintf("%s %q", typ, k)
		}
	case "eXIf":
		return "eXIf Exif"
	case "iCCP":
		return "iCCP ICC profile"
	case "caBX":
		return "caBX C2PA manifest"
	case "tIME":
		return "tIME modification time"
	}
	return fmt.Sprintf("%q chunk", typ)
}
