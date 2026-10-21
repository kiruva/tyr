package app

import (
	"errors"
	"fmt"

	"github.com/kiruva/tyr/internal/jsonfmt"
)

// Ctrl+T in the editor opens a one-line menu of JSON tools in the footer. Each
// works on the selection, or the whole buffer when nothing is selected, and is
// one undo step; nothing is written until the file is saved. Text that is not
// JSON is never touched: the footer says where it stops being JSON, and the
// cursor goes there.

// editJSONTools are the menu's choices.
var editJSONTools = []struct {
	key, label    string
	done, already string // what the footer says after, and when there was nothing to do
	do            func(string) (string, error)
}{
	{"p", "prettify", "prettified", "already pretty", jsonfmt.Prettify},
	{"m", "minify", "minified", "already minified", jsonfmt.Minify},
	{"c", "strip comments", "stripped the comments from", "no comments to strip", jsonfmt.StripComments},
}

// editJSONHint is the footer while the menu is open.
func editJSONHint() string {
	s := " JSON:"
	for _, t := range editJSONTools {
		s += " " + t.key + " " + t.label + " ·"
	}
	return s + " esc"
}

// onEditJSONKey answers the menu: a tool runs, esc closes, anything else is
// ignored so a slip does not land in the text.
func (m Model) onEditJSONKey(key string) Model {
	if key == "esc" {
		m.editJSON = false
		return m
	}
	for _, t := range editJSONTools {
		if key == t.key {
			m.editJSON = false
			m.runEditJSON(t.label, t.done, t.already, t.do)
			return m
		}
	}
	return m
}

func (m *Model) runEditJSON(label, done, already string, do func(string) (string, error)) {
	text, whole := m.editor.Target()
	out, err := do(text)
	var se *jsonfmt.SyntaxError
	switch {
	case errors.As(err, &se):
		m.editor.GoToTarget(se.Line, se.Col)
		m.editStatus = "not JSON: " + se.Error()
		if _, stripErr := jsonfmt.StripComments(text); label != "strip comments" && stripErr == nil {
			// It is JSONC: say the way out rather than where the first // is.
			m.editStatus = "it has comments or trailing commas — ^T c strips them first"
		}
		return
	case err != nil:
		m.editStatus = label + " failed: " + err.Error()
		return
	case out == text:
		m.editStatus = already
		return
	}
	m.editor.ReplaceTarget(out)
	what := "the file"
	if !whole {
		what = "the selection"
	}
	m.editStatus = fmt.Sprintf("%s %s · ^Z undoes it", done, what)
}
