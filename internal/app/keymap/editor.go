package keymap

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/config"
)

// The key editor is the other half of making the keymap data: the config file
// can be edited by hand, and this edits the same thing by pressing the key you
// want. Every change is written straight to the config — there is no "save"
// step to forget, and the binding is live in the same keystroke.
//
// A key already spoken for is refused rather than stolen. Two actions on one
// key is not a preference, it is a bug you gave yourself.

// stage is what the editor is doing.
type stage int

const (
	stageList    stage = iota // browsing the actions
	stageCapture              // waiting for the key to bind
	stageAdd                  // waiting for a key to add to the action
)

// Outcome is what the root model should do about the editor now.
type Outcome int

const (
	// Stay means the editor is still open.
	Stay Outcome = iota
	// Close means it is finished and the root should go back to normal.
	Close
)

// Result is what one keypress produced besides the new Editor and Map.
type Result struct {
	Outcome Outcome
	Cmd     tea.Cmd
}

// Editor is the key editor while it is open. It holds only what the editor
// needs; the bindings themselves belong to the root model, which passes them in
// and takes back whatever the editor made of them.
type Editor struct {
	stage  stage
	cursor int
	status string
	fault  bool // the status is a refusal rather than a note
}

// pageRows is how many actions PgUp/PgDn move by; the drawn window is sized
// from the terminal and this is only the paging step.
const pageRows = 14

// NewEditor opens the editor at the top of the list.
func NewEditor() Editor { return Editor{} }

// Cursor is the highlighted row, for the mouse handler in the root model.
func (e Editor) Cursor() int { return e.cursor }

// Capturing reports whether the editor is waiting for a key to be pressed,
// rather than browsing the list.
func (e Editor) Capturing() bool { return e.stage != stageList }

// Status is what the editor has to say about the last change, and whether that
// was a refusal rather than a note. The root model has no use for it — the
// editor draws its own footer — but it is how a caller can tell a rejected
// rebind from an accepted one without reading it off the screen.
func (e Editor) Status() (text string, refused bool) { return e.status, e.fault }

// MoveTo puts the cursor on a row, clamped to the list.
func (e Editor) MoveTo(index int) Editor {
	e.cursor = min(max(index, 0), len(specs)-1)
	return e
}

// Actions is how many rows the editor lists, for clamping a scroll.
func Actions() int { return len(specs) }

// ActionIndex is the row a named action sits on, for putting the cursor there
// without knowing how the table is ordered.
func ActionIndex(id string) (int, bool) {
	for i, spec := range specs {
		if spec.id == id {
			return i, true
		}
	}
	return 0, false
}

// Update drives the editor: browsing, or capturing the next keypress. It takes
// the bindings as they stand and returns them as they now stand, which is how a
// rebind reaches the rest of the app without this package knowing about it.
func (e Editor) Update(msg tea.KeyMsg, keys Map) (Editor, Map, Result) {
	if e.stage != stageList {
		return e.capture(msg, keys)
	}

	last := len(specs) - 1
	switch msg.String() {
	case "esc", "q":
		return NewEditor(), keys, Result{Outcome: Close}
	case "ctrl+c":
		return e, keys, Result{Cmd: tea.Quit}
	case "up", "k":
		e.cursor = max(e.cursor-1, 0)
	case "down", "j":
		e.cursor = min(e.cursor+1, last)
	case "pgup":
		e.cursor = max(e.cursor-pageRows, 0)
	case "pgdown":
		e.cursor = min(e.cursor+pageRows, last)
	case "home", "g":
		e.cursor = 0
	case "end", "G":
		e.cursor = last
	case "enter":
		e.stage = stageCapture
		e.status, e.fault = "", false
	case "a":
		e.stage = stageAdd
		e.status, e.fault = "", false
	case "d":
		return e.resetOne(keys)
	case "D":
		return e.resetAll()
	}
	return e, keys, Result{}
}

