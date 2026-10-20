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

// ErrUnsupported is returned by Strip for anything that is not one of the four
// formats it handles. A format it recognises but does not handle (HEIC, TIFF)
// is named in the wrapping error.
var ErrUnsupported = errors.New("unsupported format")

var pngSig = []byte("\x89PNG\r\n\x1a\n")

// Detect names the image format of b — "jpeg", "png", "gif" or "webp" — or
// returns "" when b is none of them.
func Detect(b []byte) string {
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

// Unsupported names a common image format Strip recognises but cannot handle,
// such as "HEIC/AVIF", or returns "".
func Unsupported(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("II*\x00")), bytes.HasPrefix(b, []byte("MM\x00*")):
		return "TIFF/RAW"
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return "HEIC/AVIF"
	}
	return ""
}

// Strip removes every piece of metadata from b that o does not keep, without
// re-encoding image data. The input is not modified; Result.Data is the
// cleaned image.
func Strip(b []byte, o Options) (*Result, error) {
	switch Detect(b) {
	case "jpeg":
		return stripJPEG(b, o)
	case "png":
		return stripPNG(b, o)
	case "gif":
		return stripGIF(b, o)
	case "webp":
		return stripWebP(b, o)
	}
	if kind := Unsupported(b); kind != "" {
		return nil, fmt.Errorf("%w (%s)", ErrUnsupported, kind)
	}
	return nil, ErrUnsupported
}
