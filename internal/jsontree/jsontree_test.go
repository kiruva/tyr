package jsontree

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const sample = `{
  "name": "tyr",
  "version": 1.50e+10,
  "tags": ["a", "b", "c"],
  "deps": {"bubbletea": {"v": "1.3"}, "odd key": null},
  "empty": {},
  "none": []
}`

func build(t *testing.T, text string) Model {
	t.Helper()
	m, err := Build(text)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return m.SetHeight(10)
}

// key sends one keypress, by the name tea gives it.
func key(m Model, k string) (Model, Result) {
	var msg tea.KeyMsg
	switch k {
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "end":
		msg = tea.KeyMsg{Type: tea.KeyEnd}
	case "home":
		msg = tea.KeyMsg{Type: tea.KeyHome}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	return m.Update(msg)
}

func keys(m Model, ks ...string) Model {
	for _, k := range ks {
		m, _ = key(m, k)
	}
	return m
}

// plain is the view without its colours, one string per row.
func plain(m Model, width, height int) []string {
	return strings.Split(ansi.Strip(m.View(width, height)), "\n")
}

// The tree opens with the top level showing, in the file's order, each
// container summed up and each scalar as written.
func TestBuildShowsTopLevel(t *testing.T) {
	m := build(t, sample)
	rows := plain(m, 60, 10)
	want := []string{
		`▾ {6 keys}`,
		`    name: "tyr"`,
		`    version: 1.50e+10`,
		`  ▸ tags: [3 items]`,
		`  ▸ deps: {2 keys}`,
		`    empty: {}`,
		`    none: []`,
	}
	for i, w := range want {
		if strings.TrimRight(rows[i], " ") != w {
			t.Errorf("row %d = %q, want %q", i, rows[i], w)
		}
	}
	if len(rows) != 10 {
		t.Errorf("%d rows drawn, want the height asked for", len(rows))
	}
}

// → opens, ← closes then goes to the parent, enter toggles.
func TestExpandCollapse(t *testing.T) {
	m := build(t, sample)
	m = keys(m, "down", "down", "down") // tags
	if got := m.Path(); got != ".tags" {
		t.Fatalf("cursor on %s, want .tags", got)
	}
	m = keys(m, "right")
	rows := plain(m, 60, 10)
	if strings.TrimSpace(rows[4]) != `[0]: "a"` {
		t.Errorf("→ did not open tags: %q", rows)
	}
	m = keys(m, "right") // → on an open node goes into it
	if got := m.Path(); got != ".tags[0]" {
		t.Errorf("→ on an open node went to %s, want its first item", got)
	}
	m = keys(m, "left") // ← on a leaf goes to the parent
	if got := m.Path(); got != ".tags" {
		t.Errorf("← on a leaf went to %s, want .tags", got)
	}
	m = keys(m, "left") // ← on an open node closes it
	if rows := plain(m, 60, 10); strings.Contains(rows[4], "[0]") {
		t.Errorf("← did not close tags: %q", rows)
	}
	m = keys(m, "enter")
	if rows := plain(m, 60, 10); !strings.Contains(rows[4], "[0]") {
		t.Errorf("enter did not open tags: %q", rows)
	}
}

// * opens everything under the cursor; - closes all but the top.
func TestExpandAllCollapseAll(t *testing.T) {
	m := build(t, sample)
	m = keys(m, "*")
	text := strings.Join(plain(m, 60, 30), "\n")
	for _, want := range []string{`v: "1.3"`, `odd key: null`, `[2]: "c"`} {
		if !strings.Contains(text, want) {
			t.Errorf("* did not open everything, %q missing:\n%s", want, text)
		}
	}
	m = keys(m, "end", "up", "up") // from none, past empty

	if got := m.Path(); got != `.deps["odd key"]` {
		t.Fatalf("cursor on %s", got)
	}
	m = keys(m, "-")
	if got := m.Path(); got != ".deps" {
		t.Errorf("- left the cursor on %s, want the row that holds it, .deps", got)
	}
	if text := strings.Join(plain(m, 60, 30), "\n"); strings.Contains(text, "bubbletea") {
		t.Errorf("- did not close deps:\n%s", text)
	}
}

// The path is jq's: identifiers dotted, anything else quoted in brackets.
func TestPath(t *testing.T) {
	m := build(t, `{"a":{"b c":[0,{"_x9":1,"":2,"q\"":3}]}}`)
	m = keys(m, "*")
	var got []string
	for range 8 {
		got = append(got, m.Path())
		m = keys(m, "down")
	}
	want := []string{".", ".a", `.a["b c"]`, `.a["b c"][0]`, `.a["b c"][1]`,
		`.a["b c"][1]._x9`, `.a["b c"][1][""]`, `.a["b c"][1]["q\""]`}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("paths\n got %q\nwant %q", got, want)
	}

	if m := build(t, `[1]`); m.Path() != "." {
		t.Errorf("root path = %q", m.Path())
	}
	m = keys(build(t, `[1]`), "down")
	if m.Path() != ".[0]" {
		t.Errorf("first item of a top-level array = %q, want .[0]", m.Path())
	}
}

// y hands the path back to be copied.
func TestCopyPath(t *testing.T) {
	m := keys(build(t, sample), "down")
	_, res := key(m, "y")
	if res.Copy != ".name" {
		t.Errorf("y copied %q, want .name", res.Copy)
	}
	if !strings.Contains(res.Status, ".name") {
		t.Errorf("status %q does not say what was copied", res.Status)
	}
}