// capture takes the next keypress as the binding, unless it is one of the keys
// the app cannot give away.
func (e Editor) capture(msg tea.KeyMsg, keys Map) (Editor, Map, Result) {
	pressed := msg.String()
	spec := specs[e.cursor]

	if pressed == "esc" {
		e.stage = stageList
		e.status, e.fault = "", false
		return e, keys, Result{}
	}
	if reservedKeys[pressed] {
		return e.refuse(keys, fmt.Sprintf("%s is reserved — it is how you get out of things", prettyKey(pressed)))
	}
	if other, taken := keys.boundTo(pressed, spec.id); taken {
		return e.refuse(keys, fmt.Sprintf("%s is already %s", prettyKey(pressed), other.desc))
	}

	bound := []string{pressed}
	if e.stage == stageAdd {
		bound = append(keys.boundKeys(spec), pressed)
	}
	return e.commit(keys, spec, bound, fmt.Sprintf("%s → %s", spec.desc, keyList(bound)))
}

// resetOne puts one action back to what it ships with.
func (e Editor) resetOne(keys Map) (Editor, Map, Result) {
	spec := specs[e.cursor]
	if sameKeys(keys.boundKeys(spec), spec.def) {
		e.status, e.fault = spec.desc+" is already the default", false
		return e, keys, Result{}
	}

	// A default can collide with something the user has since bound elsewhere.
	for _, k := range spec.def {
		if other, taken := keys.boundTo(k, spec.id); taken {
			return e.refuse(keys, fmt.Sprintf("cannot reset: %s is now %s", prettyKey(k), other.desc))
		}
	}
	return e.commit(keys, spec, spec.def, spec.desc+" back to "+keyList(spec.def))
}

// resetAll throws away every rebinding at once, so it builds the map from the
// defaults rather than taking the current one in.
func (e Editor) resetAll() (Editor, Map, Result) {
	keys := Default()
	if err := config.SaveKeys(nil); err != nil {
		return e.refuse(keys, "could not write the config: "+err.Error())
	}
	e.status, e.fault = "every key back to its default", false
	return e, keys, Result{}
}

// commit applies a change and writes the config.
func (e Editor) commit(keys Map, spec Spec, bound []string, note string) (Editor, Map, Result) {
	keys.rebind(spec, bound)
	e.stage = stageList

	if err := config.SaveKeys(toConfigKeys(keys.Overrides())); err != nil {
		return e.refuse(keys, "bound, but not saved: "+err.Error())
	}
	e.status, e.fault = note, false
	return e, keys, Result{}
}

// refuse reports why a change did not happen and goes back to the list.
func (e Editor) refuse(keys Map, why string) (Editor, Map, Result) {
	e.stage = stageList
	e.status, e.fault = why, true
	return e, keys, Result{}
}

// The config file is a line of text, so the two key names that would make it
// ambiguous — the space bar and the comma — are written as words.

// toConfigKeys renders bindings for the config file.
func toConfigKeys(binds map[string][]string) map[string][]string {
	out := make(map[string][]string, len(binds))
	for id, keys := range binds {
		named := make([]string, 0, len(keys))
		for _, k := range keys {
			named = append(named, toConfigKey(k))
		}
		out[id] = named
	}
	return out
}

// FromConfig reads bindings back, dropping any action the app has never heard
// of — and naming it — so a typo in the file cannot silently do nothing.
func FromConfig(binds map[string][]string) (map[string][]string, []string) {
	out := make(map[string][]string, len(binds))
	var unknown []string

	for id, keys := range binds {
		if _, ok := specByID(id); !ok {
			unknown = append(unknown, id)
			continue
		}
		named := make([]string, 0, len(keys))
		for _, k := range keys {
			named = append(named, fromConfigKey(k))
		}
		out[id] = named
	}
	return out, unknown
}

func toConfigKey(k string) string {
	switch k {
	case " ":
		return "space"
	case ",":
		return "comma"
	}
	return k
}

func fromConfigKey(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "space":
		return " "
	case "comma":
		return ","
	}
	return k
}
