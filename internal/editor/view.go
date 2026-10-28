package editor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/kiruva/tyr/internal/syntax"
	"github.com/kiruva/tyr/internal/ui"
)

// ---- columns ----

// runeCols is how many columns r takes when it starts at column x. A tab runs
// to the next tab stop; any other control character is drawn as ^X.
func runeCols(r rune, x int) int {
	switch {
	case r == '\t':
		return TabWidth - x%TabWidth
	case isControl(r):
		return 2
	default:
		return max(runewidth.RuneWidth(r), 0)
	}
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) }

// visualCol is the screen column p is drawn at, before scrolling.
func (m *Model) visualCol(p Pos) int {
	x := 0
	for _, r := range m.buf.lines[p.Row][:p.Col] {
		x += runeCols(r, x)
	}
	return x
}

// colAt is the rune index in row nearest to screen column x: the character
// that covers x, or the end of the line.
func (m *Model) colAt(row, x int) int {
	line := m.buf.lines[row]
	at := 0
	for i, r := range line {
		w := runeCols(r, at)
		if x < at+w {
			if x-at > w/2 && w > 1 && r != '\t' {
				return i + 1
			}
			return i
		}
		at += w
	}
	return len(line)
}

// gutterWidth is the width of the line numbers and the bar after them.
func (m *Model) gutterWidth() int { return len(fmt.Sprint(len(m.buf.lines))) + 3 }

// textWidth is how many columns of text fit beside the gutter.
func (m *Model) textWidth() int { return max(m.width-m.gutterWidth(), 1) }

// textHeight is how many lines of text fit: a prompt takes the last row.
func (m *Model) textHeight() int {
	if m.Prompting() {
		return max(m.height-1, 1)
	}
	return m.height
}

// scrollToCursor moves the window just enough to keep the cursor in it.
func (m *Model) scrollToCursor() {
	h := m.textHeight()
	if m.cur.Row < m.top {
		m.top = m.cur.Row
	}
	if m.cur.Row >= m.top+h {
		m.top = m.cur.Row - h + 1
	}
	x, w := m.visualCol(m.cur), m.textWidth()
	if x < m.left {
		m.left = x
	}
	if x >= m.left+w {
		m.left = x - w + 1
	}
}

// ---- highlighting ----

// states returns the highlighter as it stands at the start of row, working
// forward from the last line it is known for. An edit forgets the states after
// the line it touched (see touched).
func (m *Model) stateAt(row int) syntax.Highlighter {
	if len(m.hl) == 0 {
		m.hl = append(m.hl, *syntax.New(m.lang))
	}
	for len(m.hl) <= row {
		h := m.hl[len(m.hl)-1]
		h.Line(string(m.buf.lines[len(m.hl)-1]))
		m.hl = append(m.hl, h)
	}
	return m.hl[row]
}

// kinds is the syntax kind of each rune of row.
func (m *Model) kinds(row int) []syntax.Kind {
	line := m.buf.lines[row]
	out := make([]syntax.Kind, 0, len(line))
	if m.lang == syntax.None || !m.color {
		for range line {
			out = append(out, syntax.Plain)
		}
		return out
	}
	h := m.stateAt(row)
	for _, sp := range h.Line(string(line)) {
		for range sp.Text {
			out = append(out, sp.Kind)
		}
	}
	for len(out) < len(line) {
		out = append(out, syntax.Plain)
	}
	return out
}

// ---- drawing ----

// cellStyle is how one character is drawn; the zero value is plain text.
type cellStyle int

const (
	stylePlain cellStyle = iota
	styleKeyword
	styleString
	styleComment
	styleNumber
	styleHeading
	styleKey
	styleAdded
	styleRemoved
	styleControl
	styleMatch
	styleBracket
	styleSelected
	styleCursor
)

var syntaxStyle = map[syntax.Kind]cellStyle{
	syntax.Keyword: styleKeyword, syntax.Str: styleString, syntax.Comment: styleComment,
	syntax.Number: styleNumber, syntax.Heading: styleHeading, syntax.Key: styleKey,
	syntax.Name: styleKey, syntax.Added: styleAdded, syntax.Removed: styleRemoved,
}

