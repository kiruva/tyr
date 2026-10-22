// Package syntax is a small highlighter for the file viewer: enough of one to
// tell code from commentary at a glance, and no more.
//
// It is not a parser. It knows a few shapes of language — C-like, hash-comment,
// JSON, Markdown, XML, SQL and diff — and within them it finds comments, strings, numbers and
// keywords, carrying the state that spans lines (a block comment, a raw string,
// a fenced code block) from one line to the next. That is what a reader's eye
// uses, and it costs no dependency and no second pass over the file. A
// language it knows by name (Go, Python, …) is one of the shapes plus its own
// keywords.
package syntax

import (
	"path/filepath"
	"strings"
)

// Lang is the shape of language a file is highlighted as.
type Lang int

// The shapes. CLike and Hash on their own are for files of that shape whose
// language has no keyword set here; they colour a union of keywords.
const (
	None     Lang = iota // no highlighting
	CLike                // // and /* */ comments, quoted strings
	Hash                 // # comments, quoted strings
	JSON                 // strings, numbers, true/false/null
	Markdown             // headings, fences, bullets, code spans
	XML                  // tags, attributes, entities, <!-- --> comments
	SQL                  // -- and /* */ comments, keywords in any case
	Diff                 // file headers, hunks, added and removed lines
)

// The languages, each one of the shapes with its own keywords.
const (
	Go Lang = iota + 100
	C
	Cpp
	Java
	Kotlin
	JavaScript
	TypeScript
	Rust
	Swift
	CSharp
	PHP
	Python
	Ruby
	Shell
	YAML
	TOML
	HTML
	Dockerfile
	CSS
)

func (l Lang) String() string {
	if lang, ok := languages[l]; ok {
		return lang.name
	}
	switch l {
	case CLike:
		return "code"
	case Hash:
		return "script"
	case JSON:
		return "json"
	case Markdown:
		return "markdown"
	case XML:
		return "xml"
	case SQL:
		return "sql"
	case Diff:
		return "diff"
	default:
		return "plain"
	}
}

// Comment is how a line of l is commented out: a marker that runs to the end
// of the line, or failing that a pair to wrap it in. All empty: it cannot be.
func (l Lang) Comment() (line, open, close string) {
	switch {
	case l == CSS:
		return "", "/*", "*/"
	case l == XML || l == Markdown || l.shape() == XML:
		return "", "<!--", "-->"
	}
	switch l.shape() {
	case CLike:
		return "//", "", ""
	case Hash:
		return "#", "", ""
	case SQL:
		return "--", "", ""
	}
	return "", "", ""
}

// shape is the shape of language l is scanned as.
func (l Lang) shape() Lang {
	if lang, ok := languages[l]; ok {
		return lang.shape
	}
	return l
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
	Key     // a key in JSON, YAML or TOML
	Name    // a shell $variable or a Python @decorator
	Added   // a line a diff adds
	Removed // a line a diff removes
)

// Span is a run of characters that are all the same kind.
type Span struct {
	Text string
	Kind Kind
}

// byExtension maps a file extension onto the language it is.
var byExtension = map[string]Lang{
	".go": Go, ".c": C, ".h": C, ".cc": Cpp, ".cpp": Cpp, ".cxx": Cpp, ".hpp": Cpp,
	".hh": Cpp, ".java": Java, ".kt": Kotlin, ".kts": Kotlin, ".js": JavaScript,
	".mjs": JavaScript, ".cjs": JavaScript, ".jsx": JavaScript, ".ts": TypeScript,
	".tsx": TypeScript, ".mts": TypeScript, ".rs": Rust, ".swift": Swift, ".cs": CSharp,
	".php": PHP, ".scala": CLike, ".css": CSS, ".scss": CLike, ".zig": CLike,
	".dart": CLike, ".proto": CLike,

	".sh": Shell, ".bash": Shell, ".zsh": Shell, ".fish": Shell, ".py": Python,
	".pyw": Python, ".rb": Ruby, ".pl": Hash, ".yaml": YAML, ".yml": YAML, ".toml": TOML,
	".ini": Hash, ".conf": Hash, ".cfg": Hash, ".tf": Hash, ".r": Hash, ".ex": Hash,
	".exs": Hash,

	".json": JSON, ".jsonc": JSON,

	".md": Markdown, ".markdown": Markdown, ".mdx": Markdown,

	".xml": XML, ".xsd": XML, ".xsl": XML, ".xslt": XML, ".svg": XML, ".plist": XML,
	".csproj": XML, ".fsproj": XML, ".vcxproj": XML, ".xaml": XML, ".rss": XML,
	".atom": XML, ".html": HTML, ".htm": HTML, ".xhtml": HTML,

	".sql": SQL, ".diff": Diff, ".patch": Diff, ".dockerfile": Dockerfile,
}

