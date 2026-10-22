package editor

import "github.com/kiruva/tyr/internal/syntax"

// bracketReach is how many characters the search for a bracket's partner
// looks through before giving up, so an unmatched one in a big file does not
// cost a scan of the whole file on every keystroke.
const bracketReach = 200_000

var partners = map[rune]struct {
	other rune
	dir   int
}{
	'(': {')', 1}, '[': {']', 1}, '{': {'}', 1},
	')': {'(', -1}, ']': {'[', -1}, '}': {'{', -1},
}

// bracketPair finds the bracket the cursor is on — or just after — and its
// partner. Brackets in strings and comments are not code, and are skipped.
func (m *Model) bracketPair() (from, to Pos, ok bool) {
	line := m.buf.lines[m.cur.Row]
	for _, col := range []int{m.cur.Col, m.cur.Col - 1} {
		if col < 0 || col >= len(line) {
			continue
		}
		if _, isBracket := partners[line[col]]; !isBracket {
			continue
		}
		at := Pos{m.cur.Row, col}
		if !isCode(m.kinds(at.Row), col) {
			return Pos{}, Pos{}, false
		}
		to, ok = m.partnerOf(at)
		return at, to, ok
	}
	return Pos{}, Pos{}, false
}

// partnerOf walks from the bracket at p to the one that closes (or opens) it.
func (m *Model) partnerOf(p Pos) (Pos, bool) {
	open := m.buf.lines[p.Row][p.Col]
	want := partners[open]
	depth, seen := 0, 0
	row, col := p.Row, p.Col
	kinds := m.kinds(row)
	for seen < bracketReach {
		line := m.buf.lines[row]
		if col >= 0 && col < len(line) {
			seen++
			if r := line[col]; (r == open || r == want.other) && isCode(kinds, col) {
				if r == open {
					depth++
				} else {
					depth--
				}
				if depth == 0 {
					return Pos{row, col}, true
				}
			}
			col += want.dir
			continue
		}
		row += want.dir
		if row < 0 || row >= len(m.buf.lines) {
			return Pos{}, false
		}
		kinds = m.kinds(row)
		col = 0
		if want.dir < 0 {
			col = len(m.buf.lines[row]) - 1
		}
		seen++
	}
	return Pos{}, false
}

// isCode reports whether the character at col is neither in a string nor in a
// comment.
func isCode(kinds []syntax.Kind, col int) bool {
	k := kinds[col]
	return k != syntax.Str && k != syntax.Comment
}
