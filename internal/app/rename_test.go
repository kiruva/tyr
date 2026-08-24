package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/remote"
	"github.com/kiruva/tyr/internal/rename"
)

// drain runs a command and everything it produces, so a test can follow a flow
// that goes off the UI thread — the candidate sweep, and the rename job itself.
func drain(t *testing.T, m tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	for range 200 {
		if cmd == nil {
			return m
		}
		msg := cmd()
		if msg == nil {
			return m
		}
		// A focused input asks for a blinking cursor, which is an endless chain
		// of timers. Nothing under test depends on it.
		if name := fmt.Sprintf("%T", msg); strings.Contains(strings.ToLower(name), "blink") {
			return m
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = drain(t, m, c)
			}
			return m
		}
		m, cmd = m.Update(msg)
	}
	t.Fatal("commands did not settle")
	return m
}

func keyPress(m tea.Model, s string) (tea.Model, tea.Cmd) {
	switch s {
	case "enter":
		return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	case "esc":
		return m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	case "tab":
		return m.Update(tea.KeyMsg{Type: tea.KeyTab})
	case "space":
		return m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	case "down":
		return m.Update(tea.KeyMsg{Type: tea.KeyDown})
	case "ctrl+r":
		return m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	case "ctrl+y":
		return m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	case "ctrl+t":
		return m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	case "ctrl+o":
		return m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	case "backspace":
		return m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	return m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

// press sends a key and runs whatever it asked for.
func press(t *testing.T, m tea.Model, s string) tea.Model {
	t.Helper()
	m, cmd := keyPress(m, s)
	return drain(t, m, cmd)
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// focusEntry walks the cursor onto a named entry.
func focusEntry(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	app := m.(Model)
	p := &app.panes[app.active]
	p.Focus(name)
	if cur, ok := p.Current(); !ok || cur.Name != name {
		t.Fatalf("could not put the cursor on %s", name)
	}
	return app
}

// Single rename ---------------------------------------------------------------

func TestRenameOneRenamesAndFollowsTheCursor(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "old.txt"))
	m := newInDir(t, dir)
	m = focusEntry(t, m, "old.txt")

	m = press(t, m, "r")
	if got := m.(Model).mode; got != modeRenameOne {
		t.Fatalf("mode = %v, want modeRenameOne", got)
	}
	// The prompt starts prefilled, so renaming is an edit, not a retype.
	if got := m.(Model).renOne.input.Value(); got != "old.txt" {
		t.Errorf("prefill = %q, want the current name", got)
	}

	app := m.(Model)
	app.renOne.input.SetValue("new.txt")
	m = app
	m = press(t, m, "enter")

	if got := m.(Model).mode; got != modeNormal {
		t.Errorf("mode = %v, want modeNormal", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatalf("stat new.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); err == nil {
		t.Error("old.txt is still there")
	}
	after := m.(Model)
	if cur, ok := after.panes[0].Current(); !ok || cur.Name != "new.txt" {
		t.Errorf("cursor on %+v, want new.txt", cur)
	}
}

func TestRenameOneRefusesAPathAndKeepsThePrompt(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "old.txt"))
	m := newInDir(t, dir)
	m = focusEntry(t, m, "old.txt")
	m = press(t, m, "r")

	app := m.(Model)
	app.renOne.input.SetValue("sub/new.txt")
	m = app
	m = press(t, m, "enter")

	got := m.(Model)
	if got.mode != modeRenameOne {
		t.Fatalf("mode = %v, want the prompt to stay open", got.mode)
	}
	if !strings.Contains(got.renOne.status, "separator") {
		t.Errorf("status = %q, want it to explain the refusal", got.renOne.status)
	}
	if !strings.Contains(got.View(), "separator") {
		t.Error("the render does not show why the name was refused")
	}
}

func TestRenameOneWillNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "old.txt"))
	write(t, filepath.Join(dir, "taken.txt"))
	m := newInDir(t, dir)
	m = focusEntry(t, m, "old.txt")
	m = press(t, m, "r")

	app := m.(Model)
	app.renOne.input.SetValue("taken.txt")
	m = app
	m = press(t, m, "enter")

	if got := m.(Model).renOne.status; !strings.Contains(got, "already exists") {
		t.Errorf("status = %q, want it to say the name is taken", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "taken.txt"))
	if err != nil || string(data) != "taken.txt" {
		t.Errorf("taken.txt = %q, %v — want it untouched", data, err)
	}
}