// byName covers the files everyone knows by name rather than by extension.
var byName = map[string]Lang{
	"makefile": Hash, "dockerfile": Dockerfile, "containerfile": Dockerfile, "gemfile": Ruby, "rakefile": Ruby,
	"vagrantfile": Ruby, ".gitignore": Hash, ".env": Hash, ".bashrc": Shell,
	".zshrc": Shell, ".profile": Shell, "config": Hash,
}

// language is a language known by name: the shape it is scanned as and the
// words it reserves.
type language struct {
	name  string
	shape Lang
	keys  map[string]bool
}

var (
	cKeys = words(`auto break case char const continue default do double else enum extern
		float for goto if inline int long register return short signed sizeof static struct
		switch typedef union unsigned void volatile while include define ifdef ifndef endif
		pragma NULL bool catch class constexpr delete false friend namespace new nullptr
		operator private protected public template this throw true try typename using virtual
		override final noexcept`)
	javaKeys = words(`abstract assert boolean break byte case catch char class const continue
		default do double else enum extends final finally float for if implements import
		instanceof int interface long native new package private protected public return short
		static super switch synchronized this throw throws transient try void volatile while
		true false null var record yield fun val when object companion data sealed override
		open internal is as in out lateinit init by suspend`)
	jsKeys = words(`async await break case catch class const continue debugger default delete do
		else export extends false finally for from function if import in instanceof let new
		null of return static super switch this throw true try typeof undefined var void while
		with yield interface type enum implements namespace declare readonly private protected
		public abstract as keyof satisfies`)
)

