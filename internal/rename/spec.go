// Package rename plans and performs batch renames. It is UI-agnostic: Plan is
// pure (given a candidate list it produces the new names and every reason a
// name cannot be used), and Apply is the only part that touches the disk.
package rename

import (
	"fmt"
	"regexp"
	"strings"
)

// Mode is how the Find pattern is interpreted.
type Mode int

const (
	ModeRegex   Mode = iota // Go regexp, with $1 backreferences in Replace
	ModeLiteral             // plain substring
	ModeGlob                // shell wildcards: * ? and [abc]
)

func (m Mode) String() string {
	switch m {
	case ModeLiteral:
		return "literal"
	case ModeGlob:
		return "glob"
	default:
		return "regex"
	}
}

// Next cycles to the following mode, for the tool's mode key.
func (m Mode) Next() Mode { return (m + 1) % 3 }

// CaseOp is an optional case conversion applied after substitution.
type CaseOp int

const (
	CaseNone CaseOp = iota
	CaseLower
	CaseUpper
	CaseTitle
)

func (c CaseOp) String() string {
	switch c {
	case CaseLower:
		return "lower"
	case CaseUpper:
		return "UPPER"
	case CaseTitle:
		return "Title"
	default:
		return "as-is"
	}
}

// Next cycles to the following case op, for the tool's case key.
func (c CaseOp) Next() CaseOp { return (c + 1) % 4 }

// Spec is the whole rename recipe. The zero value renames nothing; use
// DefaultSpec for the identity recipe the tool starts from.
type Spec struct {
	// Find / Replace substitute inside the assembled name.
	Find    string
	Replace string
	Mode    Mode
	Ignore  bool // case-insensitive matching

	// Name / Ext are masks built from tokens ([N], [E], [C], …) and literal
	// text. "[N]" and "[E]" reproduce the original name.
	Name string
	Ext  string

	// Counter is the starting value of [C], advanced by Step for each candidate
	// the Find pattern matches.
	Counter int
	Step    int

	// Case and Trim are applied last, to the name without its extension.
	Case CaseOp
	Trim bool
}

// DefaultSpec is the identity recipe: every candidate keeps its name.
func DefaultSpec() Spec {
	return Spec{Mode: ModeRegex, Name: "[N]", Ext: "[E]", Counter: 1, Step: 1}
}

// matcher applies the Find pattern to one name. The replacement is not part of
// it: tokens make it per-candidate, so sub takes it as an argument.
type matcher struct {
	re     *regexp.Regexp // regex and glob modes
	find   string         // literal mode
	ignore bool
	all    bool // no pattern at all: every name "matches", nothing is replaced
}

// newMatcher compiles the spec's pattern, reporting a bad regex as an error so
// the tool can show it under the field instead of guessing at a fallback.
func newMatcher(s Spec) (*matcher, error) {
	if s.Find == "" {
		return &matcher{all: true}, nil
	}

	switch s.Mode {
	case ModeLiteral:
		return &matcher{find: s.Find, ignore: s.Ignore}, nil

	case ModeGlob:
		re, err := regexp.Compile(prefix(s.Ignore) + globToRegex(s.Find))
		if err != nil {
			return nil, fmt.Errorf("bad pattern: %w", err)
		}
		return &matcher{re: re}, nil

	default:
		re, err := regexp.Compile(prefix(s.Ignore) + s.Find)
		if err != nil {
			return nil, fmt.Errorf("bad regex: %w", cleanRegexErr(err))
		}
		return &matcher{re: re}, nil
	}
}

func prefix(ignore bool) string {
	if ignore {
		return "(?i)"
	}
	return ""
}

// matches reports whether the pattern is present in name.
func (mt *matcher) matches(name string) bool {
	switch {
	case mt.all:
		return true
	case mt.re != nil:
		return mt.re.MatchString(name)
	case mt.ignore:
		return strings.Contains(strings.ToLower(name), strings.ToLower(mt.find))
	default:
		return strings.Contains(name, mt.find)
	}
}

// sub returns name with every occurrence of the pattern replaced by repl, which
// has already had its tokens resolved for this candidate.
func (mt *matcher) sub(name, repl string) string {
	switch {
	case mt.all:
		return name
	case mt.re != nil:
		return mt.re.ReplaceAllString(name, repl)
	case mt.ignore:
		return replaceFold(name, mt.find, repl)
	default:
		return strings.ReplaceAll(name, mt.find, repl)
	}
}

// replaceFold is strings.ReplaceAll with a case-insensitive needle.
func replaceFold(s, old, new string) string {
	if old == "" {
		return s
	}
	lowerS, lowerOld := strings.ToLower(s), strings.ToLower(old)
	var b strings.Builder
	for {
		i := strings.Index(lowerS, lowerOld)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(new)
		s, lowerS = s[i+len(old):], lowerS[i+len(old):]
	}
}

// globToRegex translates shell wildcards into an anchored regex, so a glob
// describes the whole name the way it does on a command line.
func globToRegex(glob string) string {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			b.WriteString("(.*)")
		case '?':
			b.WriteString("(.)")
		case '[':
			if end := strings.IndexByte(glob[i:], ']'); end > 1 {
				b.WriteString(glob[i : i+end+1])
				i += end
				continue
			}
			b.WriteString(regexp.QuoteMeta(string(c)))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteByte('$')
	return b.String()
}

// cleanRegexErr strips regexp's "error parsing regexp: " preamble, which only
// repeats what the field already says.
func cleanRegexErr(err error) error {
	msg := strings.TrimPrefix(err.Error(), "error parsing regexp: ")
	return fmt.Errorf("%s", msg)
}