func TestRenameOneEscapeCancels(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "old.txt"))
	m := newInDir(t, dir)
	m = focusEntry(t, m, "old.txt")
	m = press(t, m, "r")
	m = press(t, m, "esc")

	if got := m.(Model).mode; got != modeNormal {
		t.Errorf("mode = %v, want modeNormal", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); err != nil {
		t.Error("esc renamed it anyway")
	}
}

func TestRenameRefusedOnRemotePane(t *testing.T) {
	m := newInDir(t, t.TempDir())
	app := m.(Model)
	remotePane(t, &app, 0, remote.Host{Name: "box", User: "kim"}, "/srv")

	for _, k := range []string{"r", "M"} {
		next := press(t, app, k)
		got := next.(Model)
		if got.mode != modeNormal {
			t.Errorf("%s on a remote pane opened %v", k, got.mode)
		}
		if !strings.Contains(got.errText, "local-only") {
			t.Errorf("%s: errText = %q, want it to say rename is local-only", k, got.errText)
		}
	}
}

// Batch rename ----------------------------------------------------------------

// openTool selects names in the left pane and opens the batch tool on them.
func openTool(t *testing.T, m tea.Model, names ...string) tea.Model {
	t.Helper()
	for _, n := range names {
		m = focusEntry(t, m, n)
		m = press(t, m, "space")
	}
	m = press(t, m, "M")
	if got := m.(Model).mode; got != modeRename {
		t.Fatalf("mode = %v, want modeRename", got)
	}
	if m.(Model).ren.loading {
		t.Fatal("the candidate sweep did not finish")
	}
	return m
}

// focusField tabs to a field and clears it, the way a user would.
func focusField(t *testing.T, m tea.Model, f renameField) tea.Model {
	t.Helper()
	for range renFieldCount {
		if m.(Model).ren.focus == f {
			break
		}
		m = press(t, m, "tab")
	}
	if got := m.(Model).ren.focus; got != f {
		t.Fatalf("focus = %v, want %v", got, f)
	}
	for range len(m.(Model).ren.fields[f].Value()) {
		m = press(t, m, "backspace")
	}
	if got := m.(Model).ren.fields[f].Value(); got != "" {
		t.Fatalf("field = %q, want it cleared", got)
	}
	return m
}

func TestBatchRenameAppliesAPattern(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"IMG_0001.jpg", "IMG_0002.jpg", "notes.txt"} {
		write(t, filepath.Join(dir, n))
	}
	m := newInDir(t, dir)
	m = openTool(t, m, "IMG_0001.jpg", "IMG_0002.jpg")

	m = typeKeys(t, m, `IMG_(\d+)`)
	m = press(t, m, "tab")
	m = typeKeys(t, m, "holiday-$1")

	app := m.(Model)
	if app.ren.summary.Renamed != 2 {
		t.Fatalf("summary = %+v, want 2 renames", app.ren.summary)
	}
	if view := app.View(); !strings.Contains(view, "holiday-0001.jpg") {
		t.Error("the preview does not show the new name")
	}

	m = press(t, m, "enter")
	if got := m.(Model).mode; got != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", got)
	}
	if view := m.(Model).View(); !strings.Contains(view, "Rename 2 items") {
		t.Errorf("the confirm prompt does not say what it will do:\n%s", view)
	}

	m = press(t, m, "y")

	for _, want := range []string{"holiday-0001.jpg", "holiday-0002.jpg", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("stat %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "IMG_0001.jpg")); err == nil {
		t.Error("the original is still there")
	}
	if got := m.(Model).mode; got != modeNormal {
		t.Errorf("mode = %v, want modeNormal once the job is done", got)
	}
}

