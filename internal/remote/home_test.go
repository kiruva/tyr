package remote

import (
	"runtime"
	"testing"
)

// setHome points the home directory at dir for the rest of the test.
// os.UserHomeDir reads $HOME everywhere except Windows, where it reads
// %USERPROFILE% and ignores $HOME entirely — so a test that sets only $HOME
// reads the real user's ~/.ssh there.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
}
