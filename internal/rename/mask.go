package rename

import (
	"strconv"
	"strings"
	"time"
)

// A mask is literal text with [tokens] in it. Tokens are resolved per candidate:
//
//	[N]      the name without its extension        [N2]    its 2nd character
//	[N2-5]   characters 2 to 5                     [N2-]   from the 2nd on
//	[E]      the extension, without the dot        [E1-3]  sliced the same way
//	[C]      the counter                           [C3]    zero-padded to 3
//	[P]      the name of the containing directory
//	[d]      the file's date (2006-01-02)          [t]     its time (15-04-05)
//
// An unknown token is left as typed, so a literal "[" in a name survives.

// maskInput is everything a mask can read about one candidate.
type maskInput struct {
	stem    string // name without extension
	ext     string // extension without the dot
	parent  string // containing directory's name
	counter int
	mod     time.Time
}

// expand resolves every token in mask against in.
func expand(mask string, in maskInput) string { return expandWith(mask, in, nil) }

// expandReplacement resolves the tokens in a replacement template. In the modes
// backed by a regexp the template also carries `$1` backreferences, so a value
// that happens to contain a `$` — a filename may — is escaped on the way in;
// otherwise it would be read as a group reference of its own.
func expandReplacement(mask string, in maskInput, mode Mode) string {
	if mode == ModeLiteral {
		return expand(mask, in)
	}
	return expandWith(mask, in, escapeDollar)
}

func escapeDollar(s string) string { return strings.ReplaceAll(s, "$", "$$") }

// expandWith is expand with an optional transform over each resolved value. The
// transform sees only what a token produced, never the literal text around it,
// so escaping cannot disturb what the user typed.
func expandWith(mask string, in maskInput, transform func(string) string) string {
	var b strings.Builder
	for i := 0; i < len(mask); i++ {
		if mask[i] != '[' {
			b.WriteByte(mask[i])
			continue
		}
		end := strings.IndexByte(mask[i:], ']')
		if end < 0 {
			b.WriteString(mask[i:]) // unterminated: literal text
			break
		}
		tok := mask[i+1 : i+end]
		out, ok := resolve(tok, in)
		switch {
		case !ok:
			b.WriteString(mask[i : i+end+1]) // unknown token, left as typed
		case transform != nil:
			b.WriteString(transform(out))
		default:
			b.WriteString(out)
		}
		i += end
	}
	return b.String()
}

// resolve turns one token's inside ("N2-5", "C3", …) into text. The bool reports
// whether it was a token at all.
func resolve(tok string, in maskInput) (string, bool) {
	if tok == "" {
		return "", false
	}
	kind, arg := tok[0], tok[1:]

	switch kind {
	case 'N':
		return slice(in.stem, arg)
	case 'E':
		return slice(in.ext, arg)
	case 'P':
		if arg != "" {
			return slice(in.parent, arg)
		}
		return in.parent, true
	case 'C':
		return counter(in.counter, arg)
	case 'd':
		if arg != "" || in.mod.IsZero() {
			return "", arg == ""
		}
		return in.mod.Format("2006-01-02"), true
	case 't':
		if arg != "" || in.mod.IsZero() {
			return "", arg == ""
		}
		return in.mod.Format("15-04-05"), true
	}
	return "", false
}

// counter renders [C] / [C<width>]. The width pads the digits, not the whole
// token, so a negative step that counts past zero reads as "-01" and not "0-1".
func counter(n int, arg string) (string, bool) {
	digits := strconv.Itoa(n)
	sign := ""
	if n < 0 {
		sign, digits = "-", digits[1:]
	}
	if arg == "" {
		return sign + digits, true
	}

	width, err := strconv.Atoi(arg)
	if err != nil || width < 1 {
		return "", false
	}
	if len(digits) < width {
		digits = strings.Repeat("0", width-len(digits)) + digits
	}
	return sign + digits, true
}

// slice implements the [N], [N#], [N#-#] and [N#-] forms. Indices are 1-based
// and inclusive, which is what reading a filename off the screen feels like, and
// out-of-range ends clamp instead of failing — a mask that asks for more
// characters than a short name has just yields what is there.
func slice(s, arg string) (string, bool) {
	if arg == "" {
		return s, true
	}
	r := []rune(s)

	from, to, err := bounds(arg, len(r))
	if err != nil {
		return "", false
	}
	if from > len(r) || from > to {
		return "", true
	}
	return string(r[from-1 : to]), true
}

// bounds parses "3", "3-5" or "3-" into an inclusive 1-based range.
func bounds(arg string, n int) (from, to int, err error) {
	first, rest, split := strings.Cut(arg, "-")
	from, err = strconv.Atoi(first)
	if err != nil || from < 1 {
		return 0, 0, strconv.ErrSyntax
	}
	switch {
	case !split:
		return from, from, nil
	case rest == "":
		return from, n, nil
	}
	to, err = strconv.Atoi(rest)
	if err != nil || to < 1 {
		return 0, 0, strconv.ErrSyntax
	}
	return from, min(to, n), nil
}
