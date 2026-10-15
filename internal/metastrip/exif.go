package metastrip

import (
	"bytes"
	"encoding/binary"
)

var exifHeader = []byte("Exif\x00\x00")

// exifOrientation reads tag 0x0112 from IFD0 of a TIFF-structured EXIF blob.
// A leading "Exif\0\0" header is tolerated. Returns 0 if absent or invalid.
func exifOrientation(tiff []byte) int {
	tiff = bytes.TrimPrefix(tiff, exifHeader)
	if len(tiff) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	if bo.Uint16(tiff[2:]) != 42 {
		return 0
	}
	off := int(bo.Uint32(tiff[4:]))
	if off < 8 || off+2 > len(tiff) {
		return 0
	}
	n := int(bo.Uint16(tiff[off:]))
	for k := range n {
		e := off + 2 + 12*k
		if e+12 > len(tiff) {
			break
		}
		if bo.Uint16(tiff[e:]) == 0x0112 {
			if v := int(bo.Uint16(tiff[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 0
		}
	}
	return 0
}

// orientationTIFF builds a minimal big-endian TIFF blob whose IFD0 holds only
// the orientation tag.
func orientationTIFF(v int) []byte {
	return []byte{
		'M', 'M', 0, 42, 0, 0, 0, 8, // header, IFD0 at offset 8
		0, 1, // one entry
		0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(v), 0, 0, // Orientation, SHORT, count 1
		0, 0, 0, 0, // no next IFD
	}
}
