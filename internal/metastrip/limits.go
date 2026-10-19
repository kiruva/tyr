package metastrip

import (
	"errors"
	"fmt"
	"strings"
)

// Metadata is written by whoever made the file, and a few hundred kilobytes of
// it can be built to decode into gigabytes: one string pointed at by thousands
// of EXIF entries, a zlib bomb in a text chunk, XML nested thousands deep. The
// limits below keep what Inspect reads and reports proportionate to what a
// person could look at.
const (
	maxValueBytes  = 4 << 10  // one value shown; the rest is summarised as its length
	maxItemFields  = 256      // fields decoded from one item
	maxReportBytes = 1 << 20  // keys and values across one report
	maxInflate     = 16 << 20 // bytes decompressed across one image
	maxCBORItems   = 100_000  // values in one CBOR document
	maxXMPDepth    = 64       // element nesting an XMP property is read at
	maxKeyBytes    = 256      // one key, such as an XMP property path
)

// budget is what is left to spend while inspecting one image.
type budget struct {
	inflate int // decompressed bytes still allowed
}

func newBudget() *budget { return &budget{inflate: maxInflate} }

var errBudget = errors.New("over the decompression budget")

// clip converts b with conv, converting at most maxValueBytes of it and
// saying how long it really was.
func clip(b []byte, conv func([]byte) string) string {
	if len(b) <= maxValueBytes {
		return conv(b)
	}
	return strings.ToValidUTF8(conv(b[:maxValueBytes]), "") + fmt.Sprintf("… (%d bytes)", len(b))
}

// clipString is clip for a value that is already a string.
func clipString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return strings.ToValidUTF8(s[:limit], "") + fmt.Sprintf("… (%d bytes)", len(s))
}

// span is b[off:off+size], or false when that runs past the end of b. The sum
// is never formed in an int: offsets and sizes come from the file as 32-bit
// values, and added in a 32-bit int they can wrap round to something that
// looks in range.
func span(b []byte, off, size uint64) ([]byte, bool) {
	if off > uint64(len(b)) || size > uint64(len(b))-off {
		return nil, false
	}
	return b[off : off+size], true
}
