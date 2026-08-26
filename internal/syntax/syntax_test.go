package syntax

import (
	"strings"
	"testing"
)

// kindOf finds the kind a piece of text was scanned as.
func kindOf(spans []Span, text string) (Kind, bool) {
	for _, s := range spans {
		if strings.Contains(s.Text, text) {
			return s.Kind, true
		}
	}
	return Plain, false
}

// wants asserts that each snippet came out as the kind it should have.
func wants(t *testing.T, spans []Span, cases map[string]Kind) {
	t.Helper()
	for text, want := range cases {
		got, found := kindOf(spans, text)
		if !found {
			t.Errorf("%q is not in any span", text)
			continue
		}
		if got != want {
			t.Errorf("%q scanned as %v, want %v", text, got, want)
		}
	}
}

func TestDetect(t *testing.T) {
	cases := map[string]Lang{
		"main.go":         CLike,
		"App.tsx":         CLike,
		"deploy.sh":       Hash,
		"config.yaml":     Hash,
		"Makefile":        Hash,
		"Dockerfile":      Hash,
		".gitignore":      Hash,
		"package.json":    JSON,
		"README.md":       Markdown,
		"photo.jpg":       None,
		"noextension":     None,
		"/a/b/notes.toml": Hash,
	}
	for name, want := range cases {
		if got := Detect(name); got != want {
			t.Errorf("Detect(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestCLikeLine(t *testing.T) {
	h := New(CLike)
	spans := h.Line(`	count := 42 // how many "things"`)

	wants(t, spans, map[string]Kind{
		"42":          Number,
		`// how many`: Comment,
	})
	// The quotes inside the comment stay part of the comment.
	if got, _ := kindOf(spans, `"things"`); got != Comment {
		t.Errorf(`"things" inside a comment scanned as %v`, got)
	}
}

func TestCLikeKeywordsAndStrings(t *testing.T) {
	h := New(CLike)
	spans := h.Line(`	if name == "kim" { return true }`)

	wants(t, spans, map[string]Kind{
		"if":     Keyword,
		`"kim"`:  Str,
		"return": Keyword,
		"true":   Keyword,
	})
}

// A block comment carries on across lines until it is closed.
func TestCLikeBlockComment(t *testing.T) {
	h := New(CLike)

	if got, _ := kindOf(h.Line("code /* opens here"), "opens here"); got != Comment {
		t.Fatalf("the opening line scanned as %v", got)
	}
	if got, _ := kindOf(h.Line("still inside"), "still inside"); got != Comment {
		t.Fatalf("the second line scanned as %v, want it still in the comment", got)
	}
	spans := h.Line("closes */ and 7 follows")
	if got, _ := kindOf(spans, "closes */"); got != Comment {
		t.Errorf("the closing run scanned as %v", got)
	}
	if got, _ := kindOf(spans, "7"); got != Number {
		t.Errorf("after the comment closed, 7 scanned as %v, want Number", got)
	}
}

// A raw string spans lines the same way.
func TestCLikeRawString(t *testing.T) {
	h := New(CLike)
	if got, _ := kindOf(h.Line("query := `SELECT"), "SELECT"); got != Str {
		t.Fatalf("the opening line scanned as %v", got)
	}
	if got, _ := kindOf(h.Line("FROM files`"), "FROM files"); got != Str {
		t.Errorf("the closing line scanned as %v", got)
	}
	if got, _ := kindOf(h.Line("after 1"), "1"); got != Number {
		t.Errorf("after the raw string, 1 scanned as %v", got)
	}
}

// A digit inside an identifier is part of the identifier.
func TestNumbersInsideWords(t *testing.T) {
	h := New(CLike)
	spans := h.Line("utf8.RuneLen(r)")
	for _, s := range spans {
		if s.Kind == Number {
			t.Errorf("%q in an identifier scanned as a number", s.Text)
		}
	}
}

func TestHashLine(t *testing.T) {
	h := New(Hash)
	spans := h.Line(`export NAME="tyr"  # the binary`)

	wants(t, spans, map[string]Kind{
		"export":       Keyword,
		`"tyr"`:        Str,
		"# the binary": Comment,
	})
}

// In JSON a string with a colon after it is a key, which is what makes a config
// file readable at a glance.
func TestJSONKeysAndValues(t *testing.T) {
	h := New(JSON)
	spans := h.Line(`  "name": "tyr", "count": 3, "ok": true`)

	wants(t, spans, map[string]Kind{
		`"name"`: Keyword,
		`"tyr"`:  Str,
		"3":      Number,
		"true":   Keyword,
	})
}

func TestMarkdown(t *testing.T) {
	h := New(Markdown)

	if got := h.Line("## Install"); got[0].Kind != Heading {
		t.Errorf("a heading scanned as %v", got[0].Kind)
	}
	if got, _ := kindOf(h.Line("- a bullet"), "-"); got != Keyword {
		t.Errorf("a bullet scanned as %v", got)
	}
	if got, _ := kindOf(h.Line("use `tyr --help` for more"), "`tyr --help`"); got != Str {
		t.Errorf("a code span scanned as %v", got)
	}

	// A fence puts everything until the next one in the code style.
	h.Line("```sh")
	if got := h.Line("tyr --themes"); got[0].Kind != Str {
		t.Errorf("a line inside a fence scanned as %v", got[0].Kind)
	}
	h.Line("```")
	if got := h.Line("back to prose"); got[0].Kind != Plain {
		t.Errorf("after the fence closed, prose scanned as %v", got[0].Kind)
	}
}

// An unknown language is handed back whole and unstyled.
func TestNoneIsUntouched(t *testing.T) {
	h := New(None)
	spans := h.Line(`anything /* at */ all "here" 42`)
	if len(spans) != 1 || spans[0].Kind != Plain {
		t.Fatalf("spans = %+v, want one plain span", spans)
	}
}

// Whatever the scanner does, it must not lose or invent characters.
func TestSpansReconstructTheLine(t *testing.T) {
	lines := []string{
		`func main() { fmt.Println("hi", 42) } // go`,
		`x = 'unterminated`,
		`  # comment only`,
		`"key": [1, 2, 3]`,
		"| table | row |",
		"",
	}
	for _, lang := range []Lang{CLike, Hash, JSON, Markdown} {
		h := New(lang)
		for _, line := range lines {
			var b strings.Builder
			for _, s := range h.Line(line) {
				b.WriteString(s.Text)
			}
			if b.String() != line {
				t.Errorf("%v turned %q into %q", lang, line, b.String())
			}
		}
	}
}
