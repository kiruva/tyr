package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/jsonfmt"
	"github.com/kiruva/tyr/internal/jsontree"
)

// The viewer's JSON switches. p shows a file of minified JSON laid out one
// value to a line, and t (see viewtree.go) as a tree to fold. Both are ways of
// looking: the file is not touched, and e still edits it as it is.

// togglePretty swaps the file's lines for their prettified form, or back.
func (m *Model) togglePretty() {
	v := &m.viewer
	switch {
	case v.pretty:
		v.lines, v.plain, v.pretty = v.plain, nil, false
		v.status = "pretty off"
	case v.hex || v.meta:
		v.status = "pretty is for JSON text"
		return
	default:
		text, note, err := prettyJSON(string(v.raw))
		if err != nil {
			v.status = "not JSON: " + err.Error()
			return
		}
		v.plain, v.lines, v.pretty = v.lines, splitLines([]byte(text)), true
		v.status = "pretty" + note
	}

	// The hits were lines of the text that has just been swapped out.
	status := v.status
	m.findMatches()
	if len(v.matches) > 0 || v.query == "" {
		v.status = status
	}
	m.refreshViewer()
}

// prettyJSON lays JSON out for reading (see jsonText).
func prettyJSON(text string) (pretty, note string, err error) {
	plain, note, err := jsonText(text)
	if err != nil {
		return "", "", err
	}
	pretty, err = jsonfmt.Prettify(plain)
	return pretty, note, err
}

// jsonText is text as plain JSON, for the viewer to lay out. JSONC — a VS
// Code settings file, a tsconfig — has its comments and trailing commas taken
// out, and note says so, rather than being refused over them.
func jsonText(text string) (plain, note string, err error) {
	if _, err := jsonfmt.Minify(text); err == nil {
		return text, "", nil
	}
	// StripComments understands comments, so where it says the text breaks
	// is where it really does, not at the first //.
	if plain, err = jsonfmt.StripComments(text); err != nil {
		return "", "", err
	}
	return plain, " · comments and trailing commas not shown", nil
}

// toggleTree switches between the text and the tree. The tree is built the
// first time and kept, so going back to it finds it folded as it was left.
func (m *Model) toggleTree() {
	v := &m.viewer
	switch {
	case v.treeOn:
		v.treeOn = false
		v.status = "tree off"
		return
	case v.hex || v.meta:
		v.status = "the tree is for JSON text"
		return
	case !v.treeBuilt:
		plain, note, err := jsonText(string(v.raw))
		if err != nil {
			v.status = "not JSON: " + err.Error()
			return
		}
		tree, err := jsontree.Build(plain)
		if err != nil {
			v.status = err.Error() // valid JSON, so past the tree's caps
			return
		}
		v.tree, v.treeBuilt = tree, true
		v.status = strings.TrimPrefix(note, " · ")
	default:
		v.status = ""
	}
	v.treeOn = true
}

// onViewTreeKey drives the viewer while the tree is showing. The tree has the
// keys, bar the ones that leave it: q and esc close the viewer, t goes back to
// the text and e edits the file.
func (m Model) onViewTreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.viewer
	if !v.tree.Prompting() {
		switch msg.String() {
		case "q", "esc":
			m.closeViewer()
			return m, nil
		case "t":
			m.toggleTree()
			return m, nil
		case "e":
			return m.editViewed()
		}
	}

	tree, res := v.tree.SetHeight(m.viewport.Height).Update(msg)
	v.tree, v.status = tree, res.Status
	if res.Copy != "" {
		if err := m.editor.Clipboard.Write(res.Copy); err != nil {
			v.status = "could not copy: " + err.Error()
		}
	}
	return m, res.Cmd
}