var languages = map[Lang]language{
	Go: {"go", CLike, words(`break case chan const continue default defer else fallthrough for
		func go goto if import interface map package range return select struct switch type var
		true false nil iota`)},
	C:          {"c", CLike, cKeys},
	Cpp:        {"c++", CLike, cKeys},
	Java:       {"java", CLike, javaKeys},
	Kotlin:     {"kotlin", CLike, javaKeys},
	JavaScript: {"javascript", CLike, jsKeys},
	TypeScript: {"typescript", CLike, jsKeys},
	Rust: {"rust", CLike, words(`as async await break const continue crate dyn else enum extern
		false fn for if impl in let loop match mod move mut pub ref return self Self static
		struct super trait true type unsafe use where while`)},
	Swift: {"swift", CLike, words(`associatedtype class deinit enum extension fileprivate func
		import init inout internal let open operator private protocol public rethrows static
		struct subscript typealias var break case continue default defer do else fallthrough
		for guard if in repeat return switch where while as catch false is nil self Self super
		throw throws true try async await`)},
	CSharp: {"c#", CLike, words(`abstract as base bool break byte case catch char checked class
		const continue decimal default delegate do double else enum event explicit extern false
		finally fixed float for foreach goto if implicit in int interface internal is lock long
		namespace new null object operator out override params private protected public
		readonly record ref return sbyte sealed short sizeof stackalloc static string struct
		switch this throw true try typeof uint ulong unchecked unsafe ushort using var virtual
		void volatile while async await get set`)},
	PHP: {"php", CLike, words(`abstract and array as break callable case catch class clone
		const continue declare default do echo else elseif empty extends final finally fn for
		foreach function global goto if implements include include_once instanceof interface
		isset list match namespace new or print private protected public readonly require
		require_once return static switch throw trait try unset use var while xor yield true
		false null`)},
	Python: {"python", Hash, words(`and as assert async await break class continue def del elif
		else except False finally for from global if import in is lambda None nonlocal not or
		pass raise return True try while with yield match case self`)},
	Ruby: {"ruby", Hash, words(`alias and begin break case class def defined do else elsif end
		ensure false for if in module next nil not or redo rescue retry return self super then
		true undef unless until when while yield require require_relative attr_accessor
		attr_reader attr_writer private protected public`)},
	Shell: {"shell", Hash, words(`if then else elif fi case esac for select while until do done
		in function time return exit break continue local export readonly declare typeset
		unset source alias echo set shift trap eval exec true false end`)},
	YAML: {"yaml", Hash, words(`true false null yes no on off True False Null TRUE FALSE NULL`)},
	TOML: {"toml", Hash, words(`true false`)},
	HTML: {"html", XML, nil},
	CSS: {"css", CLike, words(`important media import supports keyframes charset namespace page
		from to and not only`)},
	Dockerfile: {"dockerfile", Hash, words(`FROM AS RUN CMD LABEL EXPOSE ENV ADD COPY ENTRYPOINT
		VOLUME USER WORKDIR ARG ONBUILD STOPSIGNAL HEALTHCHECK SHELL MAINTAINER`)},
}

// keywords are the words each bare shape colours. The sets are unions across
// the languages that share a shape, for files whose language has no set of
// its own: colouring "func" in one is a smaller error than colouring nothing.
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
	SQL: words(`select from where and or not insert into values update set delete create
		table drop alter add column index primary key foreign references join left right inner
		outer full cross on as group by order having limit offset union all distinct case when
		then else end is null like ilike in between exists view trigger function procedure
		begin commit rollback transaction if default unique check constraint asc desc with
		returning true false int integer varchar text char boolean date timestamp float real
		numeric decimal serial bigint smallint replace grant revoke cascade database schema
		use explain truncate rename to`),
}

func words(list string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(list) {
		out[w] = true
	}
	return out
}

// Detect picks the language from a file's name.
func Detect(name string) Lang {
	base := strings.ToLower(filepath.Base(name))
	if lang, ok := byName[base]; ok {
		return lang
	}
	if lang, ok := byExtension[filepath.Ext(base)]; ok {
		return lang
	}
	if strings.HasPrefix(base, "dockerfile.") || strings.HasPrefix(base, "containerfile.") {
		return Dockerfile
	}
	return None
}

// Highlighter reads a file one line at a time, remembering what a line left
// open — a block comment, a raw string, a fenced code block — so the next one
// starts where it should.
type Highlighter struct {
	lang    Lang
	shape   Lang
	comment bool // inside a /* … */
	raw     bool // inside a `…` raw string
	triple  byte // inside a Python """ or ''' string: its quote
	tag     bool // inside an XML <tag …>
	fence   bool // inside a ``` … ``` block
	keys    map[string]bool
}

// New starts a highlighter for one file.
func New(lang Lang) *Highlighter {
	keys := keywords[lang]
	if l, ok := languages[lang]; ok {
		keys = l.keys
	}
	return &Highlighter{lang: lang, shape: lang.shape(), keys: keys}
}

// Line breaks one line into spans.
func (h *Highlighter) Line(text string) []Span {
	switch h.shape {
	case CLike:
		return h.cLike(text)
	case Hash:
		return h.hash(text)
	case JSON:
		return h.json(text)
	case Markdown:
		return h.markdown(text)
	case XML:
		return h.xml(text)
	case SQL:
		return h.sql(text)
	case Diff:
		return h.diff(text)
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
			out.add(text[i:i+1], Plain) // a slice, not string(byte): that splits UTF-8
			i++
		}
	}
	return out.done()
}

