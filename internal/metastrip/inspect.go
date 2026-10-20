package metastrip

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// describe decodes an item's payload into human-readable fields for -i,
// spending from bud what decompression it needs.
func describe(it Item, bud *budget) []Field {
	switch it.Kind {
	case "exif":
		return parseEXIF(it.Data)
	case "xmp", "xmp-ext":
		return parseXMP(it.Data)
	case "jumbf":
		return describeJUMBF(it.Data)
	case "photoshop":
		return parsePhotoshop(it.Data)
	case "icc":
		return parseICC(it.Data)
	case "comment":
		return []Field{{"Comment", clip(it.Data, latin1OrUTF8)}}
	case "jfif":
		return parseJFIF(it.Data)
	case "trailer":
		return []Field{{"", describeTrailer(it.Data)}}
	case "cont":
		return []Field{{"", "continuation, decoded with the first part above"}}
	case "png:tEXt", "png:zTXt", "png:iTXt":
		return parsePNGText(it.Kind[4:], it.Data, bud)
	case "png:tIME":
		if d := it.Data; len(d) == 7 {
			return []Field{{"ModifyDate", fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d UTC",
				binary.BigEndian.Uint16(d), d[2], d[3], d[4], d[5], d[6])}}
		}
	case "png:pHYs":
		if d := it.Data; len(d) == 9 {
			x, y := binary.BigEndian.Uint32(d), binary.BigEndian.Uint32(d[4:])
			if d[8] == 1 {
				return []Field{{"Resolution", fmt.Sprintf("%.0f x %.0f dpi", float64(x)*0.0254, float64(y)*0.0254)}}
			}
			return []Field{{"PixelAspect", fmt.Sprintf("%d:%d", x, y)}}
		}
	case "png:iCCP":
		name, rest, ok := bytes.Cut(it.Data, []byte{0})
		if !ok || len(rest) < 1 {
			break
		}
		f := []Field{{"ProfileName", clip(name, latin1)}}
		if prof, err := inflate(rest[1:], bud); err == nil {
			f = append(f, parseICC(prof)...)
		}
		return f
	}
	return nil
}

// ---- EXIF ----

var tiffTypeSize = [...]int{0, 1, 1, 2, 4, 8, 1, 1, 2, 4, 8, 4, 8}

var ifd0Tags = map[uint16]string{
	0x0100: "ImageWidth", 0x0101: "ImageHeight", 0x0102: "BitsPerSample", 0x0103: "Compression",
	0x0106: "PhotometricInterpretation", 0x010E: "ImageDescription", 0x010F: "Make", 0x0110: "Model",
	0x0112: "Orientation", 0x0115: "SamplesPerPixel", 0x011A: "XResolution", 0x011B: "YResolution",
	0x0128: "ResolutionUnit", 0x0131: "Software", 0x0132: "ModifyDate", 0x013B: "Artist",
	0x013C: "HostComputer", 0x0201: "ThumbnailOffset", 0x0202: "ThumbnailLength",
	0x0142: "TileWidth", 0x0143: "TileLength", 0x0213: "YCbCrPositioning", 0x4746: "Rating", 0x8298: "Copyright",
	0x9C9B: "XPTitle", 0x9C9C: "XPComment", 0x9C9D: "XPAuthor", 0x9C9E: "XPKeywords", 0x9C9F: "XPSubject",
}

var exifTags = map[uint16]string{
	0x829A: "ExposureTime", 0x829D: "FNumber", 0x8822: "ExposureProgram", 0x8827: "ISO",
	0x9000: "ExifVersion", 0x9003: "DateTimeOriginal", 0x9004: "CreateDate", 0x9010: "OffsetTime",
	0x9011: "OffsetTimeOriginal", 0x9012: "OffsetTimeDigitized", 0x9101: "ComponentsConfiguration",
	0x9201: "ShutterSpeedValue", 0x9202: "ApertureValue", 0x9203: "BrightnessValue",
	0x9204: "ExposureCompensation", 0x9205: "MaxApertureValue", 0x9206: "SubjectDistance",
	0x9207: "MeteringMode", 0x9208: "LightSource", 0x9209: "Flash", 0x920A: "FocalLength",
	0x9214: "SubjectArea", 0x927C: "MakerNote", 0x9286: "UserComment", 0x9290: "SubSecTime",
	0x9291: "SubSecTimeOriginal", 0x9292: "SubSecTimeDigitized", 0xA000: "FlashpixVersion",
	0xA001: "ColorSpace", 0xA002: "ExifImageWidth", 0xA003: "ExifImageHeight",
	0xA217: "SensingMethod", 0xA300: "FileSource", 0xA301: "SceneType", 0xA401: "CustomRendered",
	0xA402: "ExposureMode", 0xA403: "WhiteBalance", 0xA404: "DigitalZoomRatio",
	0xA405: "FocalLengthIn35mmFormat", 0xA406: "SceneCaptureType", 0xA420: "ImageUniqueID",
	0xA430: "OwnerName", 0xA431: "SerialNumber", 0xA432: "LensInfo", 0xA433: "LensMake",
	0xA434: "LensModel", 0xA435: "LensSerialNumber", 0xA460: "CompositeImage",
}

