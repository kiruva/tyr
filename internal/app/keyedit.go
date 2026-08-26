package app

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

// keyEditStage is what the editor is doing.
type keyEditStage int

const (
	keyEditList    keyEditStage = iota // browsing the actions
	keyEditCapture                     // waiting for the key to bind
	keyEditAdd                         // waiting for a key to add to the action
)

// keyEditState is the editor while it is open.
type keyEditState struct {
	stage  keyEditStage
	cursor int
	status string
	fault  bool // the status is a refusal rather than a note
}

// keyEditRows is how many actions the editor lists at once; the drawn window is
// sized from the terminal and this is only the paging step.
const keyEditRows = 14

// openKeyEditor shows the keymap.
func (m *Model) openKeyEditor() {
	m.keyEdit = keyEditState{}
	m.mode = modeKeys
}

// onKeyEditKey drives the editor: browsing, or capturing the next keypress.
func (m Model) onKeyEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.keyEdit.stage != keyEditList {
		return m.captureKey(msg)
	}

	last := len(keySpecs) - 1
	switch msg.String() {
	case "esc", "q":
		m.mode = modeNormal
		m.keyEdit = keyEditState{}
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.keyEdit.cursor = max(m.keyEdit.cursor-1, 0)
	case "down", "j":
		m.keyEdit.cursor = min(m.keyEdit.cursor+1, last)
	case "pgup":
		m.keyEdit.cursor = max(m.keyEdit.cursor-keyEditRows, 0)
	case "pgdown":
		m.keyEdit.cursor = min(m.keyEdit.cursor+keyEditRows, last)
	case "home", "g":
		m.keyEdit.cursor = 0
	case "end", "G":
		m.keyEdit.cursor = last
	case "enter":
		m.keyEdit.stage = keyEditCapture
		m.keyEdit.status, m.keyEdit.fault = "", false
	case "a":
		m.keyEdit.stage = keyEditAdd
		m.keyEdit.status, m.keyEdit.fault = "", false
	case "d":
		return m.resetBinding()
	case "D":
		return m.resetAllBindings()
	}
	return m, nil
}

// captureKey takes the next keypress as the binding, unless it is one of the
// keys the app cannot give away.
func (m Model) captureKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pressed := msg.String()
	spec := keySpecs[m.keyEdit.cursor]

	if pressed == "esc" {
		m.keyEdit.stage = keyEditList
		m.keyEdit.status, m.keyEdit.fault = "", false
		return m, nil
	}
	if reservedKeys[pressed] {
		return m.refuse(fmt.Sprintf("%s is reserved — it is how you get out of things", prettyKey(pressed)))
	}
	if other, taken := m.keys.boundTo(pressed, spec.id); taken {
		return m.refuse(fmt.Sprintf("%s is already %s", prettyKey(pressed), other.desc))
	}

	keys := []string{pressed}
	if m.keyEdit.stage == keyEditAdd {
		keys = append(m.keys.boundKeys(spec), pressed)
	}
	return m.commitBinding(spec, keys, fmt.Sprintf("%s → %s", spec.desc, keyList(keys)))
}

// resetBinding puts one action back to what it ships with.
func (m Model) resetBinding() (tea.Model, tea.Cmd) {
	spec := keySpecs[m.keyEdit.cursor]
	if sameKeys(m.keys.boundKeys(spec), spec.def) {
		m.keyEdit.status, m.keyEdit.fault = spec.desc+" is already the default", false
		return m, nil
	}

	// A default can collide with something the user has since bound elsewhere.
	for _, k := range spec.def {
		if other, taken := m.keys.boundTo(k, spec.id); taken {
			return m.refuse(fmt.Sprintf("cannot reset: %s is now %s", prettyKey(k), other.desc))
		}
	}
	return m.commitBinding(spec, spec.def, spec.desc+" back to "+keyList(spec.def))
}

// resetAllBindings throws away every rebinding at once.
func (m Model) resetAllBindings() (tea.Model, tea.Cmd) {
	m.keys = defaultKeys()
	if err := config.SaveKeys(nil); err != nil {
		return m.refuse("could not write the config: " + err.Error())
	}
	m.keyEdit.status, m.keyEdit.fault = "every key back to its default", false
	return m, nil
}

// commitBinding applies a change and writes the config.
func (m Model) commitBinding(spec keySpec, keys []string, note string) (tea.Model, tea.Cmd) {
	m.keys.rebind(spec, keys)
	m.keyEdit.stage = keyEditList

	if err := config.SaveKeys(toConfigKeys(m.keys.overrides())); err != nil {
		return m.refuse("bound, but not saved: " + err.Error())
	}
	m.keyEdit.status, m.keyEdit.fault = note, false
	return m, nil
}

// refuse reports why a change did not happen and goes back to the list.
func (m Model) refuse(why string) (tea.Model, tea.Cmd) {
	m.keyEdit.stage = keyEditList
	m.keyEdit.status, m.keyEdit.fault = why, true
	return m, nil
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

// fromConfigKeys reads bindings back, dropping any action the app has never
// heard of so a typo in the file cannot silently do nothing.
func fromConfigKeys(binds map[string][]string) (map[string][]string, []string) {
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
