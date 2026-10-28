package editor

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kiruva/tyr/internal/syntax"
	"github.com/kiruva/tyr/internal/ui"
)

// Completion is a small list under the word being typed: the language's
// keywords and snippets, and words already in the file. It opens by itself
// once a word is two characters long and something matches, or on
// Ctrl+Space; Tab takes the highlighted entry, ↑/↓ choose, and Esc or any
// other key puts it away. Enter stays a line break — completion never takes
// a key that typing needs.

// minPrefix is how long a word gets before completion opens by itself.
const minPrefix = 2

// shownItems is how many entries the list shows at once.
const shownItems = 8

// scanRows is how far either side of the cursor the file is read for words:
// enough for any file worth completing in, bounded for one that is not.
const scanRows = 3000

type itemKind int

const (
	itemWord itemKind = iota
	itemKeyword
	itemSnippet
)

type item struct {
	label  string
	detail string // what a snippet expands to, in a few words
	body   string // a snippet's text; "" means the label itself
	kind   itemKind
}

type completer struct {
	active bool
	items  []item
	sel    int
	top    int
}

// Completing reports whether the completion list is open.
func (m Model) Completing() bool { return m.comp.active }

// completeKey handles a key while the list is open. A key it does not use
// closes the list and is reported unhandled, for the editor to act on.
func (m *Model) completeKey(msg tea.KeyMsg) bool {
	c := &m.comp
	switch msg.String() {
	case "up", "ctrl+p":
		c.sel = (c.sel - 1 + len(c.items)) % len(c.items)
	case "down", "ctrl+n":
		c.sel = (c.sel + 1) % len(c.items)
	case "tab":
		m.accept(c.items[c.sel])
		return true
	case "esc":
		m.comp = completer{}
		return true
	default:
		m.comp = completer{}
		return false
	}
	if c.sel < c.top {
		c.top = c.sel
	}
	if c.sel >= c.top+shownItems {
		c.top = c.sel - shownItems + 1
	}
	return true
}

// suggest opens or refreshes the list for the word before the cursor. forced
// is Ctrl+Space: any length of word, words from the file in prose too, and a
// single match taken straight away.
func (m *Model) suggest(forced bool) {
	m.comp = completer{}
	if m.sel {
		return
	}
	line := m.buf.lines[m.cur.Row]
	start := m.cur.Col
	for start > 0 && isWord(line[start-1]) {
		start--
	}
	prefix := string(line[start:m.cur.Col])
	if !forced {
		if len([]rune(prefix)) < minPrefix {
			return
		}
		// In the middle of a word, or in a comment or a string, a list
		// popping up is in the way more than it helps.
		if m.cur.Col < len(line) && isWord(line[m.cur.Col]) {
			return
		}
		if kinds := m.kinds(m.cur.Row); m.cur.Col-1 < len(kinds) {
			if k := kinds[m.cur.Col-1]; k == syntax.Comment || k == syntax.Str {
				return
			}
		}
	}
	items := m.candidates(prefix, forced)
	if len(items) == 0 {
		return
	}
	m.comp = completer{active: true, items: items}
	if forced && len(items) == 1 {
		m.accept(items[0])
	}
}

// candidates is what prefix could become, best first: what matches in case
// before what does not, a snippet or keyword before a word from the file, a
// word used often before one used once.
func (m *Model) candidates(prefix string, forced bool) []item {
	lower := strings.ToLower(prefix)
	matches := func(w string) bool { return strings.HasPrefix(strings.ToLower(w), lower) }

	var out []item
	seen := map[string]bool{}
	v := vocabFor(m.lang)
	for _, s := range v.snippets {
		if matches(s.name) {
			out = append(out, item{label: s.name, detail: s.detail, body: s.body, kind: itemSnippet})
			seen[s.name] = true
		}
	}
	for _, w := range v.words {
		if w != prefix && !seen[w] && matches(w) {
			out = append(out, item{label: w, kind: itemKeyword})
			seen[w] = true
		}
	}

	// Words from the file. In prose every word would match something, so
	// there they wait for Ctrl+Space.
	count := map[string]int{}
	if forced || (m.lang != syntax.None && m.lang != syntax.Markdown) {
		first, last := max(m.cur.Row-scanRows, 0), min(m.cur.Row+scanRows, len(m.buf.lines)-1)
		for r := first; r <= last; r++ {
			line := m.buf.lines[r]
			for c := 0; c < len(line); {
				if !isWord(line[c]) {
					c++
					continue
				}
				e := c
				for e < len(line) && isWord(line[e]) {
					e++
				}
				// The word being typed is not a suggestion for itself.
				if (r != m.cur.Row || e != m.cur.Col) && e-c > len([]rune(prefix)) && e-c >= 3 {
					if w := string(line[c:e]); !seen[w] && matches(w) {
						count[w]++
					}
				}
				c = e
			}
		}
	}
	words := make([]string, 0, len(count))
	for w := range count {
		words = append(words, w)
	}
	sort.Slice(words, func(i, j int) bool {
		if count[words[i]] != count[words[j]] {
			return count[words[i]] > count[words[j]]
		}
		return words[i] < words[j]
	})
	for _, w := range words {
		out = append(out, item{label: w, kind: itemWord})
	}

	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := strings.HasPrefix(out[i].label, prefix), strings.HasPrefix(out[j].label, prefix)
		if ci != cj {
			return ci
		}
		return out[i].kind > out[j].kind
	})
	return out[:min(len(out), 50)]
}

