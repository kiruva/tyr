package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/syntax"
	"github.com/kiruva/tyr/internal/ui"
)

// The viewer is a pager with the four things a pager is asked for: find a
// string, wrap or don't, number the lines or don't, and show me the bytes when
// the file is not text. Everything it draws is built from the bytes it was
// handed, so every toggle is a re-render rather than a re-read — including the
// hex view, which is why a binary file opens here instead of being refused.

// hexBytesPerLine is the classic sixteen; the dump is laid out around it.
const hexBytesPerLine = 16

// viewerState is one open file (or one command's output).
type viewerState struct {
	title string
	raw   []byte
	lines []string
	lang  syntax.Lang

	hex     bool
	wrap    bool
	numbers bool
	colour  bool
	binary  bool

	// search
	input     textinput.Model
	prompting bool
	query     string
	matches   []int // indexes into the rendered lines
	current   int   // which match the cursor is on
	status    string
}

// showBytes puts content in the viewer. A file that is not text opens as hex
// rather than being turned away: "not a text file" is a fact about the file,
// not a reason to refuse to look at it.
func (m *Model) showBytes(title string, data []byte, lang syntax.Lang) {
	binary := isBinary(data)

	m.viewer = viewerState{
		title:   title,
		raw:     data,
		lines:   splitLines(data),
		lang:    lang,
		hex:     binary,
		binary:  binary,
		numbers: true,
		colour:  true,
	}
	m.viewTitle = title
	m.refreshViewer()
	m.viewport.GotoTop()
	m.mode = modeView
}

// openViewer loads the highlighted file into the pager.
func (m *Model) openViewer() tea.Cmd {
	data, title, err := m.loadCurrent()
	if err != nil {
		m.errText = err.Error()
		return nil
	}
	m.showBytes(title, data, syntax.Detect(title))
	return nil
}

// onViewKey drives the pager.
func (m Model) onViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.viewer.prompting {
		return m.onViewSearchKey(msg)
	}

	switch msg.String() {
	case "q", "esc":
		if m.viewer.query != "" {
			m.clearViewSearch()
			return m, nil
		}
		m.mode = modeNormal
		m.viewer = viewerState{}
		return m, nil
	case "e":
		cmd := m.openEditor() // hand off to the editor on the same file
		return m, cmd
	case "/":
		return m.openViewSearch()
	case "n":
		m.stepMatch(1)
		return m, nil
	case "N":
		m.stepMatch(-1)
		return m, nil
	case "w":
		m.viewer.wrap = !m.viewer.wrap
		m.viewer.status = "wrap " + onOff2(m.viewer.wrap)
		m.refreshViewer()
		return m, nil
	case "#":
		m.viewer.numbers = !m.viewer.numbers
		m.viewer.status = "line numbers " + onOff2(m.viewer.numbers)
		m.refreshViewer()
		return m, nil
	case "x":
		m.viewer.hex = !m.viewer.hex
		m.viewer.status = "hex " + onOff2(m.viewer.hex)
		m.refreshViewer()
		return m, nil
	case "s":
		m.viewer.colour = !m.viewer.colour
		m.viewer.status = "highlighting " + onOff2(m.viewer.colour)
		m.refreshViewer()
		return m, nil
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// openViewSearch puts the cursor in the search field.
func (m Model) openViewSearch() (tea.Model, tea.Cmd) {
	ti := newConnInput("text to find", false, 30)
	ti.SetValue(m.viewer.query)
	ti.CursorEnd()

	m.viewer.input = ti
	m.viewer.prompting = true
	m.viewer.status = ""
	return m, m.viewer.input.Focus()
}

// onViewSearchKey drives the search field, searching as it is typed.
func (m Model) onViewSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.viewer.prompting = false
		m.viewer.input.Blur()
		m.jumpToMatch(m.viewer.current)
		return m, nil
	case "esc":
		m.viewer.prompting = false
		m.viewer.input.Blur()
		m.clearViewSearch()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.viewer.input, cmd = m.viewer.input.Update(msg)
	m.viewer.query = m.viewer.input.Value()
	m.findMatches()
	m.refreshViewer()
	m.jumpToMatch(m.viewer.current)
	return m, cmd
}