var gpsTags = map[uint16]string{
	0: "GPSVersionID", 1: "GPSLatitudeRef", 2: "GPSLatitude", 3: "GPSLongitudeRef", 4: "GPSLongitude",
	5: "GPSAltitudeRef", 6: "GPSAltitude", 7: "GPSTimeStamp", 8: "GPSSatellites", 9: "GPSStatus",
	10: "GPSMeasureMode", 11: "GPSDOP", 12: "GPSSpeedRef", 13: "GPSSpeed", 14: "GPSTrackRef",
	15: "GPSTrack", 16: "GPSImgDirectionRef", 17: "GPSImgDirection", 18: "GPSMapDatum",
	23: "GPSDestBearingRef", 24: "GPSDestBearing", 27: "GPSProcessingMethod", 29: "GPSDateStamp",
	31: "GPSHPositioningError",
}

var interopTags = map[uint16]string{1: "InteropIndex", 2: "InteropVersion"}

var orientationNames = [...]string{"", "normal", "mirror horizontal", "rotate 180", "mirror vertical",
	"mirror horizontal, rotate 270 CW", "rotate 90 CW", "mirror horizontal, rotate 90 CW", "rotate 270 CW"}

func parseEXIF(b []byte) []Field {
	b, bo := tiffHeader(b)
	switch {
	case len(b) < 8:
		return []Field{{"", "invalid EXIF"}}
	case bo == nil:
		return []Field{{"", "invalid EXIF byte order"}}
	}

	var out []Field
	seen := map[int]bool{}
	var lat, lon float64
	var latRef, lonRef string
	haveLat, haveLon := false, false

	var walk func(off int, ifd string, names map[uint16]string)
	walk = func(off int, ifd string, names map[uint16]string) {
		if off < 8 || off+2 > len(b) || seen[off] || len(seen) > 8 {
			return
		}
		seen[off] = true
		n := int(bo.Uint16(b[off:]))
		for k := range n {
			e := off + 2 + 12*k
			if e+12 > len(b) || len(out) >= maxItemFields {
				return
			}
			tag, typ, cnt64 := bo.Uint16(b[e:]), int(bo.Uint16(b[e+2:])), uint64(bo.Uint32(b[e+4:]))
			if typ < 1 || typ >= len(tiffTypeSize) {
				continue
			}
			name := names[tag]
			if name == "" {
				name = fmt.Sprintf("%s:0x%04X", ifd, tag)
			} else if ifd == "IFD1" {
				name = "Thumbnail" + name
			}
			size := uint64(tiffTypeSize[typ]) * cnt64
			var val []byte
			if size <= 4 {
				val = b[e+8 : e+8+int(size)]
			} else {
				v, ok := span(b, uint64(bo.Uint32(b[e+8:])), size)
				if !ok {
					out = append(out, Field{name, "<invalid offset>"})
					continue
				}
				val = v
			}
			cnt := int(cnt64) // no more than len(b) now, so it fits

			if len(val) >= 4 && ifd != "GPS" && ifd != "Interop" {
				switch tag {
				case 0x8769:
					walk(int(bo.Uint32(val)), "ExifIFD", exifTags)
					continue
				case 0x8825:
					walk(int(bo.Uint32(val)), "GPS", gpsTags)
					continue
				case 0xA005:
					walk(int(bo.Uint32(val)), "Interop", interopTags)
					continue
				}
			}

			v := formatTIFFValue(bo, ifd, tag, typ, cnt, val)
			if ifd == "GPS" {
				switch tag {
				case 1:
					latRef = v
				case 3:
					lonRef = v
				case 2:
					lat, haveLat = dms(bo, typ, val)
				case 4:
					lon, haveLon = dms(bo, typ, val)
				}
			}
			out = append(out, Field{name, v})
		}
		if ifd == "IFD0" {
			if e := off + 2 + 12*n; e+4 <= len(b) {
				walk(int(bo.Uint32(b[e:])), "IFD1", ifd0Tags)
			}
		}
	}
	walk(int(bo.Uint32(b[4:])), "IFD0", ifd0Tags)

	if haveLat && haveLon {
		if latRef == "S" {
			lat = -lat
		}
		if lonRef == "W" {
			lon = -lon
		}
		out = append(out, Field{"GPSPosition", fmt.Sprintf("%.6f, %.6f", lat, lon)})
	}
	return out
}

