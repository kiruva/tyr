// Package jsontree is the viewer's tree view of a JSON document: every object
// and array a row that folds, every value as it was written, and the jq path
// of whatever the cursor is on.
//
// It is split out of the root model the way app/theme is: a Model holding only
// the tree, Update taking a keypress and returning the new Model plus a
// Result, and View taking the size it is drawn at. It knows nothing about the
// app; what to do with a copied path or a status message is the caller's.
//
// The document is held flat, in the order it was written, each node knowing
// where its descendants end. Folding is then a skip over a range rather than a
// walk of a subtree, and drawing touches only the rows on screen, so a 16 MB
// file is as quick to scroll as a small one.
package jsontree

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kiruva/tyr/internal/termsafe"
	"github.com/kiruva/tyr/internal/ui"
)

// MaxNodes is the most values a tree holds — about a hundred megabytes of
// nodes. A 16 MB file of small values can have several times that many.
const MaxNodes = 2_000_000

// MaxDepth is how deeply nested a document the tree takes. Nothing written by
// a person comes near it, and the indentation alone would be off the screen.
const MaxDepth = 1000

// ErrTooLarge is a document past MaxNodes or MaxDepth.
var ErrTooLarge = errors.New("too large for the tree view")

// Kind is what a node holds.
type Kind uint8

const (
	Object Kind = iota
	Array
	String
	Number
	Bool
	Null
)

// node is one value. A container's descendants are the nodes after it, up to
// end; a scalar's end is the next node.
type node struct {
	key    string // the member name in an object; "" otherwise
	value  string // a scalar as written; a string decoded
	parent int32  // -1 for the root
	end    int32
	size   int32 // members or items, for a container
	index  int32 // the position in an array; -1 otherwise
	depth  uint16
	kind   Kind
	open   bool
}

func (n node) container() bool { return n.kind == Object || n.kind == Array }

// Result is what one keypress produced besides the new Model.
type Result struct {
	Cmd tea.Cmd
	// Status is worth showing on the status line until the next key.
	Status string
	// Copy is text for the clipboard: the path y copied.
	Copy string
}

// Model is one document as a tree.
type Model struct {
	nodes []node
	rows  []int32 // the nodes on show, in order
	cur   int     // index into rows
	top   int     // first row drawn
	page  int     // rows the window holds

	input     textinput.Model
	prompting bool
	query     string
}

// Build reads a JSON document into a tree with its top level showing. Numbers
// are kept as written; a byte order mark is ignored. It fails on anything
// that is not exactly one JSON value, and with ErrTooLarge past the caps.
func Build(text string) (Model, error) {
	dec := json.NewDecoder(strings.NewReader(strings.TrimPrefix(text, "\uFEFF")))
	dec.UseNumber()

	var (
		nodes  []node
		stack  []int32 // the containers still open
		key    string  // a member name waiting for its value
		hasKey bool
	)
	add := func(kind Kind, value string) error {
		if len(nodes) >= MaxNodes {
			return fmt.Errorf("%w: over %d values", ErrTooLarge, MaxNodes)
		}
		n := node{kind: kind, value: value, parent: -1, index: -1, depth: uint16(len(stack))}
		if len(stack) > 0 {
			p := &nodes[stack[len(stack)-1]]
			n.parent = stack[len(stack)-1]
			if p.kind == Object {
				n.key, hasKey = key, false
			} else {
				n.index = p.size
			}
			p.size++
		}
		n.end = int32(len(nodes) + 1)
		nodes = append(nodes, n)
		return nil
	}

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Model{}, err
		}
		if len(stack) == 0 && len(nodes) > 0 {
			return Model{}, errors.New("more than one JSON value")
		}

		// In an object, every other string is a member name.
		inObject := len(stack) > 0 && nodes[stack[len(stack)-1]].kind == Object
		if s, ok := tok.(string); ok && inObject && !hasKey {
			key, hasKey = s, true
			continue
		}

		switch t := tok.(type) {
		case json.Delim:
			if t == '}' || t == ']' {
				top := stack[len(stack)-1]
				nodes[top].end = int32(len(nodes))
				stack = stack[:len(stack)-1]
				continue
			}
			if len(stack) >= MaxDepth {
				return Model{}, fmt.Errorf("%w: nested over %d deep", ErrTooLarge, MaxDepth)
			}
			kind := Object
			if t == '[' {
				kind = Array
			}
			if err := add(kind, ""); err != nil {
				return Model{}, err
			}
			stack = append(stack, int32(len(nodes)-1))
		case string:
			err = add(String, t)
		case json.Number:
			err = add(Number, t.String())
		case bool:
			err = add(Bool, strconv.FormatBool(t))
		case nil:
			err = add(Null, "null")
		}
		if err != nil {
			return Model{}, err
		}
	}
	switch {
	case len(nodes) == 0:
		return Model{}, errors.New("no JSON value")
	case len(stack) > 0:
		return Model{}, io.ErrUnexpectedEOF
	}

	nodes[0].open = true
	m := Model{nodes: nodes, page: 1}
	m.layout()
	return m, nil
}

