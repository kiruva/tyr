package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

// setFallbackBase clears XDG_CONFIG_HOME, points the platform's own default
// config base at a temp directory, and returns that base: ~/.config on Unix,
// %AppData% on Windows. It is what a user without XDG_CONFIG_HOME hits.
func setFallbackBase(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", "")

	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		// os.UserHomeDir reads %USERPROFILE% here, and os.UserConfigDir %AppData%.
		t.Setenv("USERPROFILE", home)
		base := filepath.Join(home, "AppData", "Roaming")
		t.Setenv("AppData", base)
		return base
	}
	return filepath.Join(home, ".config")
}