// accept puts it in place of the word being typed. A snippet is indented to
// the line it lands on, and leaves the cursor where its body says.
func (m *Model) accept(it item) {
	m.comp = completer{}
	text := it.label
	if it.kind == itemSnippet {
		text = m.indentSnippet(it.body)
	}
	before, after, _ := strings.Cut(text, cursorMark)
	m.change(m.compStart(), m.cur, before+after, editOther)
	for range len([]rune(after)) {
		m.cur = m.leftOf(m.cur)
	}
	m.settle()
}

// compStart is where the word being completed begins, read again from the
// line: the list may have been opened before the last key moved things.
func (m *Model) compStart() Pos {
	line := m.buf.lines[m.cur.Row]
	start := m.cur.Col
	for start > 0 && isWord(line[start-1]) {
		start--
	}
	return Pos{m.cur.Row, start}
}

// indentSnippet turns a snippet's tabs into the file's indent and puts each
// new line under the one it is typed on.
func (m *Model) indentSnippet(body string) string {
	line := m.buf.lines[m.cur.Row]
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	lead := string(line[:n])
	unit := "\t"
	switch {
	case m.lang == syntax.YAML:
		unit = "  "
	case strings.HasPrefix(lead, " ") || m.usesSpaces():
		unit = strings.Repeat(" ", TabWidth)
	}
	body = strings.ReplaceAll(body, "\t", unit)
	return strings.ReplaceAll(body, "\n", "\n"+lead)
}

// usesSpaces reports whether the file indents with spaces: the first indented
// line in it says.
func (m *Model) usesSpaces() bool {
	for _, line := range m.buf.lines[:min(len(m.buf.lines), scanRows)] {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			return line[0] == ' '
		}
	}
	return false
}

// drawCompletion lays the list over the drawn lines, under the word being
// completed, or over it when there is no room below.
func (m *Model) drawCompletion(out []string) {
	c := m.comp
	if !c.active {
		return
	}
	h := len(out)
	shown := c.items[c.top:min(c.top+shownItems, len(c.items))]

	labelW, detailW := 0, 0
	for _, it := range c.items {
		labelW = max(labelW, ansi.StringWidth(it.label))
		detailW = max(detailW, ansi.StringWidth(it.detail))
	}
	boxW := labelW + 2
	if detailW > 0 {
		boxW += detailW + 2
	}
	boxW = min(boxW, m.width)

	row := m.cur.Row - m.top
	at := row + 1
	if at+len(shown) > h {
		at = row - len(shown)
	}
	if at < 0 {
		return
	}
	x := m.gutterWidth() + m.visualCol(m.compStart()) - m.left
	x = max(min(x, m.width-boxW), 0)

	for i, it := range shown {
		text := " " + ui.PadRight(it.label, labelW)
		if detailW > 0 {
			text += "  " + ui.PadRight(it.detail, detailW)
		}
		text = ui.PadRight(ui.TruncTail(text+" ", boxW), boxW)
		style := ui.StatusBar
		if c.top+i == c.sel {
			style = ui.Cursor
		}
		line := out[at+i]
		left := ansi.Truncate(line, x, "")
		if pad := x - ansi.StringWidth(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		out[at+i] = left + style.Render(text) + ansi.TruncateLeft(line, x+boxW, "")
	}
}
