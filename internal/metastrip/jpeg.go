package metastrip

import (
	"bytes"
	"fmt"
	"slices"
)

func stripJPEG(b []byte, o Options) (*Result, error) {
	r := &Result{}
	parts := map[string]int{}    // group -> index of first item, for multi-segment payloads
	pieces := map[int][][]byte{} // index of first item -> its payload, segment by segment
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

		if it, group, drop := jpegClassify(m, payload, o); drop {
			if m == 0xE1 && bytes.HasPrefix(payload, exifHeader) {
				if v := exifOrientation(payload); v != 0 {
					r.Orientation = v
				}
			}
			kind, data := it.Kind, it.Data
			if group != "" {
				// Large ICC profiles, extended XMP and C2PA manifests span several
				// segments: reassemble them into the first one. The pieces are
				// joined once at the end; appending each to the last copied
				// everything before it, a thousand segments over.
				if first, ok := parts[group]; ok {
					if kind == "jumbf" { // each segment repeats the JUMBF box header
						data = data[min(jumbfHeaderLen(data), len(data)):]
					}
					pieces[first] = append(pieces[first], data)
					kind, data = "cont", nil
				} else {
					parts[group] = len(r.Removed)
					pieces[len(r.Removed)] = [][]byte{data}
				}
			}
			r.add(it.Name, n+2, kind, data)
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

	for first, p := range pieces {
		if len(p) > 1 {
			r.Removed[first].Data = bytes.Join(p, nil)
		}
	}

	if o.KeepOrientation && r.Orientation > 1 {
		tiff := orientationTIFF(r.Orientation)
		n := 2 + len(exifHeader) + len(tiff)
		app1 := []byte{0xFF, 0xE1, byte(n >> 8), byte(n)}
		app1 = append(app1, exifHeader...)
		app1 = append(app1, tiff...)
		out = slices.Insert(out, 2, app1...)
	}
	r.Data = out
	return r, nil
}

// jpegClassify decides whether a segment is metadata to drop and, if it is,
// what it is called and what of it -i can decode (Size is left to the caller).
// Segments sharing a non-empty group are parts of one payload.
func jpegClassify(m byte, payload []byte, o Options) (it Item, group string, drop bool) {
	switch {
	case m == 0xFE:
		return Item{Name: "COM comment", Kind: "comment", Data: payload}, "", true
	case m == 0xEE && bytes.HasPrefix(payload, []byte("Adobe")):
		// Carries only the color transform flag, needed to decode CMYK/YCCK correctly.
		return Item{}, "", false
	case m == 0xEB && bytes.HasPrefix(payload, []byte("JP")):
		it := Item{Name: "APP11 JUMBF (C2PA)"}
		if len(payload) >= 8 {
			// "JP", box instance number, packet sequence number, then the JUMBF box.
			it.Kind, it.Data = "jumbf", payload[8:]
			group = "jumbf" + string(payload[2:4])
		}
		return it, group, true
	case m >= 0xE0 && m <= 0xEF:
		it := Item{Name: fmt.Sprintf("APP%d %s", m-0xE0, appID(payload))}
		drop := true
		if m == 0xE2 && bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00")) {
			it.Name, drop = "APP2 ICC profile", !o.KeepICC
		}
		it.Kind, group, it.Data = appPayload(payload)
		return it, group, drop
	}
	return Item{}, "", false
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

// appPayload picks the decodable part out of an APPn segment by the ID it
// starts with.
func appPayload(payload []byte) (kind, group string, data []byte) {
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
