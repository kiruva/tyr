package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
)

// progressModel puts the app into progress mode with a live job attached, the
// way startPending leaves it, and hands back the cancel it installed.
func progressModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	m := newInDir(t, dir).(Model)
	m.pending = fileops.Job{Op: fileops.OpCopy}
	m.mode = modeProgress
	return m
}

// Esc asks the running job to stop. The mode does not change here: the job
// reports what it managed to do through its Result, and finishOp closes up.
func TestEscCancelsARunningOperation(t *testing.T) {
	m := progressModel(t)
	stopped := false
	m.cancelOp = func() { stopped = true }

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app := next.(Model)

	if !stopped {
		t.Fatal("Esc did not cancel the job")
	}
	if !app.cancelling {
		t.Error("cancelling = false, want true so the dialog can say so")
	}
	if app.mode != modeProgress {
		t.Errorf("mode = %v, want modeProgress until the job reports back", app.mode)
	}
}

// Ctrl+C stops the job rather than quitting: leaving mid-write would abandon a
// half-copied file with nothing in the undo history to say so.
func TestCtrlCCancelsRatherThanQuitting(t *testing.T) {
	m := progressModel(t)
	stopped := false
	m.cancelOp = func() { stopped = true }

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	app := next.(Model)

	if !stopped {
		t.Fatal("Ctrl+C did not cancel the job")
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Fatal("Ctrl+C quit mid-operation instead of stopping the job")
		}
	}
	if app.mode != modeProgress {
		t.Errorf("mode = %v, want modeProgress", app.mode)
	}
}

// Cancelling twice must not call the cancel func a second time, and must not
// re-arm anything: the job is already unwinding.
func TestCancelIsOnlyRequestedOnce(t *testing.T) {
	m := progressModel(t)
	calls := 0
	m.cancelOp = func() { calls++ }

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})

	if calls != 1 {
		t.Fatalf("cancel called %d times, want 1", calls)
	}
	if !next.(Model).cancelling {
		t.Error("cancelling was cleared by the second press")
	}
}

// Every other key stays locked out while an operation runs, so a stray press
// cannot start a second job on top of the one in flight.
func TestOtherKeysStayLockedDuringAnOperation(t *testing.T) {
	m := progressModel(t)
	m.cancelOp = func() { t.Fatal("an unrelated key cancelled the job") }

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'d'}},
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyEnter},
		{Type: tea.KeyTab},
	} {
		next, _ := m.Update(key)
		if app := next.(Model); app.mode != modeProgress {
			t.Fatalf("%v changed the mode to %v", key, app.mode)
		}
	}
}

// finishOp releases the job's context however the job ended, so it never
// outlives the work it was made for.
func TestFinishOpReleasesTheContext(t *testing.T) {
	m := progressModel(t)
	released := false
	m.cancelOp = func() { released = true }
	m.cancelling = true

	next, _ := m.finishOp(fileops.Result{Op: fileops.OpCopy})
	app := next.(Model)

	if !released {
		t.Error("finishOp did not release the context")
	}
	if app.cancelOp != nil {
		t.Error("cancelOp outlived the job")
	}
	if app.cancelling {
		t.Error("cancelling was left set")
	}
}

// A cancelled job is a decision, not a failure: it reads as a notice, not as a
// red error bar. This is the same wording a cancel at the collision prompt gets.
func TestCancelledJobReadsAsANoticeNotAnError(t *testing.T) {
	m := progressModel(t)
	m.cancelOp = func() {}

	next, _ := m.finishOp(fileops.Result{Op: fileops.OpCopy, Err: fileops.ErrCancelled})
	app := next.(Model)

	if app.errText != "" {
		t.Errorf("errText = %q, want it empty: cancelling is not a failure", app.errText)
	}
	if !strings.Contains(app.noticeText, "cancelled") {
		t.Errorf("noticeText = %q, want it to say the copy was cancelled", app.noticeText)
	}
}

// The progress dialog has to offer the way out, and say when it has been asked
// — a job stops between files, so there is a moment before it is really over.
func TestProgressDialogShowsTheCancelAffordance(t *testing.T) {
	m := progressModel(t)
	m.width, m.height = 100, 24

	if view := m.renderProgress(); !strings.Contains(view, "esc") {
		t.Errorf("the progress dialog does not offer esc:\n%s", view)
	}

	m.cancelling = true
	if view := m.renderProgress(); !strings.Contains(view, "Stopping") {
		t.Errorf("a cancelling job does not say so:\n%s", view)
	}
}