// dms converts three unsigned rationals (degrees, minutes, seconds) to decimal degrees.
func dms(bo binary.ByteOrder, typ int, val []byte) (float64, bool) {
	if typ != 5 || len(val) < 24 {
		return 0, false
	}
	var d [3]float64
	for i := range d {
		num, den := bo.Uint32(val[8*i:]), bo.Uint32(val[8*i+4:])
		if den == 0 {
			return 0, false
		}
		d[i] = float64(num) / float64(den)
	}
	return d[0] + d[1]/60 + d[2]/3600, true
}

func formatTIFFValue(bo binary.ByteOrder, ifd string, tag uint16, typ, cnt int, val []byte) string {
	switch typ {
	case 2: // ASCII
		return clip(val, func(v []byte) string {
			return strings.TrimRight(string(bytes.ReplaceAll(v, []byte{0}, []byte(" "))), " ")
		})
	case 1, 6, 7: // BYTE, SBYTE, UNDEFINED
		switch {
		case ifd != "GPS" && tag >= 0x9C9B && tag <= 0x9C9F:
			return decodeUTF16(val, binary.LittleEndian)
		case tag == 0x927C && ifd == "ExifIFD":
			return fmt.Sprintf("<%d bytes, camera vendor data>", len(val))
		case tag == 0x9286 || (ifd == "GPS" && tag == 27):
			return userComment(bo, val)
		case ifd == "GPS" && tag == 0:
			parts := make([]string, len(val))
			for i, c := range val {
				parts[i] = strconv.Itoa(int(c))
			}
			return strings.Join(parts, ".")
		}
		return bytesValue(val)
	}

	var vals []string
	for i := 0; i < cnt && i < 16; i++ {
		var s string
		switch typ {
		case 3:
			s = strconv.Itoa(int(bo.Uint16(val[2*i:])))
		case 8:
			s = strconv.Itoa(int(int16(bo.Uint16(val[2*i:]))))
		case 4:
			s = strconv.FormatUint(uint64(bo.Uint32(val[4*i:])), 10)
		case 9:
			s = strconv.Itoa(int(int32(bo.Uint32(val[4*i:]))))
		case 5:
			s = rational(float64(bo.Uint32(val[8*i:])), float64(bo.Uint32(val[8*i+4:])), tag == 0x829A)
		case 10:
			s = rational(float64(int32(bo.Uint32(val[8*i:]))), float64(int32(bo.Uint32(val[8*i+4:]))), false)
		case 11:
			s = trimFloat(float64(math.Float32frombits(bo.Uint32(val[4*i:]))))
		case 12:
			s = trimFloat(math.Float64frombits(bo.Uint64(val[8*i:])))
		}
		vals = append(vals, s)
	}
	s := strings.Join(vals, " ")
	if cnt > 16 {
		s += fmt.Sprintf(" ... (%d values)", cnt)
	}
	if ifd != "GPS" && tag == 0x0112 && cnt == 1 {
		if v := int(bo.Uint16(val)); v >= 1 && v <= 8 {
			s += " (" + orientationNames[v] + ")"
		}
	}
	return s
}

func rational(num, den float64, exposure bool) string {
	switch {
	case den == 0:
		return "undefined"
	case exposure && num > 0 && num < den:
		return fmt.Sprintf("1/%s", trimFloat(math.Round(den/num)))
	}
	return trimFloat(num / den)
}

