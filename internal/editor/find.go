package editor

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/termsafe"
	"github.com/kiruva/tyr/internal/ui"
)

// finder is Ctrl+F: a query typed on the editor's last row, searched as it is
// typed, ignoring case. The query stays after the prompt closes, so F3 and
// Ctrl+G go on finding it, and its matches stay marked.
//
// Ctrl+R opens it with a second field, the replacement: Enter then replaces
// the match that is selected and moves to the next, and Ctrl+A replaces every
// match at once, as one undo step.
type finder struct {
	active bool
	query  string
	from   Pos    // where the search started, so typing more re-searches from there
	status string // "no match", or which match this is

	replacing bool
	repl      string
	field     int // the field typing goes to: 0 the query, 1 the replacement
}

// Finding reports whether the find prompt is open.
func (m Model) Finding() bool { return m.find.active }

func (m *Model) openFind() {
	m.find.active = true
	m.find.replacing, m.find.field = false, 0
	m.find.status = ""
	from, _ := order(m.anchor, m.cur)
	if !m.sel {
		from = m.cur
	}
	m.find.from = from
	m.scrollToCursor()
}

// openReplace opens the prompt with the replacement field.
func (m *Model) openReplace() {
	m.openFind()
	m.find.replacing = true
}

// findKey drives the prompt: typing searches, Enter and ↓ go to the next
// match, ↑ to the one before, Esc closes it leaving the match selected. With
// the replacement field, Enter replaces, Tab switches field and Ctrl+A
// replaces all.
func (m *Model) findKey(msg tea.KeyMsg) bool {
	f := &m.find
	switch msg.String() {
	case "esc", "ctrl+f":
		f.active, f.replacing = false, false
		m.scrollToCursor()
	case "ctrl+r":
		f.replacing, f.field = true, 1
	case "tab", "shift+tab":
		if f.replacing {
			f.field = 1 - f.field
		}
	case "ctrl+a":
		if f.replacing {
			m.replaceAll()
		}
	case "enter":
		if f.replacing {
			m.replaceOne()
		} else {
			m.findNext(1)
		}
	case "down", "ctrl+g", "f3":
		m.findNext(1)
	case "up", "shift+f3":
		m.findNext(-1)
	case "backspace":
		if f.replacing && f.field == 1 {
			if r := []rune(f.repl); len(r) > 0 {
				f.repl = string(r[:len(r)-1])
			}
		} else if r := []rune(f.query); len(r) > 0 {
			f.query = string(r[:len(r)-1])
			m.searchFrom(f.from)
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			if f.replacing && f.field == 1 {
				f.repl += string(msg.Runes)
			} else {
				f.query += string(msg.Runes)
				m.searchFrom(f.from)
			}
		}
	}
	return true
}

// selectedMatch reports whether the selection is a match of the query.
func (m *Model) selectedMatch() (from, to Pos, ok bool) {
	if !m.sel || m.find.query == "" {
		return
	}
	from, to = order(m.anchor, m.cur)
	needle := []rune(m.find.query)
	if from.Row != to.Row || to.Col-from.Col != len(needle) {
		return
	}
	return from, to, foldEqual(m.buf.lines[from.Row][from.Col:to.Col], needle)
}

// replaceOne replaces the selected match, if that is what is selected, and
// selects the next one — which may start right where the replacement ended.
func (m *Model) replaceOne() {
	if m.find.query == "" {
		return
	}
	from, to, ok := m.selectedMatch()
	if !ok {
		m.findNext(1)
		return
	}
	m.change(from, to, m.find.repl, editOther)
	if at, found := m.search(m.cur, 1, true); found {
		m.selectMatch(at)
		return
	}
	m.find.status = "no match"
}

// replaceAll replaces every match in the buffer as one edit, running from the
// first match to the end of the last, so one undo puts them all back.
func (m *Model) replaceAll() {
	needle := []rune(m.find.query)
	n := len(needle)
	if n == 0 {
		return
	}
	var b strings.Builder
	var first, prev Pos
	count := 0
	for row, line := range m.buf.lines {
		for c := 0; c+n <= len(line); {
			if !foldEqual(line[c:c+n], needle) {
				c++
				continue
			}
			at := Pos{row, c}
			if count == 0 {
				first = at
			} else {
				b.WriteString(m.buf.slice(prev, at))
			}
			b.WriteString(m.find.repl)
			prev = Pos{row, c + n}
			count++
			c += n
		}
	}
	if count == 0 {
		m.find.status = "no match"
		return
	}
	m.change(first, prev, b.String(), editOther)
	m.find.status = fmt.Sprintf("replaced %d", count)
}