// clearViewSearch drops the query and its highlighting.
func (m *Model) clearViewSearch() {
	m.viewer.query = ""
	m.viewer.matches = nil
	m.viewer.current = 0
	m.viewer.status = ""
	m.refreshViewer()
}

// findMatches records which rendered lines contain the query.
func (m *Model) findMatches() {
	m.viewer.matches = nil
	m.viewer.current = 0
	if m.viewer.query == "" {
		return
	}

	needle := strings.ToLower(m.viewer.query)
	for i, line := range m.viewerLines() {
		if strings.Contains(strings.ToLower(line), needle) {
			m.viewer.matches = append(m.viewer.matches, i)
		}
	}
	if len(m.viewer.matches) == 0 {
		m.viewer.status = "no match for " + m.viewer.query
	}
}

// stepMatch moves to the next or previous hit, wrapping around the file.
func (m *Model) stepMatch(step int) {
	if len(m.viewer.matches) == 0 {
		if m.viewer.query != "" {
			m.viewer.status = "no match for " + m.viewer.query
		}
		return
	}
	n := len(m.viewer.matches)
	m.viewer.current = (m.viewer.current + step + n) % n
	m.refreshViewer()
	m.jumpToMatch(m.viewer.current)
}

// jumpToMatch scrolls a hit into the middle of the window.
func (m *Model) jumpToMatch(index int) {
	if index < 0 || index >= len(m.viewer.matches) {
		return
	}
	line := m.viewer.matches[index]
	m.viewport.SetYOffset(max(line-m.viewport.Height/2, 0))
	m.viewer.status = fmt.Sprintf("match %d of %d", index+1, len(m.viewer.matches))
}

// refreshViewer rebuilds what the pager shows from the bytes it holds, keeping
// the scroll position so a toggle does not lose the reader's place.
func (m *Model) refreshViewer() {
	offset := m.viewport.YOffset
	m.viewport.SetContent(strings.Join(m.renderViewerLines(), "\n"))
	m.viewport.SetYOffset(offset)
}

// viewerLines is the text the viewer is searching and displaying, before any
// styling: the hex dump when it is in hex, the file's lines otherwise.
func (m Model) viewerLines() []string {
	if m.viewer.hex {
		return hexDump(m.viewer.raw)
	}
	if !m.viewer.wrap {
		return m.viewer.lines
	}
	return wrapLines(m.viewer.lines, m.textWidth())
}

// renderViewerLines is viewerLines with the numbers, the highlighting and the
// search marks put on.
func (m Model) renderViewerLines() []string {
	lines := m.viewerLines()
	out := make([]string, 0, len(lines))

	highlighted := m.highlight(lines)
	currentLine := -1
	if len(m.viewer.matches) > 0 && m.viewer.current < len(m.viewer.matches) {
		currentLine = m.viewer.matches[m.viewer.current]
	}

	width := numberWidth(len(lines))
	for i, plain := range lines {
		body := highlighted[i]
		if m.viewer.query != "" {
			// A hit is marked on the plain text: colouring inside already
			// coloured text produces neither colour reliably.
			if marked, ok := markMatches(plain, m.viewer.query, i == currentLine); ok {
				body = marked
			}
		}
		// The hex dump numbers itself, in bytes, which is the number that
		// means something there.
		if m.viewer.numbers && !m.viewer.hex {
			body = ui.Gutter.Render(fmt.Sprintf("%*d │ ", width, i+1)) + body
		}
		out = append(out, body)
	}
	return out
}