// hash scans a line of a language whose comments start with #.
func (h *Highlighter) hash(text string) []Span {
	var out spans
	i := 0

	// A triple-quoted string that started on an earlier line runs on.
	if h.triple != 0 {
		end := strings.Index(text, strings.Repeat(string(h.triple), 3))
		if end < 0 {
			return []Span{{Text: text, Kind: Str}}
		}
		out.add(text[:end+3], Str)
		h.triple = 0
		i = end + 3
	} else {
		switch h.lang {
		case YAML:
			i = yamlKey(text, &out)
		case TOML:
			i = tomlKey(text, &out)
		case Python:
			i = decorator(text, &out)
		}
	}

	for i < len(text) {
		switch {
		case text[i] == '#':
			out.add(text[i:], Comment)
			return out.done()

		case h.lang == Python && (strings.HasPrefix(text[i:], `"""`) || strings.HasPrefix(text[i:], `'''`)):
			if end := strings.Index(text[i+3:], text[i:i+3]); end >= 0 {
				out.add(text[i:i+3+end+3], Str)
				i += 3 + end + 3
				continue
			}
			out.add(text[i:], Str)
			h.triple = text[i]
			return out.done()

		case h.vars() && text[i] == '$':
			n := scanVar(text[i:])
			kind := Name
			if n == 1 {
				kind = Plain
			}
			out.add(text[i:i+n], kind)
			i += n

		case h.vars() && text[i] == '"':
			i += shellString(text[i:], &out)

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
			out.add(text[i:i+1], Plain) // a slice, not string(byte): that splits UTF-8
			i++
		}
	}
	return out.done()
}

// yamlKey marks the key a YAML line starts with, after its indent and any
// "- " list markers, and returns where the scan carries on from. A key is a
// quoted string or bare text followed by a colon and a space (or the end of
// the line), so "http://x" is not one.
func yamlKey(text string, out *spans) int {
	p := indent(text)
	for strings.HasPrefix(text[p:], "- ") {
		p += 2
		p += indent(text[p:])
	}
	if p >= len(text) {
		return 0
	}
	end := -1
	switch text[p] {
	case '#', '[', '{':
		return 0
	case '"', '\'':
		end = p + scanString(text[p:], text[p])
	default:
		for j := p; j < len(text); j++ {
			if text[j] == '#' && (text[j-1] == ' ' || text[j-1] == '\t') {
				return 0
			}
			if text[j] == ':' {
				end = j
				break
			}
		}
	}
	if end < 0 || end >= len(text) || text[end] != ':' ||
		(end+1 < len(text) && text[end+1] != ' ' && text[end+1] != '\t') {
		return 0
	}
	out.add(text[:p], Plain)
	out.add(text[p:end], Key)
	return end
}

// tomlKey marks the key of a TOML "key = value" line — bare, quoted or dotted
// — and returns where the scan carries on from. A [table] header is left be.
func tomlKey(text string, out *spans) int {
	p := indent(text)
	j := p
	for j < len(text) && text[j] != '=' {
		switch c := text[j]; {
		case c == '"' || c == '\'':
			j += scanString(text[j:], c)
		case isWordStart(c) || isDigit(c) || c == '-' || c == '.' || c == ' ' || c == '\t':
			j++
		default:
			return 0
		}
	}
	if j >= len(text) {
		return 0
	}
	k := j
	for k > p && (text[k-1] == ' ' || text[k-1] == '\t') {
		k--
	}
	if k == p {
		return 0
	}
	out.add(text[:p], Plain)
	out.add(text[p:k], Key)
	return k
}

// decorator marks a Python @decorator at the start of a statement; anywhere
// else an @ is matrix multiplication.
func decorator(text string, out *spans) int {
	p := indent(text)
	if p+1 >= len(text) || text[p] != '@' || !isWordStart(text[p+1]) {
		return 0
	}
	j := p + 1
	for j < len(text) && (isWordStart(text[j]) || isDigit(text[j]) || text[j] == '.') {
		j++
	}
	out.add(text[:p], Plain)
	out.add(text[p:j], Name)
	return j
}