func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

func userComment(bo binary.ByteOrder, v []byte) string {
	if len(v) < 8 {
		return bytesValue(v)
	}
	text := v[8:]
	switch string(v[:8]) {
	case "UNICODE\x00":
		return strings.TrimRight(decodeUTF16(text, bo), " ")
	case "ASCII\x00\x00\x00", "\x00\x00\x00\x00\x00\x00\x00\x00":
		return clip(bytes.TrimRight(text, "\x00"), func(v []byte) string { return strings.TrimRight(string(v), " ") })
	}
	return bytesValue(v)
}

// bytesValue shows printable byte strings as text, others as hex or a size.
func bytesValue(v []byte) string {
	t := bytes.TrimRight(v, "\x00")
	printable := len(t) > 0
	for _, c := range t {
		if c < 0x20 || c > 0x7E {
			printable = false
			break
		}
	}
	switch {
	case printable:
		return clip(t, func(v []byte) string { return string(v) })
	case len(v) <= 16:
		return fmt.Sprintf("% x", v)
	}
	return fmt.Sprintf("<%d bytes>", len(v))
}

func decodeUTF16(b []byte, bo binary.ByteOrder) string {
	more := ""
	if len(b) > 2*maxValueBytes {
		b, more = b[:2*maxValueBytes], fmt.Sprintf("… (%d bytes)", len(b))
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, bo.Uint16(b[i:]))
	}
	return strings.TrimRight(string(utf16.Decode(u)), "\x00") + more
}

// ---- XMP ----

const rdfNS = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"

func parseXMP(b []byte) []Field {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	prefixes := map[string]string{"http://www.w3.org/XML/1998/namespace": "xml"}
	qname := func(n xml.Name) string {
		if p, ok := prefixes[n.Space]; ok {
			return p + ":" + n.Local
		}
		return n.Local
	}
	var out []Field
	var path []string // property path, "" for rdf and wrapper elements
	// Below maxXMPDepth an element still counts towards the nesting, so its
	// end tag is matched, but nothing inside it is reported: real XMP is a
	// handful of levels deep, and a path is rebuilt for every value.
	shallow := func() bool { return len(path) <= maxXMPDepth }
	for len(out) < maxItemFields {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" {
					prefixes[a.Value] = a.Name.Local
				}
			}
			isProp := t.Name.Space != rdfNS && t.Name.Space != "adobe:ns:meta/"
			seg := ""
			if isProp {
				seg = qname(t.Name)
			}
			path = append(path, seg)
			if !shallow() {
				continue
			}
			// Properties in attribute form: <rdf:Description dc:format="..."> or struct fields.
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Space == rdfNS || a.Name.Space == "" ||
					a.Name.Space == "http://www.w3.org/XML/1998/namespace" {
					continue
				}
				out = append(out, Field{joinPath(path, qname(a.Name)), clipString(a.Value, maxValueBytes)})
			}
		case xml.EndElement:
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		case xml.CharData:
			if len(path) > 0 && shallow() {
				if s := strings.TrimSpace(clip(t, func(v []byte) string { return string(v) })); s != "" {
					out = append(out, Field{joinPath(path, ""), s})
				}
			}
		}
	}
	if len(out) == 0 && len(b) > 0 {
		return []Field{{"", fmt.Sprintf("<%d bytes, no readable properties>", len(b))}}
	}
	return out
}