// highlight colours the lines, or hands them back untouched when highlighting
// is off, the file is hex, or the language is not one it knows.
func (m Model) highlight(lines []string) []string {
	if !m.viewer.colour || m.viewer.hex || m.viewer.lang == syntax.None {
		return lines
	}

	h := syntax.New(m.viewer.lang)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		var b strings.Builder
		for _, span := range h.Line(line) {
			b.WriteString(styleFor(span.Kind).Render(span.Text))
		}
		out = append(out, b.String())
	}
	return out
}

// styleFor maps a highlighted span onto the theme.
func styleFor(kind syntax.Kind) lipgloss.Style {
	switch kind {
	case syntax.Keyword:
		return ui.SyntaxKeyword
	case syntax.Str:
		return ui.SyntaxString
	case syntax.Comment:
		return ui.SyntaxComment
	case syntax.Number:
		return ui.SyntaxNumber
	case syntax.Heading:
		return ui.SyntaxHeading
	default:
		return lipgloss.NewStyle()
	}
}

// markMatches highlights every occurrence of the query in one line.
func markMatches(line, query string, current bool) (string, bool) {
	style := ui.Match
	if current {
		style = ui.MatchCurrent
	}

	lower, needle := strings.ToLower(line), strings.ToLower(query)
	if !strings.Contains(lower, needle) {
		return line, false
	}

	var b strings.Builder
	for {
		at := strings.Index(lower, needle)
		if at < 0 {
			b.WriteString(line)
			return b.String(), true
		}
		b.WriteString(line[:at])
		b.WriteString(style.Render(line[at : at+len(needle)]))
		line, lower = line[at+len(needle):], lower[at+len(needle):]
	}
}

// textWidth is how much room a line of the file has, after the gutter.
func (m Model) textWidth() int {
	width := m.width
	if m.viewer.numbers {
		width -= numberWidth(len(m.viewer.lines)) + 3
	}
	return max(width, 20)
}

// numberWidth is how many digits the line numbers need.
func numberWidth(lines int) int {
	width := 1
	for n := lines; n >= 10; n /= 10 {
		width++
	}
	return width
}

// splitLines breaks a file into display lines, without a trailing empty one for
// the newline every text file ends with.
func splitLines(data []byte) []string {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}

// wrapLines breaks long lines at the window's width. It wraps on the character
// rather than the word: a viewer is used on code and logs, where a column is a
// meaningful thing and a reflowed one is not.
func wrapLines(lines []string, width int) []string {
	if width < 8 {
		return lines
	}

	out := make([]string, 0, len(lines))
	for _, line := range lines {
		for utf8.RuneCountInString(line) > width {
			runes := []rune(line)
			out = append(out, string(runes[:width]))
			line = string(runes[width:])
		}
		out = append(out, line)
	}
	return out
}

// hexDump renders the classic three columns: offset, bytes, and the printable
// characters they stand for.
func hexDump(data []byte) []string {
	if len(data) == 0 {
		return []string{"(empty)"}
	}

	lines := make([]string, 0, len(data)/hexBytesPerLine+1)
	for start := 0; start < len(data); start += hexBytesPerLine {
		end := min(start+hexBytesPerLine, len(data))
		chunk := data[start:end]

		var hex, text strings.Builder
		for i := 0; i < hexBytesPerLine; i++ {
			if i == hexBytesPerLine/2 {
				hex.WriteByte(' ')
			}
			if i < len(chunk) {
				fmt.Fprintf(&hex, "%02x ", chunk[i])
				text.WriteByte(printable(chunk[i]))
				continue
			}
			hex.WriteString("   ")
		}
		lines = append(lines, fmt.Sprintf("%08x  %s |%s|", start, hex.String(), text.String()))
	}
	return lines
}

// printable is the character a byte stands for in the hex dump's right column.
func printable(b byte) byte {
	if b >= 0x20 && b < 0x7f {
		return b
	}
	return '.'
}

func onOff2(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
