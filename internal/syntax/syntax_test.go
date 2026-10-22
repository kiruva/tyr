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
		"main.go":         Go,
		"App.tsx":         TypeScript,
		"lib.rs":          Rust,
		"main.c":          C,
		"style.css":       CLike,
		"deploy.sh":       Shell,
		".zshrc":          Shell,
		"tool.py":         Python,
		"Gemfile":         Ruby,
		"config.yaml":     YAML,
		"Makefile":        Hash,
		"Dockerfile":      Dockerfile,
		".gitignore":      Hash,
		"package.json":    JSON,
		"README.md":       Markdown,
		"photo.jpg":       None,
		"noextension":     None,
		"/a/b/notes.toml": TOML,
	}
	for name, want := range cases {
		if got := Detect(name); got != want {
			t.Errorf("Detect(%q) = %v, want %v", name, got, want)
		}
	}
}

// Each language colours its own keywords, not every word any language in its
// shape reserves: "func" in C or "done" in Python is an identifier.
func TestPerLanguageKeywords(t *testing.T) {
	cases := []struct {
		lang       Lang
		line       string
		keyword    []string
		notKeyword []string
	}{
		{Go, "func main() { defer x() }", []string{"func", "defer"}, []string{"main"}},
		{C, "int func = sizeof(x);", []string{"int", "sizeof"}, []string{"func"}},
		{Rust, "pub fn go() -> impl Trait", []string{"pub", "fn", "impl"}, []string{"go"}},
		{JavaScript, "const done = await fetch(url)", []string{"const", "await"}, []string{"done"}},
		{Python, "def done(self): return None", []string{"def", "return", "None"}, []string{"done"}},
		{Shell, "for f in *; do echo $f; done", []string{"for", "in", "do", "done"}, []string{"f"}},
		{Ruby, "def go; end", []string{"def", "end"}, []string{"go"}},
		{YAML, "enabled: true", []string{"true"}, []string{"enabled"}},
	}
	for _, c := range cases {
		spans := New(c.lang).Line(c.line)
		for _, w := range c.keyword {
			if got := wordKindIn(spans, w); got != Keyword {
				t.Errorf("%v: %q scanned as %v, want Keyword", c.lang, w, got)
			}
		}
		for _, w := range c.notKeyword {
			if got := wordKindIn(spans, w); got == Keyword {
				t.Errorf("%v: %q scanned as a keyword", c.lang, w)
			}
		}
	}
}

// wordKindIn finds the kind of the span that is exactly word.
func wordKindIn(spans []Span, word string) Kind {
	for _, s := range spans {
		if strings.TrimSpace(s.Text) == word {
			return s.Kind
		}
	}
	return Plain
}