func TestBatchRenameIsRecursiveByDefault(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pics", "a-1.txt"))
	write(t, filepath.Join(dir, "pics", "sub", "a-2.txt"))
	m := newInDir(t, dir)
	m = openTool(t, m, "pics")

	if got := len(m.(Model).ren.cands); got != 2 {
		t.Fatalf("collected %d candidates, want both levels", got)
	}

	m = typeKeys(t, m, "a-")
	m = press(t, m, "ctrl+o") // regex → literal, so the pattern is plain text
	m = press(t, m, "tab")
	m = typeKeys(t, m, "b-")
	m = press(t, m, "enter")
	press(t, m, "y")

	for _, want := range []string{"pics/b-1.txt", "pics/sub/b-2.txt"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err != nil {
			t.Errorf("stat %s: %v", want, err)
		}
	}
}

// A directory name is only renamed when the tool is told to, so a recursive
// pattern cannot reshape the tree by accident.
func TestBatchRenameDirsIsOptIn(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "v1", "v1-notes.txt"))
	m := newInDir(t, dir)
	m = openTool(t, m, "v1")

	m = press(t, m, "ctrl+o") // literal
	m = typeKeys(t, m, "v1")
	m = press(t, m, "tab")
	m = typeKeys(t, m, "v2")

	if got := m.(Model).ren.summary.Total; got != 1 {
		t.Fatalf("candidates = %d, want only the file inside", got)
	}

	m = press(t, m, "ctrl+y") // include directories
	if got := m.(Model).ren.summary.Total; got != 2 {
		t.Fatalf("candidates = %d, want the directory too", got)
	}

	m = press(t, m, "enter")
	press(t, m, "y")

	if _, err := os.Stat(filepath.Join(dir, "v2", "v2-notes.txt")); err != nil {
		t.Errorf("stat v2/v2-notes.txt: %v", err)
	}
}

func TestBatchRenameToggleRecursiveRecollects(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pics", "a.txt"))
	write(t, filepath.Join(dir, "pics", "sub", "b.txt"))
	m := newInDir(t, dir)
	m = openTool(t, m, "pics")

	m = press(t, m, "ctrl+r") // recursion off: a directory alone has nothing to rename
	if got := len(m.(Model).ren.cands); got != 0 {
		t.Errorf("collected %d candidates, want none", got)
	}
	m = press(t, m, "ctrl+r")
	if got := len(m.(Model).ren.cands); got != 2 {
		t.Errorf("collected %d candidates, want both files back", got)
	}
}

func TestBatchRenameMasksAndCounter(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"one.txt", "two.txt"} {
		write(t, filepath.Join(dir, n))
	}
	m := newInDir(t, dir)
	m = openTool(t, m, "one.txt", "two.txt")

	// Straight to the name mask: shot-001.txt, shot-002.txt.
	m = focusField(t, m, renNameMask)
	m = typeKeys(t, m, "shot-[C3]")

	if got := m.(Model).ren.summary.Renamed; got != 2 {
		t.Fatalf("summary = %+v, want 2 renames", m.(Model).ren.summary)
	}
	m = press(t, m, "enter")
	press(t, m, "y")

	for _, want := range []string{"shot-001.txt", "shot-002.txt"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("stat %s: %v", want, err)
		}
	}
}

