package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Theme != "" {
		t.Fatalf("Theme = %q, want empty", cfg.Theme)
	}
}

func TestSaveThenLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Save(Config{Theme: "nord"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Theme != "nord" {
		t.Fatalf("Theme = %q, want nord", cfg.Theme)
	}

	path, _ := Path()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file: %v", err)
	}
	if want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tyr", "config"); path != want {
		t.Fatalf("Path() = %q, want %q", path, want)
	}
}

func TestSaveKeepsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, _ := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# hand written\nfuture_option = 42\ntheme = dracula\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Save(Config{Theme: "gruvbox"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	pairs, err := readPairs(path)
	if err != nil {
		t.Fatal(err)
	}
	if pairs["theme"] != "gruvbox" {
		t.Fatalf("theme = %q, want gruvbox", pairs["theme"])
	}
	if pairs["future_option"] != "42" {
		t.Fatalf("unknown key dropped: %v", pairs)
	}
}

// TestReadPairsIgnoresJunk also pins the case rule: keys are stored exactly as
// written, because a connection name is a display label, and scalar settings are
// looked up case-insensitively instead.
func TestReadPairsIgnoresJunk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	body := "\n  # comment\nTHEME  =  nord  \nnonsense line\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	pairs, err := readPairs(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs["THEME"] != "nord" {
		t.Fatalf("pairs = %v", pairs)
	}
	if got := lookup(pairs, "theme"); got != "nord" {
		t.Fatalf("lookup = %q", got)
	}
}