func joinPath(path []string, last string) string {
	var parts []string
	for _, p := range path {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if last != "" {
		parts = append(parts, last)
	}
	return clipString(strings.Join(parts, "/"), maxKeyBytes)
}

// ---- Photoshop / IPTC ----

var iptcNames = map[int]string{
	0x0100: "EnvelopeRecordVersion", 0x015A: "CodedCharacterSet",
	0x0200: "ApplicationRecordVersion", 0x0205: "ObjectName", 0x020A: "Urgency", 0x020F: "Category",
	0x0214: "SupplementalCategories", 0x0219: "Keywords", 0x0228: "SpecialInstructions",
	0x0237: "DateCreated", 0x023C: "TimeCreated", 0x0241: "OriginatingProgram", 0x0246: "ProgramVersion",
	0x0250: "By-line", 0x0255: "By-lineTitle", 0x025A: "City", 0x025C: "Sub-location",
	0x025F: "Province-State", 0x0264: "Country-PrimaryLocationCode", 0x0265: "Country-PrimaryLocationName",
	0x0267: "OriginalTransmissionReference", 0x0269: "Headline", 0x026E: "Credit", 0x0273: "Source",
	0x0274: "CopyrightNotice", 0x0276: "Contact", 0x0278: "Caption-Abstract", 0x027A: "Writer-Editor",
}

func parsePhotoshop(b []byte) []Field {
	var out []Field
	for i := 0; i+12 <= len(b) && string(b[i:i+4]) == "8BIM" && len(out) < maxItemFields; {
		id := binary.BigEndian.Uint16(b[i+4:])
		nl := int(b[i+6])
		j := i + 7 + nl + (nl+1)&1 // Pascal name, padded to even length
		if j+4 > len(b) {
			break
		}
		data, ok := span(b, uint64(j+4), uint64(binary.BigEndian.Uint32(b[j:])))
		if !ok {
			break
		}
		sz := len(data)
		i = j + 4 + sz + sz&1
		switch id {
		case 0x0404:
			out = append(out, parseIPTC(data)...)
		case 0x0409, 0x040C:
			out = append(out, Field{"PhotoshopThumbnail", fmt.Sprintf("<%d bytes, embedded JPEG>", sz)})
		case 0x0425:
			// MD5 of the IPTC block, carries no information of its own.
		default:
			out = append(out, Field{fmt.Sprintf("PhotoshopResource:0x%04X", id), bytesValue(data)})
		}
	}
	return out
}

func parseIPTC(b []byte) []Field {
	var out []Field
	for i := 0; i+5 <= len(b) && b[i] == 0x1C && len(out) < maxItemFields; {
		rec, ds := int(b[i+1]), int(b[i+2])
		n := int(binary.BigEndian.Uint16(b[i+3:]))
		i += 5
		if n&0x8000 != 0 || i+n > len(b) { // extended length datasets: stop
			break
		}
		data := b[i : i+n]
		i += n
		name := iptcNames[rec<<8|ds]
		if name == "" {
			name = fmt.Sprintf("IPTC:%d:%d", rec, ds)
		}
		v := clip(data, latin1OrUTF8)
		switch {
		case ds == 0 && n == 2:
			v = strconv.Itoa(int(binary.BigEndian.Uint16(data)))
		case rec == 1 && ds == 90:
			v = fmt.Sprintf("% x", data)
		}
		out = append(out, Field{name, v})
	}
	return out
}

// ---- ICC ----

func parseICC(b []byte) []Field {
	if len(b) < 132 {
		return []Field{{"", fmt.Sprintf("truncated ICC profile (%d bytes)", len(b))}}
	}
	be := binary.BigEndian
	out := []Field{
		{"ProfileClass", sig(b[12:16])},
		{"ColorSpace", sig(b[16:20])},
		{"ProfileVersion", fmt.Sprintf("%d.%d", b[8], b[9]>>4)},
	}
	if y := be.Uint16(b[24:]); y != 0 {
		out = append(out, Field{"ProfileDate", fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d",
			y, be.Uint16(b[26:]), be.Uint16(b[28:]), be.Uint16(b[30:]), be.Uint16(b[32:]), be.Uint16(b[34:]))})
	}
	for _, h := range []struct {
		name string
		off  int
	}{{"ProfileCMM", 4}, {"DeviceManufacturer", 48}, {"DeviceModel", 52}, {"ProfileCreator", 80}} {
		if s := sig(b[h.off : h.off+4]); s != "" {
			out = append(out, Field{h.name, s})
		}
	}
	tagNames := map[string]string{"desc": "ProfileDescription", "cprt": "ProfileCopyright",
		"dmnd": "DeviceMfgDesc", "dmdd": "DeviceModelDesc"}
	n := int(be.Uint32(b[128:]))
	for k := 0; k < n && k < 256; k++ {
		e := 132 + 12*k
		if e+12 > len(b) {
			break
		}
		name := tagNames[string(b[e:e+4])]
		tag, ok := span(b, uint64(be.Uint32(b[e+4:])), uint64(be.Uint32(b[e+8:])))
		if name == "" || !ok {
			continue
		}
		if s := iccText(tag); s != "" {
			out = append(out, Field{name, s})
		}
	}
	return out
}

