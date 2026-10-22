package editor

import (
	"strings"
	"unicode"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/syntax"
)

// TabWidth is how many columns apart the tab stops are.
const TabWidth = 4

// Clipboard is where cut and copied text goes.
type Clipboard interface {
	Read() (string, error)
	Write(string) error
}

// systemClipboard is the desktop's clipboard, with a private one standing in
// where there is none (a server, or a desktop without xclip/xsel): copy and
// paste then still work within tyr.
type systemClipboard struct{ fallback string }

func (c *systemClipboard) Read() (string, error) {
	if s, err := clipboard.ReadAll(); err == nil {
		return s, nil
	}
	return c.fallback, nil
}

func (c *systemClipboard) Write(s string) error {
	c.fallback = s
	_ = clipboard.WriteAll(s)
	return nil
}

// MemoryClipboard is a clipboard of its own, for tests.
type MemoryClipboard struct{ Text string }

func (c *MemoryClipboard) Read() (string, error) { return c.Text, nil }
func (c *MemoryClipboard) Write(s string) error  { c.Text = s; return nil }

// Model is one open file.
type Model struct {
	buf  buffer
	hist history

	cur    Pos
	want   int  // the column up and down aim for, kept across short lines
	anchor Pos  // the other end of the selection
	sel    bool // a selection is active

	top, left     int // first line and first column shown
	width, height int

	lang  syntax.Lang
	hl    []syntax.Highlighter // highlighter state at the start of each line, as far as valid
	color bool

	find finder
	jump jumper

	// Clipboard is where cut, copy and paste go; the system's by default.
	Clipboard Clipboard
}

// New makes an empty editor.
func New() Model {
	return Model{buf: newBuffer(""), color: true, Clipboard: &systemClipboard{}}
}

// SetText loads a file's contents, highlighted as lang, with the cursor at the
// top and nothing to undo.
func (m *Model) SetText(text string, lang syntax.Lang) {
	clip := m.Clipboard
	w, h := m.width, m.height
	*m = New()
	if clip != nil {
		m.Clipboard = clip
	}
	m.width, m.height = w, h
	m.buf = newBuffer(text)
	m.lang = lang
}

// SetLang changes the language the buffer is highlighted as — after a save
// under a new name, say — keeping the text and its undo history.
func (m *Model) SetLang(lang syntax.Lang) {
	m.lang, m.hl = lang, nil
}

// Text is the buffer as it would be saved.
func (m Model) Text() string { return m.buf.text() }

// Dirty reports whether the buffer differs from what was loaded or last saved.
func (m Model) Dirty() bool { return m.hist.current() != m.hist.saved }

// MarkSaved records that the buffer as it is now has been written.
func (m *Model) MarkSaved() {
	m.hist.saved = m.hist.current()
	m.hist.sealed = true
}

// SetSize sets the rectangle the editor draws into.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, 1), max(height, 1)
	m.scrollToCursor()
}

// Cursor is the cursor's line and column, counted from 1 as editors show them.
func (m Model) Cursor() (line, col int) { return m.cur.Row + 1, m.visualCol(m.cur) + 1 }

// Lines is how many lines the buffer has.
func (m Model) Lines() int { return len(m.buf.lines) }

// ---- changing the text ----

// change replaces from..to with text as one undoable step and puts the cursor
// after it.
func (m *Model) change(from, to Pos, text string, kind editKind) {
	from, to = order(m.buf.clamp(from), m.buf.clamp(to))
	old := m.buf.slice(from, to)
	if old == "" && text == "" {
		return
	}
	before := m.cur
	end := m.buf.replace(from, to, text)
	m.cur, m.sel = end, false
	m.hist.push(edit{At: from, Old: old, New: text, Before: before, After: end, kind: kind})
	m.touched(from.Row)
	m.settle()
}

// touched invalidates what was worked out about lines from row on.
func (m *Model) touched(row int) {
	if len(m.hl) > row {
		m.hl = m.hl[:row+1]
	}
}

// insert types text at the cursor, over the selection if there is one.
func (m *Model) insert(text string, kind editKind) {
	from, to := m.cur, m.cur
	if m.sel {
		from, to = order(m.anchor, m.cur)
		kind = editOther
	}
	m.change(from, to, text, kind)
}

// targetSpan is what a whole-text tool works on: the selection, or the whole
// buffer when there is none.
func (m Model) targetSpan() (from, to Pos, whole bool) {
	if m.sel && m.anchor != m.cur {
		from, to = order(m.anchor, m.cur)
		return from, to, false
	}
	return Pos{}, m.buf.end(), true
}

