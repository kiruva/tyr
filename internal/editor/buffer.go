// Package editor is tyr's text editor: a buffer of lines, a cursor, a
// selection, undo, find and syntax colouring, drawn to fit a rectangle of the
// terminal. It is built for quick fixes to code and config, which is why it
// keeps a file exactly as it found it — tabs stay tabs (drawn out to the next
// tab stop), a CRLF file stays CRLF, and any other control character is shown
// as ^X and saved as itself.
//
// It replaces bubbles' textarea, which cleans up what it is given and cannot be
// told not to: tabs became four spaces, \r a line break of its own, other
// control characters vanished, and it held 10000 lines at most. Saved back,
// that broke Makefiles and rewrote every line of a CRLF file.
package editor

import (
	"strings"
)

// Pos is a place in the buffer: a line, and a rune index within it. Col may
// equal the line's length, meaning the end of the line.
type Pos struct{ Row, Col int }

// Before reports whether p comes before q.
func (p Pos) Before(q Pos) bool {
	return p.Row < q.Row || (p.Row == q.Row && p.Col < q.Col)
}

// order returns p and q, earlier first.
func order(p, q Pos) (Pos, Pos) {
	if q.Before(p) {
		return q, p
	}
	return p, q
}

// buffer is the text: one rune slice per line, with no line endings in them.
// A file ending in a newline has an empty last line, so joining the lines back
// with the line ending gives the file again, byte for byte.
type buffer struct {
	lines [][]rune
	crlf  bool // every line ended in \r\n: the \r is dropped here and put back on save
}

// newBuffer splits text into lines. Only a file whose every line ending is
// \r\n is read as CRLF; in any other file a \r is an ordinary character, shown
// and kept where it is.
func newBuffer(text string) buffer {
	b := buffer{}
	parts := strings.Split(text, "\n")
	if len(parts) > 1 {
		b.crlf = true
		for _, p := range parts[:len(parts)-1] {
			if !strings.HasSuffix(p, "\r") {
				b.crlf = false
				break
			}
		}
	}
	b.lines = make([][]rune, len(parts))
	for i, p := range parts {
		if b.crlf && i < len(parts)-1 {
			p = strings.TrimSuffix(p, "\r")
		}
		b.lines[i] = []rune(p)
	}
	return b
}

// text is the buffer as a file.
func (b *buffer) text() string {
	eol := "\n"
	if b.crlf {
		eol = "\r\n"
	}
	var sb strings.Builder
	for i, l := range b.lines {
		if i > 0 {
			sb.WriteString(eol)
		}
		sb.WriteString(string(l))
	}
	return sb.String()
}

// clamp moves p onto the nearest place that exists.
func (b *buffer) clamp(p Pos) Pos {
	p.Row = min(max(p.Row, 0), len(b.lines)-1)
	p.Col = min(max(p.Col, 0), len(b.lines[p.Row]))
	return p
}

// end is the position after the last character.
func (b *buffer) end() Pos {
	last := len(b.lines) - 1
	return Pos{last, len(b.lines[last])}
}

// slice is the text between two positions, lines joined with \n.
func (b *buffer) slice(from, to Pos) string {
	from, to = order(b.clamp(from), b.clamp(to))
	if from.Row == to.Row {
		return string(b.lines[from.Row][from.Col:to.Col])
	}
	var sb strings.Builder
	sb.WriteString(string(b.lines[from.Row][from.Col:]))
	for r := from.Row + 1; r < to.Row; r++ {
		sb.WriteByte('\n')
		sb.WriteString(string(b.lines[r]))
	}
	sb.WriteByte('\n')
	sb.WriteString(string(b.lines[to.Row][:to.Col]))
	return sb.String()
}

// replace swaps the text between from and to for text (lines split on \n) and
// returns where the new text ends. Every change to the buffer goes through
// here, which is what makes each one undoable.
func (b *buffer) replace(from, to Pos, text string) Pos {
	from, to = order(b.clamp(from), b.clamp(to))
	head := b.lines[from.Row][:from.Col]
	tail := b.lines[to.Row][to.Col:]

	parts := strings.Split(text, "\n")
	repl := make([][]rune, len(parts))
	for i, p := range parts {
		repl[i] = []rune(p)
	}
	last := len(repl) - 1
	endPos := Pos{from.Row + last, len(repl[last])}
	if last == 0 {
		endPos.Col += len(head)
	}

	first := make([]rune, 0, len(head)+len(repl[0]))
	first = append(append(first, head...), repl[0]...)
	repl[0] = first
	repl[last] = append(repl[last], tail...)

	lines := make([][]rune, 0, len(b.lines)-(to.Row-from.Row)+last)
	lines = append(lines, b.lines[:from.Row]...)
	lines = append(lines, repl...)
	lines = append(lines, b.lines[to.Row+1:]...)
	b.lines = lines
	return endPos
}

// endOf is where text would end if it were inserted at p.
func endOf(p Pos, text string) Pos {
	n := strings.Count(text, "\n")
	if n == 0 {
		return Pos{p.Row, p.Col + len([]rune(text))}
	}
	return Pos{p.Row + n, len([]rune(text[strings.LastIndexByte(text, '\n')+1:]))}
}

// edit is one undoable change: at At, Old was replaced by New. The cursor
// positions on either side are kept so undo and redo put it back too.
type edit struct {
	At       Pos
	Old, New string
	Before   Pos // cursor before the change
	After    Pos // cursor after it
	kind     editKind
	id       int
}

// editKind says which edits may be merged into the one before: typing a word
// is one undo step, not one per letter.
type editKind int

const (
	editOther editKind = iota
	editType
	editBackspace
)

// history is the undo and redo stacks.
type history struct {
	undo, redo []edit
	nextID     int
	saved      int  // id of the edit on top of undo when last saved (0: none)
	sealed     bool // the top of undo may not be merged into (it was saved)
}

// current is the id of the state the buffer is in.
func (h *history) current() int {
	if len(h.undo) == 0 {
		return 0
	}
	return h.undo[len(h.undo)-1].id
}

// push records e, merging it into the edit before when it continues it.
func (h *history) push(e edit) {
	h.redo = nil
	if n := len(h.undo); n > 0 && !h.sealed && e.kind != editOther {
		top := &h.undo[n-1]
		switch {
		case e.kind == editType && top.kind == editType && top.Old == "" && e.Old == "" &&
			!strings.Contains(e.New, "\n") && endOf(top.At, top.New) == e.At:
			top.New += e.New
			top.After = e.After
			return
		case e.kind == editBackspace && top.kind == editBackspace && top.New == "" && e.New == "" &&
			!strings.Contains(e.Old, "\n") && e.At.Row == top.At.Row && e.At.Col+len([]rune(e.Old)) == top.At.Col:
			top.Old = e.Old + top.Old
			top.At = e.At
			top.After = e.After
			return
		}
	}
	h.sealed = false
	h.nextID++
	e.id = h.nextID
	h.undo = append(h.undo, e)
}
