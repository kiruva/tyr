// Package metastrip removes metadata from JPEG, PNG, GIF and WebP images
// without re-encoding them, and decodes what it finds for inspection. It
// parses the container and copies image data byte for byte, so pixels never
// change. It knows nothing about files or the UI: bytes in, bytes out.
package metastrip

import (
	"fmt"

	"github.com/kiruva/tyr/internal/termsafe"
)

// Field is one decoded value: a key such as "GPSPosition" and its value. An
// empty Key marks a free-form note. In a Report both are already made safe to
// print — control characters and terminal escapes in metadata are shown
// escaped rather than passed through; the parsers that fill them in do not.
type Field struct{ Key, Value string }

// Section is one removed item and what it decodes to.
type Section struct {
	Name   string
	Size   int
	Fields []Field
}

// Report is what an image carries, ready to show.
type Report struct {
	Sections []Section
	// Markers are the provenance and AI-generation hints found in the metadata:
	// C2PA credentials and who signed them, declared source types, generator
	// traces. Watermarks hidden in the pixels are not looked for.
	Markers []string
	// Location is the decoded GPS position, "" when the image has none.
	Location string
	Total    int // bytes of metadata, summed over Sections
}

// Inspect decodes everything Strip found in an image. Pass the Result of a
// Strip of the same bytes; nothing is written anywhere.
//
// What it decodes is bounded (see limits.go): long values are cut short, and
// once the report holds about a megabyte the remaining items are listed with
// their sizes but not decoded.
func Inspect(res *Result) Report {
	var rep Report
	var decoded []itemFields
	bud, shown := newBudget(), 0
	for _, it := range res.Removed {
		sec := Section{Name: termsafe.Line(it.Name), Size: it.Size}
		rep.Total += it.Size
		if shown >= maxReportBytes {
			sec.Fields = []Field{{"", "not decoded: the report is long enough already"}}
			rep.Sections = append(rep.Sections, sec)
			continue
		}

		fields := describe(it, bud)
		if len(fields) > maxItemFields {
			fields = append(fields[:maxItemFields], Field{"", fmt.Sprintf("… %d more values not shown", len(fields)-maxItemFields)})
		}
		decoded = append(decoded, itemFields{it, fields})
		for _, f := range fields {
			if shown >= maxReportBytes {
				sec.Fields = append(sec.Fields, Field{"", "… the rest not shown: the report is long enough already"})
				break
			}
			// The parsers clip what they decode; this is the backstop, with
			// room for a clipped value's "… (N bytes)" and Latin-1 doubling.
			key, value := clipString(f.Key, maxKeyBytes+64), clipString(f.Value, 2*maxValueBytes+64)
			shown += len(key) + len(value)
			sec.Fields = append(sec.Fields, Field{termsafe.Line(key), termsafe.Line(value)})
			// Only the EXIF GPS block is a location. A PNG text chunk can be
			// called GPSPosition and say anything.
			if f.Key == "GPSPosition" && it.Kind == "exif" && rep.Location == "" {
				rep.Location = termsafe.Line(value)
			}
		}
		rep.Sections = append(rep.Sections, sec)
	}
	for _, m := range findMarkers(decoded) {
		rep.Markers = append(rep.Markers, termsafe.Line(m))
	}
	return rep
}
