package metastrip

import (
	"bytes"
	"errors"
	"fmt"
)

// Options controls what little metadata may survive stripping.
type Options struct {
	KeepICC         bool // keep embedded ICC color profiles
	KeepOrientation bool // re-emit a minimal EXIF block holding only the orientation tag
}

// Item is one removed piece of metadata.
type Item struct {
	Name string
	Size int
	Kind string // how to decode Data for -i, see describe; "" if not decodable
	Data []byte // metadata payload, may alias the input
}

// Result is the stripped image plus a record of what was removed.
type Result struct {
	Data        []byte
	Removed     []Item
	Orientation int // EXIF orientation found in the input, 0 if none
}

func (r *Result) add(name string, size int, kind string, data []byte) {
	r.Removed = append(r.Removed, Item{name, size, kind, data})
}

var errUnsupported = errors.New("unsupported format")

var pngSig = []byte("\x89PNG\r\n\x1a\n")

func detect(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "jpeg"
	case bytes.HasPrefix(b, pngSig):
		return "png"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	}
	return ""
}

// describeUnsupported names common formats we recognise but don't handle.
func describeUnsupported(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("II*\x00")), bytes.HasPrefix(b, []byte("MM\x00*")):
		return "TIFF/RAW"
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return "HEIC/AVIF"
	}
	return ""
}

// strip removes all metadata from b without re-encoding image data.
func strip(b []byte, o Options) (*Result, error) {
	switch detect(b) {
	case "jpeg":
		return stripJPEG(b, o)
	case "png":
		return stripPNG(b, o)
	case "gif":
		return stripGIF(b, o)
	case "webp":
		return stripWebP(b, o)
	}
	if kind := describeUnsupported(b); kind != "" {
		return nil, fmt.Errorf("%w (%s)", errUnsupported, kind)
	}
	return nil, errUnsupported
}
