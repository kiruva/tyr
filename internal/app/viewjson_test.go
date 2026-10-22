package app

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/editor"
)

// p shows minified JSON one value to a line, says so, and leaves the file
// alone; p again goes back to the file as it is.
func TestViewerPrettyShowsJSONLaidOut(t *testing.T) {
	body := `{"name":"tyr","ids":[1,2.50e+10]}` + "\n"
	m := openIn(t, "data.json", body)
	path := m.(Model).viewer.source.realPath

	m = press(t, m, "p")
	app := m.(Model)
	if !app.viewer.pretty {
		t.Fatalf("p did not turn pretty on; status %q", app.viewer.status)
	}
	view := app.View()
	for _, want := range []string{`"name": "tyr",`, `2.50e+10`, "pretty"} {
		if !strings.Contains(view, want) {
			t.Errorf("the pretty view does not show %q:\n%s", want, view)
		}
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != body {
		t.Errorf("the file changed under p: %q, %v", got, err)
	}

	m = press(t, m, "p")
	app = m.(Model)
	if app.viewer.pretty || !strings.Contains(app.View(), `{"name":"tyr"`) {
		t.Errorf("p again did not go back to the file as it is:\n%s", app.View())
	}
}

// JSONC is shown laid out without its comments, and the status line says
// they were left out.
func TestViewerPrettyLeavesOutComments(t *testing.T) {
	m := openIn(t, "settings.jsonc", "{\"a\":1, // one\n\"b\":[2,],}\n")
	m = press(t, m, "p")
	app := m.(Model)
	if !app.viewer.pretty {
		t.Fatalf("p refused JSONC: %q", app.viewer.status)
	}
	if !strings.Contains(app.viewer.status, "comments") {
		t.Errorf("status = %q, want it to say the comments are not shown", app.viewer.status)
	}
	if strings.Contains(strings.Join(app.viewer.lines, "\n"), "// one") {
		t.Errorf("the comment is still shown: %q", app.viewer.lines)
	}
}

// Text that is not JSON stays as it is and the status line says where it
// breaks.
func TestViewerPrettyRefusesNotJSON(t *testing.T) {
	m := openIn(t, "notes.txt", "{\"a\": 1\n\"b\": 2}\n")
	m = press(t, m, "p")
	app := m.(Model)
	if app.viewer.pretty {
		t.Fatal("p turned pretty on for text that is not JSON")
	}
	if !strings.Contains(app.viewer.status, "not JSON: line 2") {
		t.Errorf("status = %q, want where it stops being JSON", app.viewer.status)
	}
	if !strings.Contains(app.View(), `{"a": 1`) {
		t.Errorf("the text is not shown as it is:\n%s", app.View())
	}
}

// A search finds what the pretty view shows, not the lines it replaced.
func TestViewerPrettySearchFollows(t *testing.T) {
	m := openIn(t, "data.json", `{"a":"x","b":"x"}`+"\n")
	m = press(t, m, "/")
	m = typeKeys(t, m, `"x"`)
	if got := len(m.(Model).viewer.matches); got != 1 {
		t.Fatalf("%d matches before p, want the one line", got)
	}
	m = press(t, m, "enter")
	m = press(t, m, "p")
	if got := m.(Model).viewer.matches; len(got) != 2 {
		t.Errorf("matches after p = %v, want the two lines it is now on", got)
	}
}

// t shows JSON as a tree to fold, y copies the path of the row, and t again
// goes back to the text.
func TestViewerTree(t *testing.T) {
	m := openIn(t, "data.json", `{"name":"tyr","deps":{"bubbletea":"1.3"}}`+"\n")
	app := m.(Model)
	clip := &editor.MemoryClipboard{}
	app.editor.Clipboard = clip

	m = press(t, app, "t")
	app = m.(Model)
	if !app.viewer.treeOn {
		t.Fatalf("t did not open the tree: %q", app.viewer.status)
	}
	view := app.View()
	for _, want := range []string{"▾ {2 keys}", "▸ deps: {1 key}", "tree"} {
		if !strings.Contains(view, want) {
			t.Errorf("the tree does not show %q:\n%s", want, view)
		}
	}

	m = press(t, m, "down")
	m = press(t, m, "down")
	m = press(t, m, "l")
	if !strings.Contains(m.(Model).View(), `bubbletea: "1.3"`) {
		t.Errorf("→ did not open deps:\n%s", m.(Model).View())
	}
	m = press(t, m, "y")
	if clip.Text != ".deps" {
		t.Errorf("y copied %q, want .deps", clip.Text)
	}
	if !strings.Contains(m.(Model).View(), "copied .deps") {
		t.Errorf("the footer does not say what was copied:\n%s", m.(Model).View())
	}

	m = press(t, m, "t")
	app = m.(Model)
	if app.viewer.treeOn || !strings.Contains(app.View(), `{"name":"tyr"`) {
		t.Errorf("t again did not go back to the text:\n%s", app.View())
	}
	// The tree is kept: back in it, deps is still open.
	m = press(t, m, "t")
	if !strings.Contains(m.(Model).View(), `bubbletea: "1.3"`) {
		t.Errorf("the tree was rebuilt and lost its folds:\n%s", m.(Model).View())
	}

	m = press(t, m, "q")
	if got := m.(Model).mode; got != modeNormal {
		t.Errorf("q in the tree: mode %v, want the viewer closed", got)
	}
}

// / in the tree finds through it, the field drawn in the footer; the
// viewer's own n and w do not reach the text underneath.
func TestViewerTreeFind(t *testing.T) {
	m := openIn(t, "data.json", `{"a":{"b":{"c":"needle"}}}`)
	m = press(t, m, "t")
	m = press(t, m, "/")
	if !strings.Contains(m.(Model).View(), "find:") {
		t.Errorf("no find field in the footer:\n%s", m.(Model).View())
	}
	m = typeKeys(t, m, "needle")
	m = press(t, m, "enter")
	app := m.(Model)
	if got := app.viewer.tree.Path(); got != ".a.b.c" {
		t.Errorf("find went to %s, want .a.b.c", got)
	}
	m = press(t, m, "w")
	if m.(Model).viewer.wrap {
		t.Error("w reached the text under the tree")
	}
}

// JSONC opens as a tree without its comments; text that is not JSON does
// not, and says why.
func TestViewerTreeRefuses(t *testing.T) {
	m := press(t, openIn(t, "a.jsonc", "{\"a\": 1, // c\n}\n"), "t")
	if app := m.(Model); !app.viewer.treeOn || !strings.Contains(app.viewer.status, "comments") {
		t.Errorf("JSONC: tree %v, status %q", app.viewer.treeOn, app.viewer.status)
	}

	m = press(t, openIn(t, "notes.txt", "hello\n"), "t")
	app := m.(Model)
	if app.viewer.treeOn {
		t.Error("t opened a tree on text that is not JSON")
	}
	if !strings.Contains(app.viewer.status, "not JSON: line 1") {
		t.Errorf("status = %q, want where it stops being JSON", app.viewer.status)
	}
}

// The wheel moves the tree's cursor.
func TestViewerTreeWheel(t *testing.T) {
	m := press(t, openIn(t, "a.json", `[1,2,3,4,5,6,7,8]`), "t")
	m, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if got := m.(Model).viewer.tree.Path(); got != ".[2]" {
		t.Errorf("one notch down put the cursor on %s, want .[2]", got)
	}
}
