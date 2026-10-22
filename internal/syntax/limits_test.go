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
	for _, lang := range allLangs() {
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
//
// The other pieces are the starts of things a scanner looks ahead for — an
// entity's ;, a variable's }, a key's colon — repeated with no end in sight.
func TestHighlightLongLineIsLinear(t *testing.T) {
	pieces := []string{"=;é", "&", "&a", "<", "<a ", "${", "$", `"$x`, "@a", "- ", "a:", "a ", "'''", "/*", "<!--"}
	for _, piece := range pieces {
		line := strings.Repeat(piece, 1_000_000/len(piece))
		for _, lang := range allLangs() {
			start := time.Now()
			spans := New(lang).Line(line)
			if took := time.Since(start); took > 2*time.Second {
				t.Errorf("lang %v: %v for a %d KB line of %q", lang, took, len(line)>>10, piece)
			}
			if got := joined(spans); got != line {
				t.Errorf("lang %v: a line of %q came back changed", lang, piece)
			}
		}
	}
}

// allLangs is every language the highlighter knows, shapes included.
func allLangs() []Lang {
	out := []Lang{CLike, Hash, JSON, Markdown, XML, SQL, Diff}
	for lang := range languages {
		out = append(out, lang)
	}
	return out
}
