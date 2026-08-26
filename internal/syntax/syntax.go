// Package syntax is a small highlighter for the file viewer: enough of one to
// tell code from commentary at a glance, and no more.
//
// It is not a parser. It knows four shapes of language — C-like, hash-comment,
// JSON and Markdown — and within them it finds comments, strings, numbers and
// keywords, carrying the state that spans lines (a block comment, a raw string,
// a fenced code block) from one line to the next. That is what a reader's eye
// uses, and it costs no dependency and no second pass over the file.
package syntax

import (
	"path/filepath"
	"strings"
)

// Lang is the shape of language a file is highlighted as.
type Lang int

const (
	None     Lang = iota // no highlighting
	CLike                // // and /* */ comments, quoted strings
	Hash                 // # comments, quoted strings
	JSON                 // strings, numbers, true/false/null
	Markdown             // headings, fences, bullets, code spans
)

func (l Lang) String() string {
	switch l {
	case CLike:
		return "code"
	case Hash:
		return "script"
	case JSON:
		return "json"
	case Markdown:
		return "markdown"
	default:
		return "plain"
	}
}

// Kind is what one run of characters turned out to be.
type Kind int

const (
	Plain Kind = iota
	Keyword
	Str
	Comment
	Number
	Heading
)

// Span is a run of characters that are all the same kind.
type Span struct {
	Text string
	Kind Kind
}

// byExtension maps a file extension onto the shape of language it is.
var byExtension = map[string]Lang{
	".go": CLike, ".c": CLike, ".h": CLike, ".cc": CLike, ".cpp": CLike, ".hpp": CLike,
	".java": CLike, ".js": CLike, ".mjs": CLike, ".ts": CLike, ".tsx": CLike, ".jsx": CLike,
	".rs": CLike, ".swift": CLike, ".kt": CLike, ".cs": CLike, ".php": CLike, ".scala": CLike,
	".css": CLike, ".scss": CLike, ".zig": CLike, ".dart": CLike, ".proto": CLike,

	".sh": Hash, ".bash": Hash, ".zsh": Hash, ".fish": Hash, ".py": Hash, ".rb": Hash,
	".pl": Hash, ".yaml": Hash, ".yml": Hash, ".toml": Hash, ".ini": Hash, ".conf": Hash,
	".cfg": Hash, ".tf": Hash, ".r": Hash, ".ex": Hash, ".exs": Hash,

	".json": JSON, ".jsonc": JSON,

	".md": Markdown, ".markdown": Markdown, ".mdx": Markdown,
}

// byName covers the files everyone knows by name rather than by extension.
var byName = map[string]Lang{
	"makefile": Hash, "dockerfile": Hash, "gemfile": Hash, "rakefile": Hash,
	"vagrantfile": Hash, ".gitignore": Hash, ".env": Hash, ".bashrc": Hash,
	".zshrc": Hash, ".profile": Hash, "config": Hash,
}

// keywords are the words each shape of language colours. The sets are unions
// across the languages that share a shape: colouring "func" in a C file is a
// smaller error than needing a lexer per language to avoid it.
var keywords = map[Lang]map[string]bool{
	CLike: words(`break case catch chan class const constexpr continue default defer delete do
		else enum export extends fallthrough false final finally float for func function go goto
		if impl implements import in int interface let map match mod mut namespace new nil null
		package private protected public range return select self static struct super switch
		this throw true try type typedef typeof union unsafe use var void where while yield
		async await pub fn`),
	Hash: words(`and as assert break case class continue def del do done elif else elsif end esac
		except exec export fi finally for from global if import in is lambda local module next
		nil none not or pass raise require return self then true false unless until while with
		yield echo function set unset source alias`),
	JSON: words(`true false null`),
}

func words(list string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(list) {
		out[w] = true
	}
	return out
}

// Detect picks the shape of language from a file's name.
func Detect(name string) Lang {
	base := strings.ToLower(filepath.Base(name))
	if lang, ok := byName[base]; ok {
		return lang
	}
	if lang, ok := byExtension[filepath.Ext(base)]; ok {
		return lang
	}
	return None
}

