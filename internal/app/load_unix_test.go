//go:build !windows

package app

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// v on a named pipe used to open it for reading, which waits for a writer that
// may never come — on the UI thread, so the app froze for good. A pipe has
// nothing to show and is refused straight away, for the viewer and the editor.
func TestViewRefusesAPipe(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
		t.Skip("no named pipes here:", err)
	}
	for _, key := range []string{"v", "e"} {
		m := focusEntry(t, newInDir(t, dir), "pipe")
		m, cmd := keyPress(m, key)
		if cmd != nil {
			t.Fatalf("%s: a command was started to read the pipe", key)
		}
		app := m.(Model)
		if app.mode != modeNormal || !strings.Contains(app.errText, "not a regular file") {
			t.Errorf("%s: mode = %v errText = %q, want it refused", key, app.mode, app.errText)
		}
	}
}