// Target is the text a whole-text tool — a JSON prettifier, say — works on:
// the selection, or everything when nothing is selected, which whole says.
// Lines are joined with \n whatever the file uses; ReplaceTarget puts the
// file's own line endings back.
func (m Model) Target() (text string, whole bool) {
	from, to, whole := m.targetSpan()
	return m.buf.slice(from, to), whole
}

// ReplaceTarget swaps the target for text as one undo step. A replaced
// selection stays selected, so what changed can be seen; after the whole
// buffer the cursor stays on its line. Text the same as the target is no
// change at all, and leaves nothing to undo.
func (m *Model) ReplaceTarget(text string) {
	from, to, whole := m.targetSpan()
	if m.buf.slice(from, to) == text {
		return
	}
	row := m.cur.Row
	m.change(from, to, text, editOther)
	if whole {
		m.cur = m.buf.clamp(Pos{row, 0})
	} else {
		m.anchor, m.sel = from, true
	}
	m.hist.undo[len(m.hist.undo)-1].After = m.cur // where redo puts it back
	m.settle()
}

// GoToTarget puts the cursor at line and column (from 1, the column in
// characters) counted within the target — where a tool said it found a
// problem.
func (m *Model) GoToTarget(line, col int) {
	from, _, _ := m.targetSpan()
	p := Pos{from.Row + line - 1, col - 1}
	if line == 1 {
		p.Col += from.Col
	}
	m.moveTo(p, false)
	m.settle()
}

// deleteSelection removes the selection, reporting whether there was one.
func (m *Model) deleteSelection() bool {
	if !m.sel || m.anchor == m.cur {
		m.sel = false
		return false
	}
	from, to := order(m.anchor, m.cur)
	m.change(from, to, "", editOther)
	return true
}

// Undo takes back the last change.
func (m *Model) Undo() {
	h := &m.hist
	if len(h.undo) == 0 {
		return
	}
	e := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	m.buf.replace(e.At, endOf(e.At, e.New), e.Old)
	h.redo = append(h.redo, e)
	h.sealed = true
	m.cur, m.sel = m.buf.clamp(e.Before), false
	m.touched(e.At.Row)
	m.settle()
}

// Redo puts back the last change undone.
func (m *Model) Redo() {
	h := &m.hist
	if len(h.redo) == 0 {
		return
	}
	e := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	m.buf.replace(e.At, endOf(e.At, e.Old), e.New)
	h.undo = append(h.undo, e)
	h.sealed = true
	m.cur, m.sel = m.buf.clamp(e.After), false
	m.touched(e.At.Row)
	m.settle()
}

// ---- moving ----

// settle keeps the cursor in the buffer and on screen, and remembers its
// column for the next move up or down.
func (m *Model) settle() {
	m.cur = m.buf.clamp(m.cur)
	m.want = m.visualCol(m.cur)
	m.scrollToCursor()
}

// moveTo puts the cursor at p, extending the selection when selecting.
func (m *Model) moveTo(p Pos, selecting bool) {
	if selecting && !m.sel {
		m.anchor, m.sel = m.cur, true
	} else if !selecting {
		m.sel = false
	}
	m.cur = m.buf.clamp(p)
}

// moveVertical moves n lines, aiming for the remembered column.
func (m *Model) moveVertical(n int, selecting bool) {
	want := m.want
	row := min(max(m.cur.Row+n, 0), len(m.buf.lines)-1)
	m.moveTo(Pos{row, m.colAt(row, want)}, selecting)
	m.want = want
	m.scrollToCursor()
}

// left and right are one character over, across line ends.
func (m *Model) leftOf(p Pos) Pos {
	if p.Col > 0 {
		return Pos{p.Row, p.Col - 1}
	}
	if p.Row > 0 {
		return Pos{p.Row - 1, len(m.buf.lines[p.Row-1])}
	}
	return p
}

func (m *Model) rightOf(p Pos) Pos {
	if p.Col < len(m.buf.lines[p.Row]) {
		return Pos{p.Row, p.Col + 1}
	}
	if p.Row < len(m.buf.lines)-1 {
		return Pos{p.Row + 1, 0}
	}
	return p
}

// wordLeft and wordRight skip to the start of the previous word and past the
// end of the next one.
func (m *Model) wordLeft(p Pos) Pos {
	if p.Col == 0 {
		return m.leftOf(p)
	}
	line := m.buf.lines[p.Row]
	c := p.Col
	for c > 0 && !isWord(line[c-1]) {
		c--
	}
	for c > 0 && isWord(line[c-1]) {
		c--
	}
	return Pos{p.Row, c}
}

