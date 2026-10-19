package editor

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/termsafe"
	"github.com/kiruva/tyr/internal/ui"
)

// finder is Ctrl+F: a query typed on the editor's last row, searched as it is
// typed, ignoring case. The query stays after the prompt closes, so F3 and
// Ctrl+G go on finding it, and its matches stay marked.
type finder struct {
	active bool
	query  string
	from   Pos    // where the search started, so typing more re-searches from there
	status string // "no match", or which match this is
}

// Finding reports whether the find prompt is open.
func (m Model) Finding() bool { return m.find.active }

func (m *Model) openFind() {
	m.find.active = true
	m.find.status = ""
	from, _ := order(m.anchor, m.cur)
	if !m.sel {
		from = m.cur
	}
	m.find.from = from
	m.scrollToCursor()
}

// findKey drives the prompt: typing searches, Enter and ↓ go to the next
// match, ↑ to the one before, Esc closes it leaving the match selected.
func (m *Model) findKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "esc", "ctrl+f":
		m.find.active = false
		m.scrollToCursor()
	case "enter", "down", "ctrl+g", "f3":
		m.findNext(1)
	case "up", "shift+f3":
		m.findNext(-1)
	case "backspace":
		if r := []rune(m.find.query); len(r) > 0 {
			m.find.query = string(r[:len(r)-1])
			m.searchFrom(m.find.from)
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.find.query += string(msg.Runes)
			m.searchFrom(m.find.from)
		}
	}
	return true
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
	if f.status != "" {
		hint = f.status + " · " + hint
	}
	return ui.StatusBar.Width(width).Render(ui.TruncTail(text+"   "+hint, width-1))
}