// Highlighter reads a file one line at a time, remembering what a line left
// open — a block comment, a raw string, a fenced code block — so the next one
// starts where it should.
type Highlighter struct {
	lang    Lang
	comment bool // inside a /* … */
	raw     bool // inside a `…` raw string
	fence   bool // inside a ``` … ``` block
	keys    map[string]bool
}

// New starts a highlighter for one file.
func New(lang Lang) *Highlighter {
	return &Highlighter{lang: lang, keys: keywords[lang]}
}

// Line breaks one line into spans.
func (h *Highlighter) Line(text string) []Span {
	switch h.lang {
	case CLike:
		return h.cLike(text)
	case Hash:
		return h.hash(text)
	case JSON:
		return h.json(text)
	case Markdown:
		return h.markdown(text)
	default:
		return []Span{{Text: text}}
	}
}

// cLike scans a line of a C-like language.
func (h *Highlighter) cLike(text string) []Span {
	var out spans
	i := 0

	// A block comment or raw string that started on an earlier line runs on.
	if h.comment {
		end := strings.Index(text, "*/")
		if end < 0 {
			return []Span{{Text: text, Kind: Comment}}
		}
		out.add(text[:end+2], Comment)
		h.comment = false
		i = end + 2
	} else if h.raw {
		end := strings.IndexByte(text, '`')
		if end < 0 {
			return []Span{{Text: text, Kind: Str}}
		}
		out.add(text[:end+1], Str)
		h.raw = false
		i = end + 1
	}

	for i < len(text) {
		switch {
		case strings.HasPrefix(text[i:], "//"):
			out.add(text[i:], Comment)
			return out.done()

		case strings.HasPrefix(text[i:], "/*"):
			if end := strings.Index(text[i+2:], "*/"); end >= 0 {
				out.add(text[i:i+2+end+2], Comment)
				i += 2 + end + 2
				continue
			}
			out.add(text[i:], Comment)
			h.comment = true
			return out.done()

		case text[i] == '`':
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				out.add(text[i:i+1+end+1], Str)
				i += 1 + end + 1
				continue
			}
			out.add(text[i:], Str)
			h.raw = true
			return out.done()

		case text[i] == '"' || text[i] == '\'':
			n := scanString(text[i:], text[i])
			out.add(text[i:i+n], Str)
			i += n

		case isDigit(text[i]) && !continuesWord(text, i):
			n := scanNumber(text[i:])
			out.add(text[i:i+n], Number)
			i += n

		case isWordStart(text[i]):
			n := scanWord(text[i:])
			word := text[i : i+n]
			out.add(word, h.wordKind(word))
			i += n

		default:
			out.add(string(text[i]), Plain)
			i++
		}
	}
	return out.done()
}

// hash scans a line of a language whose comments start with #.
func (h *Highlighter) hash(text string) []Span {
	var out spans

	for i := 0; i < len(text); {
		switch {
		case text[i] == '#':
			out.add(text[i:], Comment)
			return out.done()

		case text[i] == '"' || text[i] == '\'':
			n := scanString(text[i:], text[i])
			out.add(text[i:i+n], Str)
			i += n

		case isDigit(text[i]) && !continuesWord(text, i):
			n := scanNumber(text[i:])
			out.add(text[i:i+n], Number)
			i += n

		case isWordStart(text[i]):
			n := scanWord(text[i:])
			word := text[i : i+n]
			out.add(word, h.wordKind(word))
			i += n

		default:
			out.add(string(text[i]), Plain)
			i++
		}
	}
	return out.done()
}

// json scans a line of JSON. A string with a colon after it is a key, which is
// the one distinction that makes a config file readable at a glance.
func (h *Highlighter) json(text string) []Span {
	var out spans

	for i := 0; i < len(text); {
		switch {
		case text[i] == '"':
			n := scanString(text[i:], '"')
			kind := Str
			if isKey(text[i+n:]) {
				kind = Keyword
			}
			out.add(text[i:i+n], kind)
			i += n

		case isDigit(text[i]) || (text[i] == '-' && i+1 < len(text) && isDigit(text[i+1])):
			n := scanNumber(text[i:])
			out.add(text[i:i+n], Number)
			i += n

		case isWordStart(text[i]):
			n := scanWord(text[i:])
			word := text[i : i+n]
			out.add(word, h.wordKind(word))
			i += n

		default:
			out.add(string(text[i]), Plain)
			i++
		}
	}
	return out.done()
}