func (m *Model) wordRight(p Pos) Pos {
	line := m.buf.lines[p.Row]
	if p.Col == len(line) {
		return m.rightOf(p)
	}
	c := p.Col
	for c < len(line) && !isWord(line[c]) {
		c++
	}
	for c < len(line) && isWord(line[c]) {
		c++
	}
	return Pos{p.Row, c}
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// home is the first non-blank character, or the start of the line when the
// cursor is already there.
func (m *Model) home(p Pos) Pos {
	line := m.buf.lines[p.Row]
	first := 0
	for first < len(line) && (line[first] == ' ' || line[first] == '\t') {
		first++
	}
	if p.Col == first {
		return Pos{p.Row, 0}
	}
	return Pos{p.Row, first}
}

// indentOf is the leading spaces and tabs of a line.
func (m *Model) indentOf(row int) string {
	line := m.buf.lines[row]
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return string(line[:n])
}

// ---- keys ----

// Update handles a key or a mouse event. It reports whether the editor used it:
// the keys it leaves alone (save, quit, close) are the app's.
func (m *Model) Update(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.find.active {
			return m.findKey(msg)
		}
		if m.jump.active {
			return m.jumpKey(msg)
		}
		return m.key(msg)
	case tea.MouseMsg:
		m.mouse(msg)
		return true
	}
	return false
}

func (m *Model) key(msg tea.KeyMsg) bool {
	if msg.Paste {
		m.insert(cleanPaste(string(msg.Runes)), editOther)
		return true
	}
	k := msg.String()
	shift := strings.Contains(k, "shift+")
	page := max(m.height-1, 1)

	switch k {
	// moving, and selecting with shift
	case "left", "shift+left":
		if m.sel && !shift {
			from, _ := order(m.anchor, m.cur)
			m.moveTo(from, false)
		} else {
			m.moveTo(m.leftOf(m.cur), shift)
		}
	case "right", "shift+right":
		if m.sel && !shift {
			_, to := order(m.anchor, m.cur)
			m.moveTo(to, false)
		} else {
			m.moveTo(m.rightOf(m.cur), shift)
		}
	case "up", "shift+up":
		m.moveVertical(-1, shift)
		return true
	case "down", "shift+down":
		m.moveVertical(1, shift)
		return true
	case "pgup", "shift+pgup":
		m.top = max(m.top-page, 0)
		m.moveVertical(-page, shift)
		return true
	case "pgdown", "shift+pgdown":
		m.top = min(m.top+page, max(len(m.buf.lines)-m.height, 0))
		m.moveVertical(page, shift)
		return true
	case "home", "shift+home":
		m.moveTo(m.home(m.cur), shift)
	case "end", "shift+end", "ctrl+e":
		m.moveTo(Pos{m.cur.Row, len(m.buf.lines[m.cur.Row])}, shift)
	case "ctrl+left", "alt+left", "alt+b", "ctrl+shift+left", "alt+shift+left":
		m.moveTo(m.wordLeft(m.cur), shift)
	case "ctrl+right", "alt+right", "alt+f", "ctrl+shift+right", "alt+shift+right":
		m.moveTo(m.wordRight(m.cur), shift)
	case "ctrl+home", "ctrl+shift+home":
		m.moveTo(Pos{}, shift)
	case "ctrl+end", "ctrl+shift+end":
		m.moveTo(m.buf.end(), shift)

	// typing
	case "enter":
		m.insert("\n"+m.indentOf(m.cur.Row), editOther)
		return true
	case "tab":
		if m.sel && m.anchor.Row != m.cur.Row {
			m.indent(true)
			return true
		}
		m.insert("\t", editType)
		return true
	case "shift+tab":
		m.indent(false)
		return true
	case "backspace", "ctrl+h":
		if !m.deleteSelection() {
			m.change(m.leftOf(m.cur), m.cur, "", editBackspace)
		}
		return true
	case "delete", "ctrl+d":
		if !m.deleteSelection() {
			m.change(m.cur, m.rightOf(m.cur), "", editOther)
		}
		return true
	case "alt+backspace", "ctrl+w":
		if !m.deleteSelection() {
			m.change(m.wordLeft(m.cur), m.cur, "", editOther)
		}
		return true

	// clipboard and history
	case "ctrl+c":
		if m.sel {
			_ = m.Clipboard.Write(m.selection())
		}
		return true
	case "ctrl+x":
		if m.sel {
			_ = m.Clipboard.Write(m.selection())
			m.deleteSelection()
		}
		return true
	case "ctrl+k":
		m.cutLine()
		return true
	case "ctrl+v":
		if s, err := m.Clipboard.Read(); err == nil && s != "" {
			m.insert(cleanPaste(s), editOther)
		}
		return true
	case "ctrl+z":
		m.Undo()
		return true
	case "ctrl+y", "ctrl+shift+z":
		m.Redo()
		return true
	case "ctrl+a":
		m.SelectAll()
		return true

	// finding
	case "ctrl+f":
		m.openFind()
		return true
	case "f3", "ctrl+g":
		m.findNext(1)
		return true
	case "shift+f3":
		m.findNext(-1)
		return true
	case "ctrl+r":
		m.openReplace()
		return true
	case "ctrl+l":
		m.openJump()
		return true
	case "alt+up":
		m.moveLines(-1)
		return true
	case "alt+down":
		m.moveLines(1)
		return true
	case "alt+shift+up":
		m.duplicateLines(-1)
		return true
	case "alt+shift+down":
		m.duplicateLines(1)
		return true

	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.insert(string(msg.Runes), editType)
			return true
		}
		return false
	}
	m.want = m.visualCol(m.cur)
	m.scrollToCursor()
	return true
}