func iccText(t []byte) string {
	if len(t) < 12 {
		return ""
	}
	be := binary.BigEndian
	switch string(t[:4]) {
	case "desc":
		if s, ok := span(t, 12, uint64(be.Uint32(t[8:]))); ok {
			return clip(bytes.TrimRight(s, "\x00"), func(v []byte) string { return string(v) })
		}
	case "text":
		return clip(bytes.TrimRight(t[8:], "\x00"), func(v []byte) string { return string(v) })
	case "mluc":
		if len(t) < 28 || be.Uint32(t[8:]) == 0 {
			return ""
		}
		if s, ok := span(t, uint64(be.Uint32(t[24:])), uint64(be.Uint32(t[20:]))); ok {
			return decodeUTF16(s, be)
		}
	}
	return ""
}

// sig renders an ICC four-character signature, "" if blank or not text.
func sig(b []byte) string {
	s := strings.TrimRight(string(b), " \x00")
	for _, c := range s {
		if c < 0x20 || c > 0x7E {
			return ""
		}
	}
	return s
}

// ---- JFIF, PNG text, trailers ----

func parseJFIF(d []byte) []Field {
	if len(d) < 9 {
		return nil
	}
	x, y := binary.BigEndian.Uint16(d[3:]), binary.BigEndian.Uint16(d[5:])
	units := [...]string{" (aspect ratio)", " dpi", " dots/cm"}[min(int(d[2]), 2)]
	out := []Field{
		{"JFIFVersion", fmt.Sprintf("%d.%02d", d[0], d[1])},
		{"Resolution", fmt.Sprintf("%d x %d%s", x, y, units)},
	}
	if d[7] > 0 && d[8] > 0 {
		out = append(out, Field{"Thumbnail", fmt.Sprintf("%dx%d", d[7], d[8])})
	}
	return out
}

func parsePNGText(typ string, d []byte, bud *budget) []Field {
	key, rest, ok := bytes.Cut(d, []byte{0})
	if !ok {
		return nil
	}
	// Every PNG keyword is Latin-1, the iTXt one included.
	k := clip(key, latin1)
	var text []byte
	switch typ {
	case "tEXt":
		text = rest
	case "zTXt":
		if len(rest) < 1 {
			return nil
		}
		t, err := inflate(rest[1:], bud)
		if err != nil {
			return []Field{{k, inflateNote(err)}}
		}
		text = t
	case "iTXt":
		if len(rest) < 2 {
			return nil
		}
		compressed := rest[0] == 1
		parts := bytes.SplitN(rest[2:], []byte{0}, 3) // language, translated keyword, text
		if len(parts) < 3 {
			return nil
		}
		text = parts[2]
		if compressed {
			t, err := inflate(text, bud)
			if err != nil {
				return []Field{{k, inflateNote(err)}}
			}
			text = t
		}
		if k == "XML:com.adobe.xmp" {
			return parseXMP(text)
		}
		return []Field{{k, clip(text, func(v []byte) string { return string(v) })}}
	}
	return []Field{{k, clip(text, latin1)}}
}

func inflateNote(err error) string {
	if errors.Is(err, errBudget) {
		return "<compressed text not shown: this image already decompressed to over 16 MB>"
	}
	return "<corrupt compressed text>"
}

func describeTrailer(d []byte) string {
	head := d[:min(len(d), 64)]
	switch {
	case bytes.HasPrefix(d, []byte{0xFF, 0xD8, 0xFF}):
		return "looks like an embedded JPEG (second image or preview)"
	case bytes.Contains(head, []byte("ftyp")):
		return "looks like embedded MP4/MOV video (motion photo)"
	}
	return fmt.Sprintf("starts with %q", head[:min(len(head), 24)])
}

// inflate decompresses zlib data. What one image may decompress is shared
// between all of its chunks: a cap per chunk let twenty small zlib bombs each
// expand to the cap.
func inflate(b []byte, bud *budget) ([]byte, error) {
	if bud.inflate <= 0 {
		return nil, errBudget
	}
	zr, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(io.LimitReader(zr, int64(bud.inflate)))
	bud.inflate -= len(out)
	return out, err
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func latin1OrUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return latin1(b)
}