func (s cellStyle) render(text string) string {
	switch s {
	case styleKeyword:
		return ui.SyntaxKeyword.Render(text)
	case styleString:
		return ui.SyntaxString.Render(text)
	case styleComment:
		return ui.SyntaxComment.Render(text)
	case styleNumber:
		return ui.SyntaxNumber.Render(text)
	case styleHeading:
		return ui.SyntaxHeading.Render(text)
	case styleKey:
		return ui.SyntaxKey.Render(text)
	case styleAdded:
		return ui.SyntaxAdded.Render(text)
	case styleRemoved:
		return ui.SyntaxRemoved.Render(text)
	case styleControl:
		return ui.Faint.Render(text)
	case styleMatch:
		return ui.Match.Render(text)
	case styleBracket:
		return ui.BracketMatch.Render(text)
	case styleSelected:
		return ui.CursorInactive.Render(text)
	case styleCursor:
		return ui.Cursor.Render(text)
	default:
		return text
	}
}

// View draws the editor: exactly height lines of width columns.
func (m *Model) View() string {
	h := m.textHeight()
	out := make([]string, 0, m.height)
	digits := m.gutterWidth() - 3
	selFrom, selTo := order(m.anchor, m.cur)
	needle := []rune(m.find.query)
	var brackets []Pos
	if from, to, ok := m.bracketPair(); ok {
		brackets = []Pos{from, to}
	}

	for i := range h {
		row := m.top + i
		if row >= len(m.buf.lines) {
			out = append(out, ui.Gutter.Render(strings.Repeat(" ", digits)+" ~"))
			continue
		}
		gutter := ui.Gutter.Render(fmt.Sprintf("%*d │ ", digits, row+1))
		out = append(out, gutter+m.drawLine(row, selFrom, selTo, needle, brackets))
	}
	m.drawCompletion(out)
	if m.find.active {
		out = append(out, m.find.view(m.width))
	}
	if m.jump.active {
		out = append(out, m.jump.view(m.width))
	}
	return strings.Join(out, "\n")
}

// drawLine draws the visible part of one line, run by run of equal style.
func (m *Model) drawLine(row int, selFrom, selTo Pos, needle []rune, brackets []Pos) string {
	line := m.buf.lines[row]
	kinds := m.kinds(row)
	matches := matchesIn(line, needle)
	w := m.textWidth()

	var sb strings.Builder
	var run strings.Builder
	runStyle := stylePlain
	flush := func() {
		if run.Len() > 0 {
			sb.WriteString(runStyle.render(run.String()))
			run.Reset()
		}
	}
	put := func(text string, s cellStyle) {
		if s != runStyle {
			flush()
			runStyle = s
		}
		run.WriteString(text)
	}

	x := 0
	for i := 0; i <= len(line); i++ {
		p := Pos{row, i}
		style := stylePlain
		switch {
		case p == m.cur:
			style = styleCursor
		case m.sel && !p.Before(selFrom) && p.Before(selTo):
			style = styleSelected
		case slices.Contains(brackets, p):
			style = styleBracket
		case i < len(line) && matches[i]:
			style = styleMatch
		case i < len(line) && isControl(line[i]):
			style = styleControl
		case i < len(line):
			style = syntaxStyle[kinds[i]]
		}

		if i == len(line) {
			// The end of the line: only the cursor, or a selected line break,
			// shows here.
			if x >= m.left && x < m.left+w && (style == styleCursor || style == styleSelected) {
				put(" ", style)
			}
			break
		}

		r := line[i]
		cols := runeCols(r, x)
		var glyph string
		switch {
		case r == '\t':
			glyph = strings.Repeat(" ", cols)
		case isControl(r):
			glyph = "^" + string(r^0x40)
			if r >= 0x80 {
				glyph = "^?"
			}
		default:
			glyph = string(r)
		}
		if r == '\t' && style == styleCursor {
			// The cursor sits on the tab's first column, not across all of it.
			if x >= m.left && x < m.left+w {
				put(" ", styleCursor)
			}
			x++
			cols--
			style, glyph = stylePlain, strings.Repeat(" ", cols)
			if cols == 0 {
				continue
			}
		}
		switch {
		case x+cols <= m.left || x >= m.left+w:
			// off screen
		case x < m.left || x+cols > m.left+w:
			// cut by an edge: blank the part that shows
			put(strings.Repeat(" ", min(x+cols, m.left+w)-max(x, m.left)), style)
		default:
			put(glyph, style)
		}
		x += cols
	}
	flush()
	return sb.String() + strings.Repeat(" ", max(w-lipgloss.Width(sb.String()), 0))
}
