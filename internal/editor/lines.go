package editor

import "strings"

// selectedRows is the lines the selection touches, or the cursor's line. A
// selection ending at the start of a line does not take that line.
func (m *Model) selectedRows() (first, last int) {
	first, last = m.cur.Row, m.cur.Row
	if m.sel {
		from, to := order(m.anchor, m.cur)
		first, last = from.Row, to.Row
		if to.Col == 0 && to.Row > from.Row {
			last--
		}
	}
	return first, last
}

// rows is the text of lines first..last, joined with \n.
func (m *Model) rows(first, last int) string {
	return m.buf.slice(Pos{first, 0}, Pos{last, len(m.buf.lines[last])})
}

// moveLines moves the selected lines (or the cursor's) one line up (dir -1) or
// down (dir 1), past the line beside them, as one undo step. The cursor and
// the selection go with them.
func (m *Model) moveLines(dir int) {
	first, last := m.selectedRows()
	if (dir < 0 && first == 0) || (dir > 0 && last == len(m.buf.lines)-1) {
		return
	}
	block := m.rows(first, last)
	var from, to int
	var text string
	if dir < 0 {
		from, to = first-1, last
		text = block + "\n" + m.rows(from, from)
	} else {
		from, to = first, last+1
		text = m.rows(to, to) + "\n" + block
	}
	cur, anchor, sel := m.cur, m.anchor, m.sel
	m.change(Pos{from, 0}, Pos{to, len(m.buf.lines[to])}, text, editOther)
	m.restore(cur, anchor, sel, dir)
}

// duplicateLines copies the selected lines (or the cursor's) below them. Going
// down (dir 1) the cursor moves onto the copy; going up it stays, so the copy
// is above it.
func (m *Model) duplicateLines(dir int) {
	first, last := m.selectedRows()
	block := m.rows(first, last)
	shift := 0
	if dir > 0 {
		shift = last - first + 1
	}
	cur, anchor, sel := m.cur, m.anchor, m.sel
	end := Pos{last, len(m.buf.lines[last])}
	m.change(end, end, "\n"+block, editOther)
	m.restore(cur, anchor, sel, shift)
}

// restore puts the cursor and selection back after a line operation, shift
// lines further on, and makes that where redo leaves the cursor too.
func (m *Model) restore(cur, anchor Pos, sel bool, shift int) {
	m.cur = m.buf.clamp(Pos{cur.Row + shift, cur.Col})
	m.anchor = m.buf.clamp(Pos{anchor.Row + shift, anchor.Col})
	m.sel = sel
	m.hist.undo[len(m.hist.undo)-1].After = m.cur
	m.settle()
}

// ToggleComment comments out the selected lines (or the cursor's) with the
// language's marker, or uncomments them when every one that is not blank is
// commented already, as one undo step. A line marker goes at the lines'
// common indent; a pair (<!-- -->, /* */) wraps each line at its own. It
// reports false, changing nothing, for a language with no comments.
func (m *Model) ToggleComment() bool {
	marker, open, close := m.lang.Comment()
	if marker == "" && open == "" {
		return false
	}
	first, last := m.selectedRows()
	lines := strings.Split(m.rows(first, last), "\n")

	commented, indent := true, -1
	for _, line := range lines {
		text := strings.TrimLeft(line, " \t")
		if text == "" {
			continue
		}
		if lead := leadRunes(line); indent < 0 || lead < indent {
			indent = lead
		}
		if marker != "" {
			commented = commented && strings.HasPrefix(text, marker)
		} else {
			commented = commented && len(text) >= len(open)+len(close) &&
				strings.HasPrefix(text, open) && strings.HasSuffix(text, close)
		}
	}
	if indent < 0 {
		return true // only blank lines: nothing to do
	}

	curOld := len(m.buf.lines[m.cur.Row])
	for i, line := range lines {
		text := strings.TrimLeft(line, " \t")
		if text == "" {
			continue
		}
		lead := line[:len(line)-len(text)]
		switch {
		case commented && marker != "":
			lines[i] = lead + strings.TrimPrefix(text[len(marker):], " ")
		case commented:
			inner := text[len(open) : len(text)-len(close)]
			lines[i] = lead + strings.TrimSuffix(strings.TrimPrefix(inner, " "), " ")
		case marker != "":
			r := []rune(line)
			lines[i] = string(r[:indent]) + marker + " " + string(r[indent:])
		default:
			lines[i] = lead + open + " " + text + " " + close
		}
	}

	cur, sel := m.cur, m.sel
	at := leadRunes(string(m.buf.lines[cur.Row])) // where the cursor's line changed
	if marker != "" && !commented {
		at = indent
	}
	m.change(Pos{first, 0}, Pos{last, len(m.buf.lines[last])}, strings.Join(lines, "\n"), editOther)
	if sel {
		m.anchor, m.cur, m.sel = Pos{first, 0}, Pos{last, len(m.buf.lines[last])}, true
	} else {
		if cur.Col >= at {
			cur.Col = max(cur.Col+len(m.buf.lines[cur.Row])-curOld, at)
		}
		m.cur = m.buf.clamp(cur)
	}
	m.hist.undo[len(m.hist.undo)-1].After = m.cur
	m.settle()
	return true
}

// leadRunes is how many spaces and tabs line starts with.
func leadRunes(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}
