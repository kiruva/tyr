package app

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/remote"
)

// The tests here pin one race shared by every tool that works off the UI
// thread: a run is cancelled and another started before the first one's reply
// arrives. The late reply belongs to a run nobody is waiting for any more. It
// must be dropped — not shown as the new run's result, and not allowed to
// cancel or forget the run that is still going.

// A cancelled search's reply does not end the search started after it.
func TestFindDropsAStaleReply(t *testing.T) {
	dir := t.TempDir()
	seedFiles(t, dir, "one.go")

	m := newInDir(t, dir)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = typeKeys(t, m, "*.go")
	m, first := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // back to the form
	m, second := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.(Model).find.stage; got != findStageRunning {
		t.Fatalf("stage = %v, want the second search running", got)
	}

	m = runCmd(t, m, first)
	app := m.(Model)
	if app.find.stage != findStageRunning {
		t.Fatalf("stage = %v (status %q), want the second search still running", app.find.stage, app.find.status)
	}
	if app.find.task.cancel == nil {
		t.Fatal("the stale reply dropped the running search's cancel")
	}

	m = runCmd(t, m, second)
	if app := m.(Model); app.find.stage != findStageResults || len(app.find.results) != 1 {
		t.Fatalf("stage = %v with %d hits, want the second search's one hit", app.find.stage, len(app.find.results))
	}
}

// A cancelled command's output is not shown as the next command's, and its
// arrival does not stop the command that is running now.
func TestCommandDropsAStaleReply(t *testing.T) {
	m := newInDir(t, t.TempDir())

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = typeKeys(t, m, "echo first")
	m, first := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // kill it

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = typeKeys(t, m, "echo second")
	m, second := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.(Model).mode; got != modeRunning {
		t.Fatalf("mode = %v, want the second command running", got)
	}

	m = runCmd(t, m, first)
	app := m.(Model)
	if app.mode != modeRunning {
		t.Fatalf("mode = %v, want the second command still running", app.mode)
	}
	if app.command.task.cancel == nil {
		t.Fatal("the stale reply cancelled the running command")
	}

	m = runCmd(t, m, second)
	app = m.(Model)
	if app.mode != modeView || !strings.Contains(app.viewTitle, "second") {
		t.Fatalf("mode = %v title = %q, want the second command's output", app.mode, app.viewTitle)
	}
}

// A comparison cancelled by closing the tool does not close the one opened
// after it.
func TestSyncDropsAStaleReply(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	seedFiles(t, left, "only-left.txt")

	m := twoPanes(t, left, right)
	m, first := m.Update(tea.KeyMsg{Type: tea.KeyF9})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m, second := m.Update(tea.KeyMsg{Type: tea.KeyF9})

	m = runCmd(t, m, first)
	app := m.(Model)
	if app.mode != modeSync || app.sync.stage != syncStageRunning {
		t.Fatalf("mode = %v stage = %v, want the second comparison still running", app.mode, app.sync.stage)
	}
	if app.sync.task.cancel == nil {
		t.Fatal("the stale reply dropped the running comparison's cancel")
	}

	m = runCmd(t, m, second)
	if app := m.(Model); app.sync.stage != syncStageList || len(app.sync.rows) != 1 {
		t.Fatalf("stage = %v with %d rows, want the second comparison's one row", app.sync.stage, len(app.sync.rows))
	}
}

// Toggling recursion off while the recursive sweep is still out must not let
// that sweep land last: its subdirectory files would then be renamed by a plan
// that says recursion is off.
func TestRenameDropsAStaleSweep(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pics", "a.txt"))
	write(t, filepath.Join(dir, "pics", "sub", "b.txt"))
	m := newInDir(t, dir)
	m = openTool(t, m, "pics")

	m = press(t, m, "ctrl+r")             // off
	m, recursive := keyPress(m, "ctrl+r") // on, sweep still out
	m, flat := keyPress(m, "ctrl+r")      // off again

	m = runCmd(t, m, flat)
	m = runCmd(t, m, recursive)

	app := m.(Model)
	if app.ren.opt.Recursive {
		t.Fatal("recursion is on, want off")
	}
	if got := len(app.ren.cands); got != 0 {
		t.Errorf("collected %d candidates, want none: the recursive sweep landed after the flat one", got)
	}
	if app.ren.collect.running() {
		t.Error("still loading after the current sweep landed")
	}
	if app.ren.status != "" {
		t.Errorf("status = %q, want none: the superseded sweep's reply was shown", app.ren.status)
	}
}

// A remote create that finishes after the pane has moved on does not refresh
// or highlight in the directory the pane is in now.
func TestRemoteCreateAfterThePaneMoved(t *testing.T) {
	m := newInDir(t, t.TempDir())
	app := m.(Model)
	h := remote.Host{Name: "box"}
	remotePane(t, &app, 0, h, "/srv")

	// What createRemoteCmd sends back for a folder made in /srv.
	done := createdMsg{pane: 0, host: h, dir: "/srv", name: "logs", isDir: true}

	moved := app
	remotePane(t, &moved, 0, h, "/var")
	if _, cmd := moved.Update(done); cmd != nil {
		t.Error("the create refreshed a directory it did not happen in")
	}

	other := app
	remotePane(t, &other, 0, remote.Host{Name: "elsewhere"}, "/srv")
	if _, cmd := other.Update(done); cmd != nil {
		t.Error("the create refreshed the same path on another host")
	}

	if _, cmd := app.Update(done); cmd == nil {
		t.Error("the create did not refresh the directory it happened in")
	}
}