// / finds in keys and values, inside closed nodes too, and opens the way
// there; n goes on to the next and wraps.
func TestFind(t *testing.T) {
	m := build(t, sample)
	m = keys(m, "/")
	if !m.Prompting() {
		t.Fatal("/ did not open the find field")
	}
	m = keys(m, "1", ".", "3", "enter")
	if m.Prompting() {
		t.Error("enter left the find field open")
	}
	if got := m.Path(); got != ".deps.bubbletea.v" {
		t.Errorf("find went to %s, want the value inside the closed deps", got)
	}

	m = keys(m, "/", "x", "esc") // esc gives up without moving
	if m.Prompting() || m.Path() != ".deps.bubbletea.v" {
		t.Errorf("esc: prompting %v, cursor on %s", m.Prompting(), m.Path())
	}

	m = keys(build(t, sample), "/", "B", "enter") // case does not matter; keys count
	if got := m.Path(); got != ".tags[1]" {
		t.Errorf("first hit for b = %s, want .tags[1]", got)
	}
	m = keys(m, "n")
	if got := m.Path(); got != ".deps.bubbletea" {
		t.Errorf("n went to %s, want the key bubbletea", got)
	}
	m = keys(m, "N")
	if got := m.Path(); got != ".tags[1]" {
		t.Errorf("N went to %s, want back to .tags[1]", got)
	}

	_, res := key(keys(build(t, sample), "/", "z", "z", "enter"), "n")
	if !strings.Contains(res.Status, "no match") {
		t.Errorf("status %q, want it to say nothing matched", res.Status)
	}
}

// A long value is cut at the width, a key with a newline in it is shown
// escaped, and the cursor row is as wide as the window.
func TestViewFitsWidth(t *testing.T) {
	m := build(t, `{"k\ney": "`+strings.Repeat("x", 200)+`"}`)
	for i, row := range plain(m, 40, 3) {
		if w := ansi.StringWidth(row); w > 40 {
			t.Errorf("row %d is %d wide, over 40: %q", i, w, row)
		}
	}
	if rows := plain(m, 40, 3); !strings.Contains(rows[1], `k\ney`) {
		t.Errorf("the newline in the key is not escaped: %q", rows[1])
	}
}

// The window follows the cursor.
func TestScrollFollowsCursor(t *testing.T) {
	m := build(t, "["+strings.TrimSuffix(strings.Repeat("7,", 50), ",")+"]")
	m = m.SetHeight(5)
	m = keys(m, "end")
	rows := plain(m, 30, 5)
	if !strings.Contains(rows[4], "[49]: 7") {
		t.Errorf("end did not scroll to the last item: %q", rows)
	}
	m = m.Scroll(-3)
	if got := m.Path(); got != ".[46]" {
		t.Errorf("the wheel moved the cursor to %s, want .[46]", got)
	}
}

// Numbers, keys and strings come through as they were written.
func TestScalarsVerbatim(t *testing.T) {
	m := build(t, `[12345678901234567890123, -0.0, 1E400, "é\t", true, null]`)
	text := strings.Join(plain(m, 60, 10), "\n")
	for _, want := range []string{"12345678901234567890123", "-0.0", "1E400", `"é\t"`, "true", "null"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q not shown as written:\n%s", want, text)
		}
	}
}

func TestBuildErrors(t *testing.T) {
	for _, text := range []string{"", "  ", `{"a":}`, `[1] [2]`, `{"a":1`} {
		if _, err := Build(text); err == nil {
			t.Errorf("Build(%q) took it", text)
		}
	}
	if _, err := Build("\uFEFF" + `{"a":1}`); err != nil {
		t.Errorf("a byte order mark is refused: %v", err)
	}
	deep := strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	if _, err := Build(deep); !errors.Is(err, ErrTooLarge) {
		t.Errorf("nesting past the cap: %v, want ErrTooLarge", err)
	}
}

// A file bigger than the tree holds is refused rather than eating memory.
func TestBuildNodeCap(t *testing.T) {
	text := "[" + strings.TrimSuffix(strings.Repeat("0,", MaxNodes), ",") + "]"
	if _, err := Build(text); !errors.Is(err, ErrTooLarge) {
		t.Errorf("%d values: %v, want ErrTooLarge", MaxNodes+1, err)
	}
}

// A big file stays usable: building, opening everything, drawing and finding
// the last value all take time in proportion to the file, and drawing only
// pays for what is on screen.
func TestHugeArrayIsFast(t *testing.T) {
	const n = 500_000
	var b strings.Builder
	b.WriteString("[")
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%d}`, i)
	}
	b.WriteString("]")

	start := time.Now()
	m, err := Build(b.String())
	if err != nil {
		t.Fatal(err)
	}
	m = m.SetHeight(40)
	m = keys(m, "*", "end", "/", "4", "9", "9", "9", "9", "9", "enter")
	m.View(120, 40)
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("%v for %d objects", took, n)
	}
	if got := m.Path(); got != ".[499999].id" {
		t.Errorf("find went to %s", got)
	}

	start = time.Now()
	for range 100 {
		m.View(120, 40)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("%v for 100 redraws of 40 rows", took)
	}
}