// SetHeight is how many rows the window holds, for paging and for keeping the
// cursor in view.
func (m Model) SetHeight(h int) Model {
	m.page = max(h, 1)
	m.follow()
	return m
}

// Prompting reports whether the find field has the keys.
func (m Model) Prompting() bool { return m.prompting }

// InputView is the find field, for the caller's footer.
func (m Model) InputView() string { return m.input.View() }

// Tick hands a non-key message (the cursor blink) to the find field.
func (m Model) Tick(msg tea.Msg) (Model, tea.Cmd) {
	if !m.prompting {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// Path is the jq path of the row the cursor is on.
func (m Model) Path() string { return m.path(m.rows[m.cur]) }

// Scroll moves the cursor by step rows, which is what the wheel does.
func (m Model) Scroll(step int) Model {
	m.cur = min(max(m.cur+step, 0), len(m.rows)-1)
	m.follow()
	return m
}

// Update drives the tree.
func (m Model) Update(msg tea.KeyMsg) (Model, Result) {
	if m.prompting {
		return m.onFindKey(msg)
	}

	k := msg.String()
	if ui.MoveCursor(&m.cur, k, len(m.rows)-1, m.page) {
		m.follow()
		return m, Result{}
	}

	i := m.rows[m.cur]
	n := &m.nodes[i]
	switch k {
	case "right", "l":
		switch {
		case n.open && n.size > 0:
			m.cur++ // into it: its first child is the next row
		case n.container() && n.size > 0:
			n.open = true
			m.layout()
		}
	case "left", "h":
		switch {
		case n.open && i != 0:
			n.open = false
			m.layout()
		case n.parent >= 0:
			m.goTo(n.parent)
		}
	case "enter", " ":
		if n.container() && n.size > 0 && (i != 0 || !n.open) {
			n.open = !n.open
			m.layout()
		}
	case "*":
		for j := i; j < n.end; j++ {
			if m.nodes[j].container() {
				m.nodes[j].open = true
			}
		}
		m.layout()
	case "-":
		for j := range m.nodes[1:] {
			m.nodes[j+1].open = false
		}
		m.layout()
	case "y":
		p := m.path(i)
		return m, Result{Copy: p, Status: "copied " + p}
	case "/":
		m.input = textinput.New()
		m.input.Prompt = ""
		m.input.Placeholder = "key or value to find"
		m.input.SetValue(m.query)
		m.input.CursorEnd()
		m.prompting = true
		return m, Result{Cmd: m.input.Focus()}
	case "n":
		return m.find(1)
	case "N":
		return m.find(-1)
	}
	m.follow()
	return m, Result{}
}

// onFindKey drives the find field: enter looks, esc gives up.
func (m Model) onFindKey(msg tea.KeyMsg) (Model, Result) {
	switch msg.String() {
	case "enter":
		m.prompting = false
		m.input.Blur()
		m.query = m.input.Value()
		return m.find(1)
	case "esc":
		m.prompting = false
		m.input.Blur()
		return m, Result{}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, Result{Cmd: cmd}
}

// find moves to the next node, forward or back from the cursor and wrapping,
// whose key or value holds the query. Closed nodes are searched too, and
// opened on the way to a hit.
func (m Model) find(step int) (Model, Result) {
	if m.query == "" {
		return m, Result{}
	}
	needle := strings.ToLower(m.query)
	total := len(m.nodes)
	from := int(m.rows[m.cur])
	for k := 1; k <= total; k++ {
		i := ((from+k*step)%total + total) % total
		n := m.nodes[i]
		if strings.Contains(strings.ToLower(n.key), needle) ||
			(!n.container() && strings.Contains(strings.ToLower(n.value), needle)) {
			for p := n.parent; p >= 0; p = m.nodes[p].parent {
				m.nodes[p].open = true
			}
			m.layout()
			m.goTo(int32(i))
			return m, Result{Status: m.path(int32(i))}
		}
	}
	return m, Result{Status: "no match for " + m.query}
}

// layout lists the nodes on show — each open container's children, skipping
// over a closed one's — and keeps the cursor on the node it was on, or the
// nearest one still showing that holds it.
func (m *Model) layout() {
	var at int32
	if len(m.rows) > 0 {
		at = m.rows[m.cur]
	}
	m.rows = m.rows[:0]
	for i := int32(0); i < int32(len(m.nodes)); {
		m.rows = append(m.rows, i)
		if m.nodes[i].open {
			i++
		} else {
			i = m.nodes[i].end
		}
	}
	// The outermost closed node above at is the one showing.
	show := at
	for p := m.nodes[at].parent; p >= 0; p = m.nodes[p].parent {
		if !m.nodes[p].open {
			show = p
		}
	}
	m.goTo(show)
}

// goTo puts the cursor on a node that is showing.
func (m *Model) goTo(i int32) {
	m.cur = sort.Search(len(m.rows), func(r int) bool { return m.rows[r] >= i })
	m.cur = min(m.cur, len(m.rows)-1)
	m.follow()
}

// follow scrolls just enough to keep the cursor in the window.
func (m *Model) follow() {
	m.top = scrollTop(m.top, m.cur, m.page, len(m.rows))
}

func scrollTop(top, cur, page, rows int) int {
	top = min(top, cur)
	if cur >= top+page {
		top = cur - page + 1
	}
	return max(min(top, rows-page), 0)
}

// path is jq's path to a node: .name for a key that is an identifier,
// ["any key"] for one that is not, [3] for an item, and . for the whole.
func (m Model) path(i int32) string {
	var parts []string
	for ; m.nodes[i].parent >= 0; i = m.nodes[i].parent {
		n := m.nodes[i]
		switch {
		case n.index >= 0:
			parts = append(parts, "["+strconv.Itoa(int(n.index))+"]")
		case identifier(n.key):
			parts = append(parts, "."+n.key)
		default:
			parts = append(parts, "["+quote(n.key)+"]")
		}
	}
	var b strings.Builder
	for j := len(parts) - 1; j >= 0; j-- {
		b.WriteString(parts[j])
	}
	p := b.String()
	if !strings.HasPrefix(p, ".") {
		p = "." + p
	}
	return p
}

// identifier reports whether jq takes a key after a dot.
func identifier(s string) bool {
	for i, c := range s {
		letter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return s != ""
}

// quote writes a key as a JSON string, which is how jq reads one; <, > and &
// are left as they are rather than escaped for HTML.
func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(b.String(), "\n")
}

// View draws the rows in the window, height lines of at most width cells.
func (m Model) View(width, height int) string {
	top := scrollTop(m.top, m.cur, height, len(m.rows))
	lines := make([]string, height)
	for r := range height {
		if top+r < len(m.rows) {
			lines[r] = m.row(m.rows[top+r], top+r == m.cur, width)
		}
	}
	return strings.Join(lines, "\n")
}

// row draws one node: indented by depth, a fold marker on a container that
// has anything to fold, the key or index, then the value — or, for a
// container, what is in it.
func (m Model) row(i int32, cursor bool, width int) string {
	n := m.nodes[i]

	marker := "  "
	if n.container() && n.size > 0 {
		marker = "▸ "
		if n.open {
			marker = "▾ "
		}
	}
	label := ""
	switch {
	case n.index >= 0:
		label = "[" + strconv.Itoa(int(n.index)) + "]: "
	case n.parent >= 0:
		label = termsafe.Line(n.key) + ": "
	}

	value, style := termsafe.Line(n.value), lipgloss.NewStyle()
	switch n.kind {
	case Object:
		value, style = summary(n.size, "{}", "key", "keys"), ui.Faint
	case Array:
		value, style = summary(n.size, "[]", "item", "items"), ui.Faint
	case String:
		value, style = `"`+value+`"`, ui.SyntaxString
	case Number:
		style = ui.SyntaxNumber
	case Bool, Null:
		style = ui.SyntaxKeyword
	}

	lead := strings.Repeat("  ", int(n.depth)) + marker + label
	if cursor {
		line := ansi.Truncate(lead+value, width, "…")
		return ui.Cursor.Render(ui.PadRight(line, width))
	}
	return ansi.Truncate(lead+style.Render(value), width, "…")
}

// summary says what a container holds: {} or [] when nothing, else a count.
func summary(size int32, empty, one, many string) string {
	switch size {
	case 0:
		return empty
	case 1:
		return empty[:1] + "1 " + one + empty[1:]
	}
	return empty[:1] + strconv.Itoa(int(size)) + " " + many + empty[1:]
}