// searchFrom selects the first match at or after p, wrapping round.
func (m *Model) searchFrom(p Pos) {
	if m.find.query == "" {
		m.sel, m.find.status = false, ""
		m.cur = m.buf.clamp(m.find.from)
		m.settle()
		return
	}
	if at, ok := m.search(p, 1, true); ok {
		m.selectMatch(at)
		return
	}
	m.find.status = "no match"
	m.sel = false
}

// findNext moves to the next match after the cursor (dir 1) or the one before
// it (dir -1).
func (m *Model) findNext(dir int) {
	if m.find.query == "" {
		if !m.find.active {
			m.openFind()
		}
		return
	}
	start := m.cur
	if dir < 0 && m.sel {
		start, _ = order(m.anchor, m.cur)
	}
	if at, ok := m.search(start, dir, false); ok {
		m.selectMatch(at)
		return
	}
	m.find.status = "no match"
}

func (m *Model) selectMatch(at Pos) {
	n := len([]rune(m.find.query))
	m.anchor, m.cur, m.sel = at, Pos{at.Row, at.Col + n}, true
	m.find.status = ""
	m.settle()
}

// search looks for the query from p in direction dir, wrapping round the end
// of the buffer. inclusive allows a match starting at p itself.
func (m *Model) search(p Pos, dir int, inclusive bool) (Pos, bool) {
	needle := []rune(m.find.query)
	rows := len(m.buf.lines)
	for i := 0; i <= rows; i++ {
		row := ((p.Row+dir*i)%rows + rows) % rows
		line := m.buf.lines[row]
		if dir > 0 {
			start := 0
			if i == 0 {
				start = p.Col
				if !inclusive {
					start++
				}
			}
			for c := start; c+len(needle) <= len(line); c++ {
				if i == rows && c >= p.Col {
					break
				}
				if foldEqual(line[c:c+len(needle)], needle) {
					return Pos{row, c}, true
				}
			}
			continue
		}
		end := len(line) - len(needle)
		if i == 0 {
			end = min(end, p.Col-1)
		}
		for c := end; c >= 0; c-- {
			if foldEqual(line[c:c+len(needle)], needle) {
				return Pos{row, c}, true
			}
		}
	}
	return Pos{}, false
}

func foldEqual(a, b []rune) bool {
	for i := range b {
		if unicode.ToLower(a[i]) != unicode.ToLower(b[i]) {
			return false
		}
	}
	return true
}

// matchesIn marks the runes of line that belong to a match of needle.
func matchesIn(line, needle []rune) []bool {
	if len(needle) == 0 || len(needle) > len(line) {
		return make([]bool, len(line))
	}
	out := make([]bool, len(line))
	for c := 0; c+len(needle) <= len(line); c++ {
		if foldEqual(line[c:c+len(needle)], needle) {
			for k := range needle {
				out[c+k] = true
			}
		}
	}
	return out
}

func (f *finder) view(width int) string {
	text := " find: " + termsafe.Line(f.query)
	hint := "enter next · ↑ previous · esc done"
	if f.replacing {
		mark := [2]string{" ", " "}
		mark[f.field] = "›"
		text = mark[0] + "find: " + termsafe.Line(f.query) + "  " + mark[1] + "replace: " + termsafe.Line(f.repl)
		hint = "enter replace · ↓ skip · ^A all · tab field · esc done"
	}
	if f.status != "" {
		hint = f.status + " · " + hint
	}
	return promptBar(text, hint, width)
}

// promptBar draws a prompt on the editor's last row. What was typed keeps its
// end in view; the hint after it is what gets cut when the row is short.
func promptBar(fields, hint string, width int) string {
	fields = ui.TruncTail(fields, width-1)
	return ui.StatusBar.Width(width).Render(ui.TruncHead(fields+"   "+hint, width-1))
}
