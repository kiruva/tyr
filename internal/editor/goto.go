package editor

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/termsafe"
)

// jumper is Ctrl+L: a line number, or line:column, typed on the editor's last
// row. The column is counted the way the footer shows it, so a tab counts as
// the columns it covers.
type jumper struct {
	active bool
	text   string
	status string
}

// Prompting reports whether a prompt — find, replace or go to line — has the
// keyboard.
func (m Model) Prompting() bool { return m.find.active || m.jump.active }

func (m *Model) openJump() {
	m.jump = jumper{active: true}
	m.scrollToCursor()
}

func (m *Model) jumpKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "esc", "ctrl+l":
		m.jump.active = false
		m.scrollToCursor()
	case "enter":
		m.jumpTo()
	case "backspace":
		if r := []rune(m.jump.text); len(r) > 0 {
			m.jump.text = string(r[:len(r)-1])
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.jump.text += string(msg.Runes)
			m.jump.status = ""
		}
	}
	return true
}

// jumpTo goes to the line typed, clamped to the buffer, and puts it in the
// middle of the screen.
func (m *Model) jumpTo() {
	lineText, colText, hasCol := strings.Cut(strings.TrimSpace(m.jump.text), ":")
	line, err := strconv.Atoi(lineText)
	col := 1
	if err == nil && hasCol {
		col, err = strconv.Atoi(colText)
	}
	if err != nil {
		m.jump.status = "a line number, or line:column"
		return
	}
	m.jump.active = false
	row := min(max(line-1, 0), len(m.buf.lines)-1)
	m.moveTo(Pos{row, m.colAt(row, max(col-1, 0))}, false)
	h := m.textHeight()
	m.top = min(max(row-h/2, 0), max(len(m.buf.lines)-h, 0))
	m.settle()
}

func (j *jumper) view(width int) string {
	text := " go to line: " + termsafe.Line(j.text)
	hint := "line or line:col · enter go · esc cancel"
	if j.status != "" {
		hint = j.status + " · " + hint
	}
	return promptBar(text, hint, width)
}
