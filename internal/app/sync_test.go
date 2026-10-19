package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/compare"
)

// openSyncOn compares two prepared directories and returns the tool with its
// rows loaded.
func openSyncOn(t *testing.T, left, right string) tea.Model {
	t.Helper()
	m := twoPanes(t, left, right)
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF9})
	m = drain(t, m, cmd)

	if app := m.(Model); app.mode != modeSync || app.sync.stage != syncStageList {
		t.Fatalf("mode = %v stage = %v, want the sync list", app.mode, app.sync.stage)
	}
	return m
}

// The tool proposes a direction for every difference: missing files go across,
// and the newer side wins where both have one.
func TestSyncProposesDirections(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	writeAged(t, filepath.Join(left, "only-left.txt"), "x", 0)
	writeAged(t, filepath.Join(right, "only-right.txt"), "x", 0)
	writeAged(t, filepath.Join(left, "newer.txt"), "left is newer", 0)
	writeAged(t, filepath.Join(right, "newer.txt"), "right is older, and longer", time.Hour)
	writeAged(t, filepath.Join(left, "same.txt"), "identical", time.Hour)
	writeAged(t, filepath.Join(right, "same.txt"), "identical", time.Hour)

	m := openSyncOn(t, left, right)
	app := m.(Model)

	actions := map[string]syncAction{}
	for _, r := range app.sync.rows {
		actions[r.pair.Rel] = r.action
	}
	if got := actions["only-left.txt"]; got != syncToRight {
		t.Errorf("only-left.txt = %v, want copy to the right", got)
	}
	if got := actions["only-right.txt"]; got != syncToLeft {
		t.Errorf("only-right.txt = %v, want copy to the left", got)
	}
	if got := actions["newer.txt"]; got != syncToRight {
		t.Errorf("newer.txt = %v, want the newer left side to win", got)
	}
	if got := actions["same.txt"]; got != syncSkip {
		t.Errorf("same.txt = %v, want it left alone", got)
	}

	// Equal rows are hidden until asked for.
	if len(app.visibleSyncRows()) != 3 {
		t.Errorf("%d rows listed, want the three that differ", len(app.visibleSyncRows()))
	}
	m = press(t, m, "e")
	if got := len(m.(Model).visibleSyncRows()); got != 4 {
		t.Errorf("%d rows listed with equals shown, want 4", got)
	}
}

// Running the sync copies both ways and overwrites without asking again.
func TestSyncRuns(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	writeAged(t, filepath.Join(left, "a.txt"), "from left", 0)
	writeAged(t, filepath.Join(right, "b.txt"), "from right", 0)
	writeAged(t, filepath.Join(left, "both.txt"), "left wins", 0)
	writeAged(t, filepath.Join(right, "both.txt"), "right loses, and is longer", time.Hour)

	m := openSyncOn(t, left, right)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // to the confirm prompt

	app := m.(Model)
	if app.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", app.mode)
	}
	if view := app.View(); !strings.Contains(view, "Synchronize") {
		t.Errorf("the prompt does not say what it will do:\n%s", view)
	}

	m = press(t, m, "y")

	if got := readBody(t, filepath.Join(right, "a.txt")); got != "from left" {
		t.Errorf("a.txt on the right = %q", got)
	}
	if got := readBody(t, filepath.Join(left, "b.txt")); got != "from right" {
		t.Errorf("b.txt on the left = %q", got)
	}
	if got := readBody(t, filepath.Join(right, "both.txt")); got != "left wins" {
		t.Errorf("both.txt on the right = %q, want the newer left copy", got)
	}
	if mode := m.(Model).mode; mode != modeNormal {
		t.Errorf("mode = %v after the sync, want modeNormal", mode)
	}
}

// A row can be argued with: → and ← set the direction, s skips.
func TestSyncRowDecisions(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	writeAged(t, filepath.Join(left, "one.txt"), "x", 0)
	writeAged(t, filepath.Join(left, "two.txt"), "x", 0)

	m := openSyncOn(t, left, right)

	// Both rows start as "copy right"; skip the first, which steps to the second.
	m = press(t, m, "s")
	app := m.(Model)
	if app.sync.rows[0].action != syncSkip {
		t.Errorf("row 0 = %v, want skip", app.sync.rows[0].action)
	}
	if app.sync.cursor != 1 {
		t.Errorf("cursor = %d, want it to have stepped down", app.sync.cursor)
	}

	toRight, toLeft := app.syncCounts()
	if toRight != 1 || toLeft != 0 {
		t.Errorf("counts = %d →, %d ←, want 1 and 0", toRight, toLeft)
	}

	// a puts every row back to what was proposed.
	m = press(t, m, "a")
	if got := m.(Model).sync.rows[0].action; got != syncToRight {
		t.Errorf("row 0 after reset = %v, want copy to the right", got)
	}
}