// A name already taken is shown as blocked and left alone; the rest of the batch
// still runs.
func TestBatchRenameHoldsBackConflicts(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "x-a.txt"} {
		write(t, filepath.Join(dir, n))
	}
	m := newInDir(t, dir)
	m = openTool(t, m, "a.txt", "b.txt")

	m = focusField(t, m, renNameMask)
	m = typeKeys(t, m, "x-[N]")

	sum := m.(Model).ren.summary
	if sum.Renamed != 1 || sum.Conflicts != 1 {
		t.Fatalf("summary = %+v, want one rename and one conflict", sum)
	}
	if view := m.(Model).View(); !strings.Contains(view, "exists") {
		t.Error("the preview does not flag the clash")
	}

	m = press(t, m, "enter")
	press(t, m, "y")

	if data, _ := os.ReadFile(filepath.Join(dir, "x-a.txt")); string(data) != "x-a.txt" {
		t.Errorf("x-a.txt = %q, want it untouched", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "x-b.txt")); err != nil {
		t.Errorf("the rest of the batch did not run: %v", err)
	}
}

// A pattern that does not compile says so and cannot be applied.
func TestBatchRenameBadRegexIsReported(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"))
	m := newInDir(t, dir)
	m = openTool(t, m, "a.txt")

	m = typeKeys(t, m, "([")

	got := m.(Model)
	if got.ren.status == "" {
		t.Fatal("want a status explaining the pattern")
	}
	if !strings.Contains(got.View(), "regex") {
		t.Errorf("the render does not name the problem: %q", got.ren.status)
	}

	m = press(t, m, "enter")
	if mode := m.(Model).mode; mode != modeRename {
		t.Errorf("mode = %v, want to stay in the tool", mode)
	}
}

func TestBatchRenameNothingToDo(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"))
	m := newInDir(t, dir)
	m = openTool(t, m, "a.txt")

	m = press(t, m, "enter")
	got := m.(Model)
	if got.mode != modeRename {
		t.Errorf("mode = %v, want to stay in the tool", got.mode)
	}
	if !strings.Contains(got.ren.status, "nothing to rename") {
		t.Errorf("status = %q, want it to say there is nothing to do", got.ren.status)
	}
}

// Cancelling the confirm goes back to the tool with the form intact — trying
// patterns out is the whole point of the preview.
func TestBatchRenameCancelReturnsToTheTool(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"))
	m := newInDir(t, dir)
	m = openTool(t, m, "a.txt")

	m = press(t, m, "ctrl+o") // literal
	m = typeKeys(t, m, "a")
	m = press(t, m, "tab")
	m = typeKeys(t, m, "z")
	m = press(t, m, "enter")
	m = press(t, m, "n")

	got := m.(Model)
	if got.mode != modeRename {
		t.Fatalf("mode = %v, want modeRename", got.mode)
	}
	if v := got.ren.fields[renFind].Value(); v != "a" {
		t.Errorf("find = %q, want the form kept", v)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Error("cancelling renamed it anyway")
	}
}

func TestBatchRenameEscapeClosesTheTool(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"))
	m := newInDir(t, dir)
	m = openTool(t, m, "a.txt")
	m = press(t, m, "esc")

	got := m.(Model)
	if got.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", got.mode)
	}
	if got.ren.open {
		t.Error("the tool state was left behind")
	}
}

func TestBatchRenameNeedsASelection(t *testing.T) {
	dir := t.TempDir()
	m := newInDir(t, dir)
	// Cursor is on "..", which is never a candidate.
	m = press(t, m, "M")

	got := m.(Model)
	if got.mode != modeRename {
		if !strings.Contains(got.errText, "select") {
			t.Errorf("errText = %q, want it to ask for a selection", got.errText)
		}
		return
	}
	t.Error("the tool opened with nothing to work on")
}

