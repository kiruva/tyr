package jsonfmt

import (
	"errors"
	"strings"
	"testing"
)

func TestPrettify(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"object", `{"b":1,"a":[1,2,{}],"c":{}}`, "{\n  \"b\": 1,\n  \"a\": [\n    1,\n    2,\n    {}\n  ],\n  \"c\": {}\n}"},
		// Numbers and escapes are kept as written, never re-encoded.
		{"literals", `{"n":1.50e+10,"big":12345678901234567890,"s":"\u00e9\t\"q\""}`,
			"{\n  \"n\": 1.50e+10,\n  \"big\": 12345678901234567890,\n  \"s\": \"\\u00e9\\t\\\"q\\\"\"\n}"},
		{"scalar", ` 42 `, "42"},
		{"keeps the final newline", "[1]\n", "[\n  1\n]\n"},
		{"tabs when the file uses tabs", "{\n\t\"a\": [1]\n}", "{\n\t\"a\": [\n\t\t1\n\t]\n}"},
		{"four spaces when the file uses four", "{\n    \"a\": 1, \"b\": 2}", "{\n    \"a\": 1,\n    \"b\": 2\n}"},
		{"byte order mark kept", "\ufeff{\"a\":1}", "\ufeff{\n  \"a\": 1\n}"},
		{"already pretty", "{\n  \"a\": 1\n}\n", "{\n  \"a\": 1\n}\n"},
	}
	for _, c := range cases {
		got, err := Prettify(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s:\ngot  %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestMinify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"{\n  \"a\": [1, 2],\n  \"b\": \"x y\"\n}\n", `{"a":[1,2],"b":"x y"}` + "\n"},
		{"\ufeff [ 1 ]", "\ufeff[1]"},
		{`"  spaces inside a string stay  "`, `"  spaces inside a string stay  "`},
	}
	for _, c := range cases {
		got, err := Minify(c.in)
		if err != nil || got != c.want {
			t.Errorf("Minify(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestStripComments(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"line comment", "{\n  \"a\": 1 // one\n}", "{\n  \"a\": 1 \n}"},
		{"block comment keeps its lines", "{\n  /* a\n     b */ \"a\": 1\n}", "{\n  \n \"a\": 1\n}"},
		{"block comment between tokens is a space", "[1/**/,2]", "[1 ,2]"},
		{"comment markers inside strings stay", `{"url": "http://x/*y*/", "c": "//"}`, `{"url": "http://x/*y*/", "c": "//"}`},
		{"escaped quote does not end the string", `{"a": "\"//\""} // c`, `{"a": "\"//\""} `},
		{"trailing commas", "{\n  \"a\": [1, 2,],\n  \"b\": 3,\n}", "{\n  \"a\": [1, 2],\n  \"b\": 3\n}"},
		{"trailing comma before a comment", "[1, // last\n]", "[1 \n]"},
		{"plain JSON is untouched", `{"a":[1,2]}`, `{"a":[1,2]}`},
	}
	for _, c := range cases {
		got, err := StripComments(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s:\ngot  %q\nwant %q", c.name, got, c.want)
		}
		if strings.Count(got, "\n") != strings.Count(c.in, "\n") {
			t.Errorf("%s: line count changed", c.name)
		}
	}
}

func TestErrorsSayWhere(t *testing.T) {
	cases := []struct {
		name      string
		do        func(string) (string, error)
		in        string
		line, col int
	}{
		{"missing comma", Prettify, "{\n  \"a\": 1\n  \"b\": 2\n}", 3, 3},
		{"comment in plain JSON", Minify, "[1, // x\n2]", 1, 5},
		{"unterminated comment", StripComments, "[1 /* x", 1, 4},
		{"still not JSON", StripComments, "{\"a\" 1}", 1, 6},
		{"after a wide character", Prettify, "[\"é\" x]", 1, 6},
		{"empty", Prettify, "", 1, 1},
	}
	for _, c := range cases {
		_, err := c.do(c.in)
		var se *SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%s: err = %v, want a *SyntaxError", c.name, err)
			continue
		}
		if se.Line != c.line || se.Col != c.col {
			t.Errorf("%s: at %d:%d, want %d:%d (%v)", c.name, se.Line, se.Col, c.line, c.col, se)
		}
	}
}

// Prettify, then minify, is the minified original.
func TestRoundTrip(t *testing.T) {
	in := `{"a":[1,{"b":null,"c":true}],"d":"x\ny","e":-0.5E-3}`
	pretty, err := Prettify(in)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Minify(pretty)
	if err != nil || back != in {
		t.Errorf("round trip = %q, %v", back, err)
	}
}

func TestErrorsSayWhatWasExpected(t *testing.T) {
	cases := []struct{ in, want string }{
		{"{\"a\": 1 \"b\": 2}", "comma missing"},
		{"[1 2]", "comma missing"},
		{"{\"a\" 1}", "a : after the key"},
		{"{a: 1}", "a key in double quotes"},
		{"[1,", "ends too early"},
		{"[1] 2", "nothing more"},
	}
	for _, c := range cases {
		_, err := Prettify(c.in)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want it to say %q", c.in, err, c.want)
		}
	}
}

func FuzzStripComments(f *testing.F) {
	for _, s := range []string{"{\"a\": [1, 2,], // c\n /* b\n */ \"b\": \"//\"}", "[1/**/,2]", `"\"//"`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, err := StripComments(in)
		if err != nil {
			return
		}
		if strings.Count(out, "\n") != strings.Count(in, "\n") {
			t.Fatalf("line count changed: %q → %q", in, out)
		}
		if _, err := Minify(out); err != nil {
			t.Fatalf("stripped text is not JSON: %q → %q: %v", in, out, err)
		}
	})
}

func FuzzPrettify(f *testing.F) {
	for _, s := range []string{`{"a":[1,{"b":null}],"c":"x\ny"}`, "[\n\t1\n]\n", "\uFEFF{}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		pretty, err := Prettify(in)
		if err != nil {
			return
		}
		a, err1 := Minify(in)
		b, err2 := Minify(pretty)
		if err1 != nil || err2 != nil || strings.TrimSuffix(a, "\n") != strings.TrimSuffix(b, "\n") {
			t.Fatalf("prettify changed the JSON: %q → %q", in, pretty)
		}
	})
}