// Two identical directories have nothing to do, and the tool says so.
func TestSyncOnMatchingDirectories(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	writeAged(t, filepath.Join(left, "a.txt"), "same", time.Hour)
	writeAged(t, filepath.Join(right, "a.txt"), "same", time.Hour)

	m := openSyncOn(t, left, right)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.mode != modeSync {
		t.Fatalf("mode = %v, want the tool to stay open", app.mode)
	}
	if !strings.Contains(app.sync.status, "skip") {
		t.Errorf("status = %q, want it to say there is nothing to do", app.sync.status)
	}
}

// Comparing a pane with itself is refused before any walking happens.
func TestSyncRefusesTheSameDirectory(t *testing.T) {
	dir := t.TempDir()
	m := newInDir(t, dir)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyF9})

	app := m.(Model)
	if app.mode == modeSync {
		t.Fatal("the tool opened on one directory compared with itself")
	}
	if !strings.Contains(app.errText, "same directory") {
		t.Errorf("errText = %q, want it to explain", app.errText)
	}
}

// Nested files are compared too, and copied with their directories.
func TestSyncRecursesIntoSubdirectories(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	nested := filepath.Join(left, "sub", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAged(t, filepath.Join(nested, "buried.txt"), "deep", 0)

	m := openSyncOn(t, left, right)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	press(t, m, "y")

	if got := readBody(t, filepath.Join(right, "sub", "deep", "buried.txt")); got != "deep" {
		t.Fatalf("the nested file did not come across: %q", got)
	}
}

// A file on one side where the other has a folder is a conflict row, skipped,
// and cannot be pushed either way: running the sync as proposed leaves the
// folder and what is in it alone.
func TestSyncLeavesFileAgainstFolderAlone(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	writeAged(t, filepath.Join(left, "foo"), "left file", 0)
	if err := os.MkdirAll(filepath.Join(right, "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeAged(t, filepath.Join(right, "foo", "precious.txt"), "right data", 0)
	writeAged(t, filepath.Join(left, "other.txt"), "x", 0) // so there is something to run

	m := openSyncOn(t, left, right)
	for _, r := range m.(Model).sync.rows {
		if r.pair.Kind == compare.Conflict && r.action != syncSkip {
			t.Errorf("%s = %v, want a conflict skipped", r.pair.Rel, r.action)
		}
	}

	// → on the conflict row (the first) does not take.
	m = press(t, m, "l")
	app := m.(Model)
	if got := app.sync.rows[0]; got.pair.Kind != compare.Conflict || got.action != syncSkip {
		t.Errorf("row 0 = %v %v after →, want a conflict still skipped", got.pair.Kind, got.action)
	}
	if app.sync.status == "" {
		t.Error("→ on a conflict said nothing")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	press(t, m, "y")
	if got := readBody(t, filepath.Join(right, "foo", "precious.txt")); got != "right data" {
		t.Errorf("precious.txt = %q, want it untouched", got)
	}
	if got := readBody(t, filepath.Join(right, "other.txt")); got != "x" {
		t.Errorf("other.txt = %q, want the rest of the sync to have run", got)
	}
}

// Two files that differ but carry the same timestamp give no reason to pick a
// side, so neither is picked.
func TestSyncSkipsDifferentFilesWithEqualTimes(t *testing.T) {
	p := compare.Pair{Rel: "a", Kind: compare.Differs, Newer: compare.Neither}
	if got := proposedAction(p); got != syncSkip {
		t.Errorf("proposed %v, want skip", got)
	}
}

// One pane inside the other is refused: a sync would copy the inner tree into
// itself.
func TestSyncRefusesNestedDirectories(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, roots := range [][2]string{{outer, inner}, {inner, outer}} {
		m := twoPanes(t, roots[0], roots[1])
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyF9})
		app := m.(Model)
		if app.mode == modeSync {
			t.Errorf("%v: the tool opened on one directory inside the other", roots)
		}
		if !strings.Contains(app.errText, "inside") {
			t.Errorf("%v: errText = %q, want it to explain", roots, app.errText)
		}
	}
}

// Two panes on the same directory by different routes are the same directory.
func TestSyncRefusesTheSameDirectoryThroughALink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	m := twoPanes(t, real, link)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyF9})
	if app := m.(Model); app.mode == modeSync {
		t.Fatal("the tool opened on one directory compared with itself")
	}
}

func TestErrorIsCancelledUnwraps(t *testing.T) {
	if !errorIsCancelled(fmt.Errorf("walk: %w", context.Canceled)) {
		t.Error("a wrapped context.Canceled was not recognised")
	}
}