// The replacement field takes the same tokens the masks do, and the tool says so
// while it has focus.
func TestBatchRenameReplacementTokens(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a-one.txt", "a-two.txt"} {
		write(t, filepath.Join(dir, n))
	}
	m := newInDir(t, dir)
	m = openTool(t, m, "a-one.txt", "a-two.txt")

	m = press(t, m, "ctrl+o") // literal
	m = typeKeys(t, m, "a")
	m = press(t, m, "tab")
	m = typeKeys(t, m, "v[C2]")

	app := m.(Model)
	if view := app.View(); !strings.Contains(view, "v01-one.txt") || !strings.Contains(view, "v02-two.txt") {
		t.Errorf("the preview does not show the expanded counter:\n%s", view)
	}
	if !strings.Contains(app.View(), "[C] counter") {
		t.Error("the token legend is not shown while replace has focus")
	}

	m = press(t, m, "enter")
	press(t, m, "y")

	for _, want := range []string{"v01-one.txt", "v02-two.txt"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("stat %s: %v", want, err)
		}
	}
}

// Undo ------------------------------------------------------------------------

// renameBatchTo runs a literal find/replace over the selection and applies it.
func renameBatchTo(t *testing.T, m tea.Model, find, replace string, names ...string) tea.Model {
	t.Helper()
	m = openTool(t, m, names...)
	m = press(t, m, "ctrl+o") // literal, so the test's patterns are plain text
	m = typeKeys(t, m, find)
	m = press(t, m, "tab")
	m = typeKeys(t, m, replace)
	m = press(t, m, "enter")
	return press(t, m, "y")
}

func TestUndoPutsABatchBack(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a-1.txt", "a-2.txt"} {
		write(t, filepath.Join(dir, n))
	}
	m := newInDir(t, dir)
	m = renameBatchTo(t, m, "a-", "b-", "a-1.txt", "a-2.txt")

	if got := len(m.(Model).undoStack); got != 1 {
		t.Fatalf("undo stack has %d entries, want 1", got)
	}
	if notice := m.(Model).noticeText; !strings.Contains(notice, "ctrl+z") {
		t.Errorf("notice = %q, want it to mention undo", notice)
	}

	m = press(t, m, "ctrl+z")
	if got := m.(Model).mode; got != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", got)
	}
	if view := m.(Model).View(); !strings.Contains(view, "Undo rename") {
		t.Errorf("the prompt does not say what it will do:\n%s", view)
	}
	m = press(t, m, "y")

	for _, want := range []string{"a-1.txt", "a-2.txt"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("stat %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "b-1.txt")); err == nil {
		t.Error("the renamed name is still there")
	}
	if got := len(m.(Model).undoStack); got != 0 {
		t.Errorf("undo stack has %d entries, want it consumed", got)
	}
}

func TestUndoOfASingleRename(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "old.txt"))
	m := newInDir(t, dir)
	m = focusEntry(t, m, "old.txt")
	m = press(t, m, "r")

	app := m.(Model)
	app.renOne.input.SetValue("new.txt")
	m = app
	m = press(t, m, "enter")

	if got := len(m.(Model).undoStack); got != 1 {
		t.Fatalf("undo stack has %d entries, want the single rename recorded", got)
	}

	m = press(t, m, "ctrl+z")
	press(t, m, "y")

	if _, err := os.Stat(filepath.Join(dir, "old.txt")); err != nil {
		t.Errorf("stat old.txt: %v", err)
	}
}

// The hard case: a recursive batch that renamed directories as well as their
// contents has to come apart in the opposite order it went together.
func TestUndoOfARecursiveDirectoryBatch(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "v1", "v1-notes.txt"))
	write(t, filepath.Join(dir, "v1", "v1-sub", "v1-deep.txt"))
	m := newInDir(t, dir)

	m = openTool(t, m, "v1")
	m = press(t, m, "ctrl+y") // rename directories too
	m = press(t, m, "ctrl+o") // literal
	m = typeKeys(t, m, "v1")
	m = press(t, m, "tab")
	m = typeKeys(t, m, "v2")
	m = press(t, m, "enter")
	m = press(t, m, "y")

	if _, err := os.Stat(filepath.Join(dir, "v2", "v2-sub", "v2-deep.txt")); err != nil {
		t.Fatalf("the batch did not land: %v", err)
	}

	m = press(t, m, "ctrl+z")
	m = press(t, m, "y")

	if got := m.(Model).errText; got != "" {
		t.Errorf("errText = %q, want the undo to be clean", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "v1", "v1-sub", "v1-deep.txt")); err != nil {
		t.Errorf("stat v1/v1-sub/v1-deep.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "v2")); err == nil {
		t.Error("v2 is still there")
	}
}

