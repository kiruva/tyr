package syntax

import (
	"strings"
	"testing"
	"time"
)

func joined(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// Highlighting never changes the text: a byte at a time, each byte of an é
// was turned into a character of its own ("cafÃ©").
func TestHighlightKeepsUTF8(t *testing.T) {
	for _, lang := range []Lang{CLike, Hash, JSON, Markdown} {
		for _, line := range []string{"café ünïcode", `[“x”] → ok`, "- naïve * list"} {
			if got := joined(New(lang).Line(line)); got != line {
				t.Errorf("lang %v: %q came back as %q", lang, line, got)
			}
		}
	}
}

// A long line costs time in proportion to its length. Every punctuation byte
// used to be appended to the span before it, copying the span each time: a
// megabyte-long minified line took half a minute per redraw.
func TestHighlightLongLineIsLinear(t *testing.T) {
	for _, lang := range []Lang{CLike, Hash, JSON, Markdown} {
		line := strings.Repeat("=;é", 400_000)
		start := time.Now()
		spans := New(lang).Line(line)
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("lang %v: %v for a %d KB line", lang, took, len(line)>>10)
		}
		if got := joined(spans); got != line {
			t.Errorf("lang %v: the line came back changed", lang)
		}
	}
}
