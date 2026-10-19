package app

import "testing"

func TestBgTask(t *testing.T) {
	var task bgTask
	if task.finish(0) {
		t.Fatal("an idle task accepted a reply")
	}

	ctx, first := task.start()
	_, second := task.start()
	if ctx.Err() == nil {
		t.Error("starting again did not cancel the run before it")
	}
	if task.finish(first) {
		t.Error("the superseded run's reply was accepted")
	}
	if !task.running() {
		t.Error("a rejected reply ended the run in flight")
	}
	if !task.finish(second) {
		t.Fatal("the current run's reply was refused")
	}
	if task.running() || task.finish(second) {
		t.Error("the same reply was accepted twice")
	}

	// A tool zeroes its state when it closes; the next run must not reuse a
	// number a reply from before the close can still carry.
	_, third := task.start()
	task = bgTask{}
	_, fourth := task.start()
	if third == fourth {
		t.Errorf("seq %d reused after the state was zeroed", third)
	}
	task.stop()
	if task.finish(fourth) {
		t.Error("a stopped run's reply was accepted")
	}
}