// selection is the selected text.
func (m *Model) selection() string {
	from, to := order(m.anchor, m.cur)
	return m.buf.slice(from, to)
}

// SelectAll selects the whole buffer.
func (m *Model) SelectAll() {
	m.anchor, m.cur, m.sel = Pos{}, m.buf.end(), true
	m.scrollToCursor()
}

// cutLine cuts the cursor's line, line break and all, to the clipboard.
func (m *Model) cutLine() {
	row := m.cur.Row
	from, to := Pos{row, 0}, Pos{row + 1, 0}
	if row == len(m.buf.lines)-1 {
		if row == 0 {
			to = Pos{0, len(m.buf.lines[0])}
		} else {
			from = Pos{row - 1, len(m.buf.lines[row-1])}
			to = Pos{row, len(m.buf.lines[row])}
		}
	}
	_ = m.Clipboard.Write(string(m.buf.lines[row]) + "\n")
	m.sel = false
	m.change(from, to, "", editOther)
	m.cur = m.buf.clamp(Pos{min(row, len(m.buf.lines)-1), 0})
	m.settle()
}

// indent adds a tab to, or takes one level of indentation off, every line the
// selection touches (or the cursor's line), as one undoable step.
func (m *Model) indent(in bool) {
	first, last := m.selectedRows()
	var lines []string
	for r := first; r <= last; r++ {
		line := string(m.buf.lines[r])
		switch {
		case in && line != "":
			line = "\t" + line
		case !in && strings.HasPrefix(line, "\t"):
			line = line[1:]
		case !in:
			n := 0
			for n < TabWidth && n < len(line) && line[n] == ' ' {
				n++
			}
			line = line[n:]
		}
		lines = append(lines, line)
	}
	text := strings.Join(lines, "\n")
	from, to := Pos{first, 0}, Pos{last, len(m.buf.lines[last])}
	if m.buf.slice(from, to) == text {
		return
	}
	m.change(from, to, text, editOther)
	m.anchor, m.cur, m.sel = Pos{first, 0}, Pos{last, len(m.buf.lines[last])}, true
	m.settle()
}

// cleanPaste turns a terminal's or a clipboard's line breaks into the buffer's.
func cleanPaste(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// ---- mouse ----

func (m *Model) mouse(msg tea.MouseMsg) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.top = max(m.top-3, 0)
		return
	case tea.MouseButtonWheelDown:
		m.top = min(m.top+3, max(len(m.buf.lines)-m.height, 0))
		return
	case tea.MouseButtonLeft:
	default:
		return
	}
	row := m.top + msg.Y
	if row >= len(m.buf.lines) {
		row = len(m.buf.lines) - 1
	}
	if row < 0 {
		return
	}
	p := Pos{row, m.colAt(row, m.left+msg.X-m.gutterWidth())}
	switch msg.Action {
	case tea.MouseActionPress:
		m.moveTo(p, false)
	case tea.MouseActionMotion:
		m.moveTo(p, true)
	default:
		return
	}
	m.want = m.visualCol(m.cur)
	m.scrollToCursor()
}
