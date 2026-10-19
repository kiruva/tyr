package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The config file kept in a dotfiles repo and linked into place stays linked:
// a save writes the file the link points at, whole, and leaves no temporary
// file beside it.
func TestRewriteKeepsALinkedConfigLinked(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	dotfiles := filepath.Join(t.TempDir(), "tyr.conf")
	if err := os.WriteFile(dotfiles, []byte("theme = dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, path); err != nil {
		t.Skip("no symlinks here:", err)
	}

	if err := rewrite(func(pairs map[string]string) { pairs["x"] = "1" }); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if info, _ := os.Lstat(path); info.Mode()&os.ModeSymlink == 0 {
		t.Error("the link was replaced by a file")
	}
	body, _ := os.ReadFile(dotfiles)
	if !strings.Contains(string(body), "theme = dark") || !strings.Contains(string(body), "x = 1") {
		t.Errorf("linked file = %q, want both settings", body)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("config folder holds %d entries, want only the link", len(entries))
	}
}