// markdown marks the shapes that carry a document's structure.
func (h *Highlighter) markdown(text string) []Span {
	trimmed := strings.TrimSpace(text)

	if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
		h.fence = !h.fence
		return []Span{{Text: text, Kind: Comment}}
	}
	if h.fence {
		return []Span{{Text: text, Kind: Str}}
	}
	if strings.HasPrefix(trimmed, "#") {
		return []Span{{Text: text, Kind: Heading}}
	}
	if strings.HasPrefix(trimmed, ">") {
		return []Span{{Text: text, Kind: Comment}}
	}

	var out spans
	indent := len(text) - len(strings.TrimLeft(text, " \t"))
	i := 0

	// A bullet or a number at the start of the line is the list's shape.
	if marker := listMarker(text[indent:]); marker > 0 {
		out.add(text[:indent], Plain)
		out.add(text[indent:indent+marker], Keyword)
		i = indent + marker
	}

	for i < len(text) {
		if text[i] == '`' {
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				out.add(text[i:i+1+end+1], Str)
				i += 1 + end + 1
				continue
			}
		}
		if text[i] == '[' {
			if end := strings.IndexByte(text[i:], ']'); end > 0 {
				out.add(text[i:i+end+1], Keyword)
				i += end + 1
				continue
			}
		}
		out.add(string(text[i]), Plain)
		i++
	}
	return out.done()
}

// wordKind says whether a bare word is one of the language's keywords.
func (h *Highlighter) wordKind(word string) Kind {
	if h.keys != nil && h.keys[word] {
		return Keyword
	}
	return Plain
}

// spans accumulates runs, merging neighbours of the same kind so the renderer
// is handed as few pieces as possible.
type spans []Span

func (s *spans) add(text string, kind Kind) {
	if text == "" {
		return
	}
	if n := len(*s); n > 0 && (*s)[n-1].Kind == kind {
		(*s)[n-1].Text += text
		return
	}
	*s = append(*s, Span{Text: text, Kind: kind})
}

func (s *spans) done() []Span {
	if len(*s) == 0 {
		return []Span{{Text: ""}}
	}
	return *s
}

// scanString measures a quoted string from its opening quote, honouring
// backslash escapes and stopping at the end of the line if it is unterminated.
func scanString(text string, quote byte) int {
	for i := 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case quote:
			return i + 1
		}
	}
	return len(text)
}

// scanNumber measures a number, including a hex prefix and a decimal point.
func scanNumber(text string) int {
	i := 0
	if text[0] == '-' {
		i++
	}
	for ; i < len(text); i++ {
		c := text[i]
		if isDigit(c) || c == '.' || c == '_' ||
			(c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') ||
			c == 'x' || c == 'X' || c == 'o' || c == 'b' {
			continue
		}
		break
	}
	return max(i, 1)
}

// scanWord measures an identifier.
func scanWord(text string) int {
	i := 0
	for ; i < len(text); i++ {
		if !isWordStart(text[i]) && !isDigit(text[i]) {
			break
		}
	}
	return max(i, 1)
}

// isKey reports whether what follows a JSON string is a colon.
func isKey(rest string) bool {
	return strings.HasPrefix(strings.TrimLeft(rest, " \t"), ":")
}

// listMarker measures a markdown bullet or numbered item at the start of text.
func listMarker(text string) int {
	if len(text) >= 2 && (text[0] == '-' || text[0] == '*' || text[0] == '+') && text[1] == ' ' {
		return 2
	}
	for i := 0; i < len(text); i++ {
		if isDigit(text[i]) {
			continue
		}
		if i > 0 && (text[i] == '.' || text[i] == ')') && i+1 < len(text) && text[i+1] == ' ' {
			return i + 2
		}
		break
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isWordStart(c byte) bool {
	return c == '_' || c >= 128 || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// continuesWord reports whether the character at i is part of the identifier
// before it, so the 2 in "utf8" is not read as a number.
func continuesWord(text string, i int) bool {
	return i > 0 && (isWordStart(text[i-1]) || isDigit(text[i-1]))
}
