package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kiruva/tyr/internal/metastrip"
	"github.com/kiruva/tyr/internal/pane"
)

// An image opened in the viewer shows what it carries rather than its bytes:
// where it was taken, what made it, who signed it, and every other piece of
// metadata decoded. The hex dump is still one key away. The report is plain
// text, so search, wrap and line numbers work on it as on any file.

// metaKeyW caps the key column, so one long XMP path does not push every value
// off to the right.
const metaKeyW = 28

// imageReport decodes an image's metadata into viewer lines. ok is false when
// data is not an image the inspector reads; note then says why, when it is an
// image of a kind it does not handle.
func imageReport(data []byte) (lines []string, note string, ok bool) {
	format := metastrip.Detect(data)
	if format == "" {
		if kind := metastrip.Unsupported(data); kind != "" {
			return nil, "metadata: " + kind + " is not supported", false
		}
		return nil, "", false
	}
	res, err := metastrip.Strip(data, metastrip.Options{})
	if err != nil {
		if errors.Is(err, metastrip.ErrUnsupported) {
			return nil, "", false
		}
		return nil, "metadata: " + err.Error(), false
	}
	return metaLines(strings.ToUpper(format), metastrip.Inspect(res)), "", true
}

// metaLines lays a report out: what matters most first — where, and whether
// it says it was made by a machine — then each item with its fields.
func metaLines(format string, rep metastrip.Report) []string {
	if len(rep.Sections) == 0 {
		return []string{format + " · no metadata"}
	}
	lines := []string{fmt.Sprintf("%s · %d %s, %s of metadata",
		format, len(rep.Sections), plural(len(rep.Sections), "item", "items"),
		pane.HumanSize(int64(rep.Total)))}

	if rep.Location != "" {
		lines = append(lines, "", "Location   "+rep.Location)
	}
	if len(rep.Markers) > 0 {
		lines = append(lines, "", "Provenance / AI markers (from metadata; pixel watermarks not checked)")
		for _, mk := range rep.Markers {
			lines = append(lines, "  ! "+mk)
		}
	}
	for _, s := range rep.Sections {
		lines = append(lines, "", fmt.Sprintf("%s (%s)", s.Name, pane.HumanSize(int64(s.Size))))
		width := 0
		for _, f := range s.Fields {
			width = max(width, min(len(f.Key), metaKeyW))
		}
		for _, f := range s.Fields {
			if f.Key == "" {
				lines = append(lines, "    "+f.Value)
				continue
			}
			lines = append(lines, fmt.Sprintf("    %-*s  %s", width, f.Key, f.Value))
		}
	}
	return lines
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
