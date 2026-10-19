// Package termsafe keeps text that tyr did not write from talking to the
// terminal. File names, archive members, remote listings, file contents and
// error messages all come from somewhere else, and any of them can carry an
// escape sequence: OSC 52 writes the clipboard, OSC 0 retitles the window, a
// cursor movement repaints a line with something it never said. A file name is
// enough to deliver one, so nothing a pane shows can be trusted to be inert.
//
// There are two layers, and they do different jobs. Line and Text are for the
// places that show such text: they turn every control character into a visible
// escape, so a name with an ESC in it looks like a name with an ESC in it, and
// is measured at the width it will actually be drawn. Frame is the backstop on
// the whole screen: it lets through the colour sequences tyr's own styling is
// made of and drops every other sequence and control character, so a path that
// forgot to call Line still cannot reach the terminal with a command.
package termsafe

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Line makes untrusted text safe to show on one line: newlines, tabs, every
// other control character, bidi overrides and the Unicode line and paragraph
// separators are written out as escapes (\n, \t, \u001b), and invalid UTF-8
// becomes U+FFFD.
func Line(s string) string { return escape(s, false) }

// Text is Line for text laid out over many lines, a file in the viewer say:
// newlines and tabs are left alone, since they are the layout, and everything
// else Line escapes is escaped.
func Text(s string) string { return escape(s, true) }

func escape(s string, layout bool) string {
	if !needsEscape(s, layout) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s) + 8)
	for _, r := range strings.ToValidUTF8(s, "\ufffd") {
		switch {
		case layout && (r == '\n' || r == '\t'):
			sb.WriteRune(r)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\t':
			sb.WriteString(`\t`)
		case unsafeRune(r):
			fmt.Fprintf(&sb, `\u%04x`, r)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// needsEscape is the fast path: almost every name and line is plain, and
// should not be copied just to find that out.
func needsEscape(s string, layout bool) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if (r == '\n' || r == '\t') && layout {
			continue
		}
		if unsafeRune(r) {
			return true
		}
	}
	return false
}

// unsafeRune is a rune a terminal may act on rather than draw, or one that
// reorders or breaks what is drawn around it: C0 and C1 controls, DEL, the
// bidi controls, and U+2028/U+2029.
func unsafeRune(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) ||
		r == '\u2028' || r == '\u2029'
}

// Frame filters a rendered screen down to what tyr itself puts there: printable
// text, newlines and tabs, and SGR sequences (ESC [ … m — colour, bold,
// reverse). Every other escape sequence is removed whole, string sequences
// (OSC, DCS, APC, PM, SOS) included, and so is every other control character.
//
// Removed rather than escaped: the layout was measured by lipgloss, which counts
// an escape sequence as zero cells wide, so dropping one keeps every line the
// width it was laid out at. Line is where a sequence is made visible instead.
func Frame(s string) string {
	if !frameNeedsWork(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			n, keep := sequence(s[i:])
			if keep {
				b.WriteString(s[i : i+n])
			}
			i += n
			continue
		case c == '\n' || c == '\t':
			b.WriteByte(c)
		case c < 0x20 || c == 0x7f:
			// dropped: BEL, BS, CR and the rest can all rewrite what is shown
		case c < utf8.RuneSelf:
			b.WriteByte(c)
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			switch {
			case r == utf8.RuneError && size == 1:
				b.WriteRune(utf8.RuneError) // a lone 0x9b is CSI to some terminals
			case unsafeRune(r):
				// dropped: C1 controls, bidi overrides, line separators
			default:
				b.WriteString(s[i : i+size])
			}
			i += size
			continue
		}
		i++
	}
	return b.String()
}

// frameNeedsWork reports whether Frame would change anything other than
// keeping SGR sequences, so a frame with only tyr's own styling in it — which
// is nearly every frame — is passed through without being copied.
func frameNeedsWork(s string) bool {
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			n, keep := sequence(s[i:])
			if !keep {
				return true
			}
			i += n
			continue
		case c == '\n' || c == '\t':
		case c < 0x20 || c == 0x7f:
			return true
		case c >= utf8.RuneSelf:
			r, size := utf8.DecodeRuneInString(s[i:])
			if (r == utf8.RuneError && size == 1) || unsafeRune(r) {
				return true
			}
			i += size
			continue
		}
		i++
	}
	return false
}

// sequence measures the escape sequence at the start of s, which begins with
// ESC, and reports whether it is an SGR sequence worth keeping. The grammar is
// ECMA-48's: CSI is ESC [ then parameter bytes, intermediate bytes and one
// final byte; the string sequences run to BEL or ST; anything else is ESC,
// optional intermediates and a final byte.
func sequence(s string) (n int, sgr bool) {
	if len(s) < 2 {
		return len(s), false
	}
	switch s[1] {
	case '[':
		j := 2
		for j < len(s) && s[j] >= 0x30 && s[j] <= 0x3f {
			j++
		}
		params := s[2:j]
		k := j
		for k < len(s) && s[k] >= 0x20 && s[k] <= 0x2f {
			k++
		}
		if k >= len(s) || s[k] < 0x40 || s[k] > 0x7e {
			// Unterminated or malformed: drop what was read, and let the byte
			// that broke it be judged on its own.
			return k, false
		}
		return k + 1, s[k] == 'm' && k == j && sgrParams(params)
	case ']', 'P', 'X', '^', '_':
		return stringSequence(s), false
	default:
		j := 1
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
			j++
		}
		if j < len(s) && s[j] >= 0x30 && s[j] <= 0x7e {
			return j + 1, false
		}
		return 1, false // a bare ESC
	}
}

// stringSequence measures an OSC, DCS, APC, PM or SOS: it runs to BEL, to ST
// (ESC \, or the C1 form U+009C), or — when a hostile name was cut short by
// truncation and lost its terminator — to the end of the line. Stopping at the
// newline is safe: the opening ESC is dropped, so the terminal never starts the
// string that the rest of the line would have been swallowed into.
func stringSequence(s string) int {
	for j := 2; j < len(s); j++ {
		switch {
		case s[j] == 0x07:
			return j + 1
		case s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\':
			return j + 2
		case s[j] == 0xc2 && j+1 < len(s) && s[j+1] == 0x9c:
			return j + 2
		case s[j] == '\n':
			return j
		}
	}
	return len(s)
}

// sgrParams reports whether a CSI's parameters are the plain digits and
// separators SGR uses. A private marker (<, =, >, ?) makes it something else.
func sgrParams(p string) bool {
	for i := 0; i < len(p); i++ {
		c := p[i]
		if (c < '0' || c > '9') && c != ';' && c != ':' {
			return false
		}
	}
	return true
}
