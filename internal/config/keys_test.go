package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeysRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Save(Config{Theme: "nord"}); err != nil {
		t.Fatalf("save theme: %v", err)
	}
	binds := map[string][]string{
		"copy":   {"f5", "c"},
		"select": {"space"},
	}
	if err := SaveKeys(binds); err != nil {
		t.Fatalf("save keys: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Theme != "nord" {
		t.Errorf("Theme = %q — writing keys disturbed it", cfg.Theme)
	}
	if got := cfg.Keys["copy"]; len(got) != 2 || got[0] != "f5" || got[1] != "c" {
		t.Errorf("copy = %v, want [f5 c]", got)
	}
	if got := cfg.Keys["select"]; len(got) != 1 || got[0] != "space" {
		t.Errorf("select = %v, want [space]", got)
	}
}

// Saving replaces the whole set, so an action taken out of the map goes back to
// its default rather than keeping a stale line.
func TestSaveKeysReplaces(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := SaveKeys(map[string][]string{"copy": {"y"}, "move": {"z"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveKeys(map[string][]string{"copy": {"y"}}); err != nil {
		t.Fatal(err)
	}

	cfg, _ := Load()
	if _, still := cfg.Keys["move"]; still {
		t.Error("move is still rebound after being left out")
	}
	if got := cfg.Keys["copy"]; len(got) != 1 || got[0] != "y" {
		t.Errorf("copy = %v, want [y]", got)
	}

	// Nothing rebound leaves no key lines behind at all.
	if err := SaveKeys(nil); err != nil {
		t.Fatal(err)
	}
	path, _ := Path()
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "key.") {
		t.Errorf("the config still has key lines:\n%s", body)
	}
}

func TestKeysFromHandwrittenFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, _ := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "key.copy = f5, c\nkey.SELECT = space\nkey.empty =\ntheme = nord\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Keys["copy"]; len(got) != 2 || got[1] != "c" {
		t.Errorf("copy = %v, want the spaces trimmed", got)
	}
	if _, ok := cfg.Keys["select"]; !ok {
		t.Error("an upper-case action name was not read")
	}
	if _, ok := cfg.Keys["empty"]; ok {
		t.Error("an action bound to nothing was read as a binding")
	}
}

func TestMouseSetting(t *testing.T) {
	if !(Config{}).MouseEnabled() {
		t.Error("the mouse should be on unless it is turned off")
	}
	if (Config{Mouse: "OFF"}).MouseEnabled() {
		t.Error("mouse = off should turn it off")
	}
	if !(Config{Mouse: "on"}).MouseEnabled() {
		t.Error("mouse = on should leave it on")
	}
}
