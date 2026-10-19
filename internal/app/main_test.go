package app

import (
	"fmt"
	"os"
	"testing"
)

// TestMain gives the package a home of its own. An undo of a copy sends the
// copies to the trash, and a test that forgot isolateTrash would otherwise put
// its files in the real one — and read the real config while it was at it.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "tyr-app-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, v := range []string{"HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "LOCALAPPDATA", "APPDATA"} {
		_ = os.Setenv(v, home)
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
