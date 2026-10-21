// Package jsonfmt reformats JSON text for the editor and the viewer:
// prettified, minified, or with JSONC's comments and trailing commas taken
// out. Text in, text out, and nothing in between is decoded: key order,
// number literals and string escapes come out exactly as they went in, which
// is what makes it safe to run on a config file — 1.50e+10 stays 1.50e+10,
// and a 20-digit ID is not rounded into a float.
//
// Each function fails without changing anything when the input is not what it
// expects, and the error says where, as a line and column a person can go to.
package jsonfmt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// SyntaxError is where the text stopped being JSON: Line and Col count from 1,
// Col in characters, pointing at the one that does not fit.
type SyntaxError struct {
	Line, Col int
	Msg       string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("line %d, col %d: %s", e.Line, e.Col, e.Msg)
}

const bom = "\uFEFF"

// Prettify lays JSON out one value to a line, indented the way the text
// already is — with tabs, or with as many spaces as its first indented line —
// or two spaces when it is not indented at all. A byte order mark and a final
// newline are kept.
func Prettify(text string) (string, error) {
	body, mark, newline := split(text)
	compact, err := compactOf(body)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", indentOf(body)); err != nil {
		return "", locate(body, err) // cannot happen: compact is valid
	}
	return mark + out.String() + newline, nil
}

// Minify puts JSON on one line with no space outside its strings. A byte
// order mark and a final newline are kept.
func Minify(text string) (string, error) {
	body, mark, newline := split(text)
	compact, err := compactOf(body)
	if err != nil {
		return "", err
	}
	return mark + string(compact) + newline, nil
}

// StripComments turns JSONC — JSON with // and /* */ comments and trailing
// commas, as VS Code and tsconfig write it — into plain JSON. Nothing else
// moves: a comment leaves the line breaks it spanned, so every line keeps its
// number, and a block comment between two tokens becomes a space so they do
// not run together. The result has to be JSON, or nothing is returned.
func StripComments(text string) (string, error) {
	out := make([]byte, 0, len(text))
	// lastComma is where in out the last comma outside a string went, and
	// whether only space and comments have followed it: a ] or } then makes
	// it a trailing comma, which is taken out.
	lastComma, open := -1, false
	inString, escaped := false, false

	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case inString:
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			i++
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				end = len(text) - i
			}
			i += end // the newline itself is kept
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return "", position(text, i, "comment is not closed")
			}
			comment := text[i : i+2+end+2]
			if n := strings.Count(comment, "\n"); n > 0 {
				out = append(out, strings.Repeat("\n", n)...)
			} else {
				out = append(out, ' ')
			}
			i += len(comment)
		default:
			switch c {
			case '"':
				inString, open = true, false
			case ',':
				lastComma, open = len(out), true
			case ']', '}':
				if open {
					// Only space follows the comma, so this moves little.
					out = append(out[:lastComma], out[lastComma+1:]...)
				}
				open = false
			case ' ', '\t', '\r', '\n':
			default:
				open = false
			}
			out = append(out, c)
			i++
		}
	}

	result := string(out)
	body, _, _ := split(result)
	if _, err := compactOf(body); err != nil {
		var se *SyntaxError
		if errors.As(err, &se) {
			// Line numbers are the same in both; columns after a stripped
			// comment are close enough to find the spot.
			return "", se
		}
		return "", err
	}
	return result, nil
}

// split takes a byte order mark off the front and a final newline off the
// end, to put back on the result.
func split(text string) (body, mark, newline string) {
	body, hasMark := strings.CutPrefix(text, bom)
	if hasMark {
		mark = bom
	}
	trimmed := strings.TrimRight(body, " \t\r\n")
	if rest := body[len(trimmed):]; strings.Contains(rest, "\n") {
		newline = "\n"
	}
	return body, mark, newline
}

// compactOf is body with no space outside its strings, or where it is not
// JSON.
func compactOf(body string) ([]byte, error) {
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(body)); err != nil {
		return nil, locate(body, err)
	}
	return out.Bytes(), nil
}

// indentOf is the indent the text already uses, or two spaces.
func indentOf(body string) string {
	for _, line := range strings.Split(body, "\n")[1:] {
		switch {
		case strings.HasPrefix(line, "\t"):
			return "\t"
		case strings.HasPrefix(line, " "):
			n := len(line) - len(strings.TrimLeft(line, " "))
			if strings.TrimSpace(line) != "" && n <= 8 {
				return strings.Repeat(" ", n)
			}
		}
	}
	return "  "
}

// locate turns encoding/json's byte offset into a line and column, and its
// message into one that says what was expected there.
func locate(body string, err error) error {
	var se *json.SyntaxError
	if !errors.As(err, &se) {
		return err
	}
	msg := se.Error()
	if strings.Contains(msg, "unexpected end") {
		return position(body, len(strings.TrimRight(body, " \t\r\n")), "it ends too early — something is not closed")
	}
	// Offset counts the bytes read, the offending one included.
	return position(body, int(se.Offset)-1, explain(msg))
}

// explain rewords encoding/json's "invalid character 'x' after object key"
// into what a person fixing the file needs: what was found, and what belongs
// there instead.
func explain(msg string) string {
	rest, ok := strings.CutPrefix(msg, "invalid character ")
	if !ok {
		return msg
	}
	found, reason := rest, ""
	if end := strings.Index(rest[1:], "' "); strings.HasPrefix(rest, "'") && end >= 0 {
		found, reason = rest[:end+2], rest[end+3:]
	}
	expected := map[string]string{
		"after object key":                           "a : after the key",
		"after object key:value pair":                "a , or } — a comma missing?",
		"after array element":                        "a , or ] — a comma missing?",
		"after array value":                          "a , or ] — a comma missing?",
		"looking for beginning of value":             "a value",
		"looking for beginning of object key string": "a key in double quotes",
		"after top-level value":                      "nothing more after the value",
		"in string literal":                          "no control characters inside a string",
	}[reason]
	if expected == "" {
		return "unexpected " + rest
	}
	return "found " + found + ", expected " + expected
}

// position is the line and column of byte i in text.
func position(text string, i int, msg string) *SyntaxError {
	i = min(max(i, 0), len(text))
	before := text[:i]
	line := strings.Count(before, "\n") + 1
	col := utf8.RuneCountInString(before[strings.LastIndexByte(before, '\n')+1:]) + 1
	return &SyntaxError{Line: line, Col: col, Msg: msg}
}