// shellString scans a double-quoted shell string from its opening quote,
// marking the variables expanded inside it, and returns its length.
func shellString(text string, out *spans) int {
	start := 0
	for i := 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '$':
			if n := scanVar(text[i:]); n > 1 {
				out.add(text[start:i], Str)
				out.add(text[i:i+n], Name)
				i += n - 1
				start = i + 1
			}
		case '"':
			out.add(text[start:i+1], Str)
			return i + 1
		}
	}
	out.add(text[start:], Str)
	return len(text)
}

// scanVar measures a shell variable from its $: $NAME, ${…}, $1 or one of the
// special parameters. A $ with none of those after it is 1 long.
func scanVar(text string) int {
	if len(text) < 2 {
		return 1
	}
	switch c := text[1]; {
	case c == '{':
		if end := strings.IndexByte(text, '}'); end > 0 {
			return end + 1
		}
		return len(text)
	case isWordStart(c):
		return 1 + scanWord(text[1:])
	case isDigit(c) || strings.IndexByte("?@#$!*-", c) >= 0:
		return 2
	}
	return 1
}

// indent measures the spaces and tabs a line starts with.
func indent(text string) int {
	return len(text) - len(strings.TrimLeft(text, " \t"))
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
				kind = Key
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
			out.add(text[i:i+1], Plain) // a slice, not string(byte): that splits UTF-8
			i++
		}
	}
	return out.done()
}

// xml scans a line of XML or HTML: tags and their attributes, entities, and
// comments. A comment or a tag can run across lines.
func (h *Highlighter) xml(text string) []Span {
	var out spans
	i := 0

	if h.comment {
		end := strings.Index(text, "-->")
		if end < 0 {
			return []Span{{Text: text, Kind: Comment}}
		}
		out.add(text[:end+3], Comment)
		h.comment = false
		i = end + 3
	}

	for i < len(text) {
		if h.tag {
			switch c := text[i]; {
			case c == '>':
				out.add(text[i:i+1], Keyword)
				h.tag = false
				i++
			case strings.HasPrefix(text[i:], "/>") || strings.HasPrefix(text[i:], "?>"):
				out.add(text[i:i+2], Keyword)
				h.tag = false
				i += 2
			case c == '"' || c == '\'':
				n := scanString(text[i:], c)
				out.add(text[i:i+n], Str)
				i += n
			case isNameByte(c):
				n := scanName(text[i:])
				out.add(text[i:i+n], Key)
				i += n
			default:
				out.add(text[i:i+1], Plain)
				i++
			}
			continue
		}

		switch {
		case strings.HasPrefix(text[i:], "<!--"):
			if end := strings.Index(text[i+4:], "-->"); end >= 0 {
				out.add(text[i:i+4+end+3], Comment)
				i += 4 + end + 3
				continue
			}
			out.add(text[i:], Comment)
			h.comment = true
			return out.done()

		case text[i] == '<' && i+1 < len(text) && (isNameByte(text[i+1]) || strings.IndexByte("/!?", text[i+1]) >= 0):
			n := 2 + scanName(text[i+2:])
			if isNameByte(text[i+1]) {
				n = 1 + scanName(text[i+1:])
			}
			out.add(text[i:i+n], Keyword)
			h.tag = true
			i += n

		case text[i] == '&':
			// An entity is short: looking further for its ; is quadratic on a
			// long line of &s.
			if end := strings.IndexByte(text[i:min(i+11, len(text))], ';'); end > 1 &&
				!strings.ContainsAny(text[i+1:i+end], " \t<&") {
				out.add(text[i:i+end+1], Number)
				i += end + 1
				continue
			}
			out.add(text[i:i+1], Plain)
			i++

		default:
			// Text between tags goes in one piece, up to the next < or &.
			n := strings.IndexAny(text[i+1:], "<&")
			if n < 0 {
				n = len(text) - i - 1
			}
			out.add(text[i:i+1+n], Plain)
			i += 1 + n
		}
	}
	return out.done()
}