// Several batches walk back one at a time, newest first.
func TestUndoWalksBackThroughTheHistory(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"))
	m := newInDir(t, dir)

	m = renameBatchTo(t, m, "a", "b", "a.txt")
	m = renameBatchTo(t, m, "b", "c", "b.txt")
	if got := len(m.(Model).undoStack); got != 2 {
		t.Fatalf("undo stack has %d entries, want 2", got)
	}

	m = press(t, m, "ctrl+z")
	m = press(t, m, "y")
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatalf("first undo did not land: %v", err)
	}

	m = press(t, m, "ctrl+z")
	m = press(t, m, "y")
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Errorf("second undo did not land: %v", err)
	}
	if got := len(m.(Model).undoStack); got != 0 {
		t.Errorf("undo stack has %d entries, want it empty", got)
	}
}

func TestUndoWithNothingRecorded(t *testing.T) {
	m := newInDir(t, t.TempDir())
	m = press(t, m, "ctrl+z")

	got := m.(Model)
	if got.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", got.mode)
	}
	if !strings.Contains(got.errText, "nothing to undo") {
		t.Errorf("errText = %q, want it to say there is nothing to undo", got.errText)
	}
}

// A name that moved on since the rename cannot be put back. The dead batch is
// dropped so it does not sit on top of the history blocking the ones under it.
func TestUndoOfAStaleBatchIsRefusedAndDropped(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"))
	m := newInDir(t, dir)
	m = renameBatchTo(t, m, "a", "b", "a.txt")

	if err := os.Rename(filepath.Join(dir, "b.txt"), filepath.Join(dir, "elsewhere.txt")); err != nil {
		t.Fatal(err)
	}

	m = press(t, m, "ctrl+z")
	got := m.(Model)
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want the undo refused outright", got.mode)
	}
	if !strings.Contains(got.errText, "moved on") {
		t.Errorf("errText = %q, want it to explain the refusal", got.errText)
	}
	if len(got.undoStack) != 0 {
		t.Error("the dead batch is still on the stack")
	}
	if _, err := os.Stat(filepath.Join(dir, "elsewhere.txt")); err != nil {
		t.Error("the file was disturbed")
	}
}

// Cancelling the undo prompt leaves the history alone, so it can still be undone
// later.
func TestUndoCancelKeepsTheHistory(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"))
	m := newInDir(t, dir)
	m = renameBatchTo(t, m, "a", "b", "a.txt")

	m = press(t, m, "ctrl+z")
	m = press(t, m, "n")

	got := m.(Model)
	if got.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", got.mode)
	}
	if len(got.undoStack) != 1 {
		t.Errorf("undo stack has %d entries, want it kept", len(got.undoStack))
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Error("cancelling undid it anyway")
	}
}

func TestUndoStackIsBounded(t *testing.T) {
	var m Model
	for i := range renameUndoDepth + 5 {
		m.pushUndo("/root", []rename.Change{{Rel: fmt.Sprintf("%d.txt", i), New: "x.txt", Changed: true}})
	}
	if got := len(m.undoStack); got != renameUndoDepth {
		t.Errorf("undo stack has %d entries, want it capped at %d", got, renameUndoDepth)
	}
	// The oldest are the ones dropped.
	if got := m.undoStack[0].changes[0].Rel; got != "5.txt" {
		t.Errorf("oldest kept entry = %s, want 5.txt", got)
	}
}

func TestPushUndoIgnoresAnEmptyBatch(t *testing.T) {
	var m Model
	m.pushUndo("/root", nil)
	if len(m.undoStack) != 0 {
		t.Error("an empty batch should not be recorded")
	}
}
