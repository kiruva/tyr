package metastrip

import (
	"bytes"
	"fmt"
	"slices"
)

func stripJPEG(b []byte, o Options) (*Result, error) {
	r := &Result{}
	parts := map[string]int{} // group -> index of first item, for multi-segment payloads
	out := make([]byte, 0, len(b))
	out = append(out, 0xFF, 0xD8)
	i := 2
	for {
		if i >= len(b) {
			return nil, fmt.Errorf("jpeg: missing EOI marker")
		}
		if b[i] != 0xFF {
			return nil, fmt.Errorf("jpeg: expected marker at offset %d", i)
		}
		for i < len(b) && b[i] == 0xFF { // fill bytes
			i++
		}
		if i >= len(b) {
			return nil, fmt.Errorf("jpeg: truncated marker")
		}
		m := b[i]
		i++

		if m == 0xD9 { // EOI
			out = append(out, 0xFF, 0xD9)
			if extra := len(b) - i; extra > 0 {
				r.add("data after EOI", extra, "trailer", b[i:])
			}
			break
		}
		if (m >= 0xD0 && m <= 0xD7) || m == 0x01 { // standalone markers
			out = append(out, 0xFF, m)
			continue
		}

		if i+2 > len(b) {
			return nil, fmt.Errorf("jpeg: truncated segment at offset %d", i)
		}
		n := int(b[i])<<8 | int(b[i+1])
		if n < 2 || i+n > len(b) {
			return nil, fmt.Errorf("jpeg: bad segment length at offset %d", i)
		}
		seg := b[i : i+n] // length field + payload
		payload := seg[2:]
		i += n

		if name, drop := jpegClassify(m, payload, o); drop {
			if m == 0xE1 && bytes.HasPrefix(payload, exifHeader) {
				if v := exifOrientation(payload); v != 0 {
					r.Orientation = v
				}
			}
			kind, group, data := jpegPayload(m, payload)
			if group != "" {
				// Large ICC profiles, extended XMP and C2PA manifests span several
				// segments: reassemble them into the first one.
				if first, ok := parts[group]; ok {
					if kind == "jumbf" { // each segment repeats the JUMBF box header
						data = data[min(jumbfHeaderLen(data), len(data)):]
					}
					prev := &r.Removed[first]
					prev.Data = append(slices.Clip(prev.Data), data...)
					kind, data = "cont", nil
				} else {
					parts[group] = len(r.Removed)
				}
			}
			r.add(name, n+2, kind, data)
		} else {
			out = append(out, 0xFF, m)
			out = append(out, seg...)
		}

		if m == 0xDA { // SOS: copy entropy-coded data up to the next real marker
			start := i
			for i < len(b) && !startsMarker(b, i) {
				i++
			}
			out = append(out, b[start:i]...)
			if i >= len(b) { // truncated file: keep what is there
				break
			}
		}
	}

	if o.KeepOrientation && r.Orientation > 1 {
		tiff := orientationTIFF(r.Orientation)
		n := 2 + len(exifHeader) + len(tiff)
		app1 := []byte{0xFF, 0xE1, byte(n >> 8), byte(n)}
		app1 = append(app1, exifHeader...)
		app1 = append(app1, tiff...)
		out = append(out[:2], append(app1, out[2:]...)...)
	}
	r.Data = out
	return r, nil
}

// jpegClassify decides whether a segment is metadata to drop.
func jpegClassify(m byte, payload []byte, o Options) (name string, drop bool) {
	switch {
	case m == 0xFE:
		return "COM comment", true
	case m == 0xE2 && bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00")):
		return "APP2 ICC profile", !o.KeepICC
	case m == 0xEB && bytes.HasPrefix(payload, []byte("JP")):
		return "APP11 JUMBF (C2PA)", true
	case m == 0xEE && bytes.HasPrefix(payload, []byte("Adobe")):
		// Carries only the color transform flag, needed to decode CMYK/YCCK correctly.
		return "", false
	case m >= 0xE0 && m <= 0xEF:
		return fmt.Sprintf("APP%d %s", m-0xE0, appID(payload)), true
	}
	return "", false
}

var knownAppIDs = []struct{ prefix, name, kind string }{
	{"Exif\x00\x00", "Exif", "exif"},
	{"http://ns.adobe.com/xap/1.0/\x00", "XMP", "xmp"},
	{"http://ns.adobe.com/xmp/extension/\x00", "XMP extension", "xmp-ext"},
	{"Photoshop 3.0\x00", "Photoshop/IPTC", "photoshop"},
	{"JFIF\x00", "JFIF", "jfif"},
	{"JFXX\x00", "JFIF thumbnail", ""},
	{"MPF\x00", "MPF multi-picture", ""},
	{"Ducky", "Ducky", ""},
	{"ICC_PROFILE\x00", "ICC profile", "icc"},
}

// jpegPayload extracts decodable metadata from a dropped segment for -i.
// Segments sharing a non-empty group are parts of one payload.
func jpegPayload(m byte, payload []byte) (kind, group string, data []byte) {
	switch {
	case m == 0xFE:
		return "comment", "", payload
	case m == 0xEB && bytes.HasPrefix(payload, []byte("JP")) && len(payload) >= 8:
		// "JP", box instance number, packet sequence number, then the JUMBF box.
		return "jumbf", "jumbf" + string(payload[2:4]), payload[8:]
	}
	for _, k := range knownAppIDs {
		if k.kind == "" || !bytes.HasPrefix(payload, []byte(k.prefix)) {
			continue
		}
		data := payload[len(k.prefix):]
		switch k.kind {
		case "icc": // sequence number, segment count
			return k.kind, k.kind, data[min(2, len(data)):]
		case "xmp-ext": // GUID, full length, offset
			return k.kind, k.kind, data[min(40, len(data)):]
		}
		return k.kind, "", data
	}
	return "", "", nil
}

func appID(p []byte) string {
	for _, k := range knownAppIDs {
		if bytes.HasPrefix(p, []byte(k.prefix)) {
			return k.name
		}
	}
	j := 0
	for j < len(p) && j < 32 && p[j] >= 0x20 && p[j] < 0x7F {
		j++
	}
	if j == 0 {
		return "data"
	}
	return fmt.Sprintf("%q", p[:j])
}

// startsMarker reports whether b[i] begins a real marker inside entropy-coded
// data: an 0xFF that is neither a stuffed byte (0xFF00) nor a restart marker
// (RST0–RST7), both of which belong to the scan.
func startsMarker(b []byte, i int) bool {
	if b[i] != 0xFF || i+1 >= len(b) {
		return false
	}
	next := b[i+1]
	return next != 0x00 && (next < 0xD0 || next > 0xD7)
}