// sql scans a line of SQL: -- and /* */ comments, quoted strings and
// identifiers, numbers, and keywords in whatever case they are written.
func (h *Highlighter) sql(text string) []Span {
	var out spans
	i := 0

	if h.comment {
		end := strings.Index(text, "*/")
		if end < 0 {
			return []Span{{Text: text, Kind: Comment}}
		}
		out.add(text[:end+2], Comment)
		h.comment = false
		i = end + 2
	}

	for i < len(text) {
		switch {
		case strings.HasPrefix(text[i:], "--"):
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

		case text[i] == '\'' || text[i] == '"' || text[i] == '`':
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
			out.add(word, h.wordKind(strings.ToLower(word)))
			i += n

		default:
			out.add(text[i:i+1], Plain) // a slice, not string(byte): that splits UTF-8
			i++
		}
	}
	return out.done()
}

// diff colours a line of a diff or patch by its first characters.
func (h *Highlighter) diff(text string) []Span {
	kind := Plain
	switch {
	case strings.HasPrefix(text, "diff ") || strings.HasPrefix(text, "index ") ||
		strings.HasPrefix(text, "--- ") || strings.HasPrefix(text, "+++ "):
		kind = Heading
	case strings.HasPrefix(text, "@@"):
		kind = Keyword
	case strings.HasPrefix(text, "+"):
		kind = Added
	case strings.HasPrefix(text, "-"):
		kind = Removed
	case strings.HasPrefix(text, `\`):
		kind = Comment
	}
	return []Span{{Text: text, Kind: kind}}
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
	lead := indent(text)
	i := 0

	// A bullet or a number at the start of the line is the list's shape.
	if marker := listMarker(text[lead:]); marker > 0 {
		out.add(text[:lead], Plain)
		out.add(text[lead:lead+marker], Keyword)
		i = lead + marker
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
		out.add(text[i:i+1], Plain) // a slice, not string(byte): that splits UTF-8
		i++
	}
	return out.done()
}

// vars reports whether the language expands $variables, in and out of
// double quotes.
func (h *Highlighter) vars() bool { return h.lang == Shell || h.lang == Dockerfile }

// wordKind says whether a bare word is one of the language's keywords.
func (h *Highlighter) wordKind(word string) Kind {
	if h.keys != nil && h.keys[word] {
		return Keyword
	}
	return Plain
}

// spans accumulates runs, merging neighbours of the same kind so the renderer
// is handed as few pieces as possible. The merging is done once, in done:
// joining each piece onto the last as it came copied the run every time, which
// on a long line of punctuation is quadratic.
type spans []Span

func (s *spans) add(text string, kind Kind) {
	if text == "" {
		return
	}
	*s = append(*s, Span{Text: text, Kind: kind})
}

func (s *spans) done() []Span {
	in := *s
	if len(in) == 0 {
		return []Span{{Text: ""}}
	}
	out := make([]Span, 0, len(in))
	for i := 0; i < len(in); {
		j := i + 1
		for j < len(in) && in[j].Kind == in[i].Kind {
			j++
		}
		if j == i+1 {
			out = append(out, in[i])
		} else {
			var b strings.Builder
			for _, sp := range in[i:j] {
				b.WriteString(sp.Text)
			}
			out = append(out, Span{Text: b.String(), Kind: in[i].Kind})
		}
		i = j
	}
	return out
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

// isNameByte reports whether c can be part of an XML name.
func isNameByte(c byte) bool {
	return isWordStart(c) || isDigit(c) || c == '-' || c == ':' || c == '.'
}

// scanName measures an XML name; it may be empty.
func scanName(text string) int {
	i := 0
	for i < len(text) && isNameByte(text[i]) {
		i++
	}
	return i
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