func TestLangNames(t *testing.T) {
	for lang, want := range map[Lang]string{Go: "go", Python: "python", CLike: "code", Hash: "script", None: "plain"} {
		if got := lang.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", lang, got, want)
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
// file readable at a glance. Keys are their own kind: as keywords they were
// coloured the same as true/false/null.
func TestJSONKeysAndValues(t *testing.T) {
	h := New(JSON)
	spans := h.Line(`  "name": "tyr", "count": 3, "ok": true`)

	wants(t, spans, map[string]Kind{
		`"name"`: Key,
		`"ok"`:   Key,
		`"tyr"`:  Str,
		"3":      Number,
		"true":   Keyword,
	})
}

// A Python triple-quoted string runs across lines, and a # inside it is not a
// comment.
func TestPythonTripleQuotes(t *testing.T) {
	h := New(Python)
	if got, _ := kindOf(h.Line(`    """Docstring opens`), "Docstring"); got != Str {
		t.Fatalf("the opening line scanned as %v", got)
	}
	if got, _ := kindOf(h.Line("    # not a comment"), "not a comment"); got != Str {
		t.Fatalf("inside the string, a # line scanned as %v", got)
	}
	spans := h.Line(`    closes""" + 1  # real`)
	wants(t, spans, map[string]Kind{`closes"""`: Str, "1": Number, "# real": Comment})

	spans = h.Line(`x = '''one line''' if True else 2`)
	wants(t, spans, map[string]Kind{`'''one line'''`: Str, "if": Keyword, "2": Number})
	if got := h.Line("after"); got[0].Kind != Plain {
		t.Errorf("after a closed one-line triple string, text scanned as %v", got[0].Kind)
	}
}

// A decorator at the start of a statement is marked; @ elsewhere is an operator.
func TestPythonDecorator(t *testing.T) {
	wants(t, New(Python).Line("    @functools.cache"), map[string]Kind{"@functools.cache": Name})
	if got, _ := kindOf(New(Python).Line("c = a @ b"), "@"); got == Name {
		t.Error("matrix-multiply @ scanned as a decorator")
	}
}

// Shell variables are marked in and out of double quotes, never in single.
func TestShellVariables(t *testing.T) {
	spans := New(Shell).Line(`echo "home is $HOME and ${USER:-me}" $1 '$NOT' # $neither`)
	wants(t, spans, map[string]Kind{
		`"home is `:   Str,
		"$HOME":       Name,
		" and ":       Str,
		"${USER:-me}": Name,
		"$1":          Name,
		`'$NOT'`:      Str,
		"# $neither":  Comment,
	})
	// ${#list} is a length, not a comment.
	wants(t, New(Shell).Line(`n=${#list}`), map[string]Kind{"${#list}": Name})
	// A backslash keeps the dollar literal.
	if got, _ := kindOf(New(Shell).Line(`echo "\$5"`), "$5"); got != Str {
		t.Errorf(`an escaped \$5 scanned as %v`, got)
	}
}

// YAML and TOML keys are keys.
func TestConfigKeys(t *testing.T) {
	wants(t, New(YAML).Line("  name: tyr  # the binary"), map[string]Kind{
		"name": Key, "# the binary": Comment,
	})
	wants(t, New(YAML).Line(`- "quoted key": true`), map[string]Kind{`"quoted key"`: Key, "true": Keyword})
	wants(t, New(YAML).Line("url: http://x"), map[string]Kind{"url": Key})
	spans := New(YAML).Line("plain: text: more")
	wants(t, spans, map[string]Kind{"plain": Key})
	if got, _ := kindOf(spans, "text"); got == Key {
		t.Error("a YAML value with a colon in it scanned as a key")
	}
	for _, line := range []string{"- just an item", "http://x", "# key: in a comment"} {
		for _, s := range New(YAML).Line(line) {
			if s.Kind == Key {
				t.Errorf("YAML %q: %q scanned as a key", line, s.Text)
			}
		}
	}

	wants(t, New(TOML).Line(`server.port = 8080`), map[string]Kind{"server.port": Key, "8080": Number})
	wants(t, New(TOML).Line(`"odd key" = true`), map[string]Kind{`"odd key"`: Key, "true": Keyword})
	for _, line := range []string{"[server]", `list = ["a = b"]`} {
		for _, s := range New(TOML).Line(line) {
			if s.Kind == Key && s.Text != "list" {
				t.Errorf("TOML %q: %q scanned as a key", line, s.Text)
			}
		}
	}
}

// Markup: tags and their attributes, strings, entities, and comments and tags
// that run across lines.
func TestXML(t *testing.T) {
	h := New(XML)
	wants(t, h.Line(`<item id="7" lang='no'>fish &amp; chips</item>`), map[string]Kind{
		"<item":  Keyword,
		"id":     Key,
		`"7"`:    Str,
		`'no'`:   Str,
		"&amp;":  Number,
		"</item": Keyword,
		"fish":   Plain,
	})
	h.Line("<!-- a comment")
	if got, _ := kindOf(h.Line(`  <still id="x">`), "still"); got != Comment {
		t.Fatalf("a tag inside a comment scanned as %v", got)
	}
	wants(t, h.Line("ends --> <a"), map[string]Kind{"ends -->": Comment, "<a": Keyword})
	// The tag left open carries its attributes onto the next line.
	wants(t, h.Line(`   href="x" />text`), map[string]Kind{"href": Key, `"x"`: Str, "/>": Keyword, "text": Plain})

	if Detect("index.html") != HTML || Detect("icon.svg") != XML {
		t.Errorf("Detect: html = %v, svg = %v", Detect("index.html"), Detect("icon.svg"))
	}
	if New(HTML).Line("<p>")[0].Kind != Keyword {
		t.Error("HTML is not scanned as markup")
	}
}

// SQL keywords are found in any case; -- and /* */ are comments.
func TestSQL(t *testing.T) {
	h := New(SQL)
	wants(t, h.Line(`select name, 'it''s' FROM users where id = 42 -- one`), map[string]Kind{
		"select":  Keyword,
		"FROM":    Keyword,
		"where":   Keyword,
		"name":    Plain,
		`'it''s'`: Str,
		"42":      Number,
		"-- one":  Comment,
	})
	h.Line("/* opens")
	if got := h.Line("SELECT inside"); got[0].Kind != Comment {
		t.Errorf("inside a block comment, a line scanned as %v", got[0].Kind)
	}
	wants(t, h.Line("*/ Insert"), map[string]Kind{"*/": Comment, "Insert": Keyword})
	if got, _ := kindOf(h.Line("x - -1"), "-1"); got == Comment {
		t.Error("a minus sign scanned as a comment")
	}
}

// A diff is read line by line from its first character.
func TestDiff(t *testing.T) {
	h := New(Diff)
	for line, want := range map[string]Kind{
		"diff --git a/x b/x":  Heading,
		"--- a/x":             Heading,
		"+++ b/x":             Heading,
		"@@ -1,3 +1,4 @@ fn":  Keyword,
		"+added":              Added,
		"-removed":            Removed,
		" context":            Plain,
		`\ No newline at end`: Comment,
	} {
		if got := h.Line(line); len(got) != 1 || got[0].Kind != want {
			t.Errorf("%q scanned as %+v, want one %v span", line, got, want)
		}
	}
	if Detect("fix.patch") != Diff || Detect("a.diff") != Diff {
		t.Error("Detect does not know .patch / .diff")
	}
}

// Dockerfile instructions are keywords; the rest is shell-ish.
func TestDockerfile(t *testing.T) {
	for _, name := range []string{"Dockerfile", "Dockerfile.dev", "app.dockerfile", "Containerfile"} {
		if got := Detect(name); got != Dockerfile {
			t.Errorf("Detect(%q) = %v, want dockerfile", name, got)
		}
	}
	wants(t, New(Dockerfile).Line(`FROM golang:1.23 AS build  # base`), map[string]Kind{
		"FROM": Keyword, "AS": Keyword, "# base": Comment,
	})
	wants(t, New(Dockerfile).Line(`RUN echo "$HOME" ${GOPATH}`), map[string]Kind{
		"RUN": Keyword, "$HOME": Name, "${GOPATH}": Name,
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
	for _, lang := range allLangs() {
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
