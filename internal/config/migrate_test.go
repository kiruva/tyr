package config

import (
	"os"
	"path/filepath"
	"testing"
)

// seedLegacy writes a config file under the pre-rename directory and returns
// that directory.
func seedLegacy(t *testing.T, body string) string {
	t.Helper()
	dir, err := legacyDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMigrateMovesLegacyDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	old := seedLegacy(t, "theme = nord\nconn.prod.host = example.com\n")

	mig, err := Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if mig == nil {
		t.Fatal("Migrate reported no move, want one")
	}
	if mig.From != old {
		t.Fatalf("From = %q, want %q", mig.From, old)
	}

	// The settings are readable under the new name...
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Theme != "nord" {
		t.Fatalf("Theme = %q, want nord", cfg.Theme)
	}
	conns, err := Connections()
	if err != nil {
		t.Fatalf("load connections: %v", err)
	}
	if len(conns) != 1 || conns[0].Host != "example.com" {
		t.Fatalf("connections = %+v, want the seeded one", conns)
	}

	// ...and nothing is left at the old location.
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Fatalf("legacy dir still present: %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	seedLegacy(t, "theme = nord\n")

	if _, err := Migrate(); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	mig, err := Migrate()
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if mig != nil {
		t.Fatalf("second Migrate moved %+v, want no-op", mig)
	}
}

// The guard that matters most: an existing config under the new name is never
// clobbered, and the legacy directory is left alone for the user to deal with.
func TestMigrateKeepsExistingConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	old := seedLegacy(t, "theme = nord\n")
	if err := Save(Config{Theme: "dracula"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	mig, err := Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if mig != nil {
		t.Fatalf("Migrate moved %+v over an existing config", mig)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Theme != "dracula" {
		t.Fatalf("Theme = %q, want the existing dracula", cfg.Theme)
	}
	if _, err := os.Lstat(old); err != nil {
		t.Fatalf("legacy dir should be left untouched: %v", err)
	}
}

func TestMigrateWithNothingToDo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	mig, err := Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if mig != nil {
		t.Fatalf("Migrate = %+v, want no-op on a fresh machine", mig)
	}
}

// A file (not a directory) sitting at the legacy path is somebody else's, and
// moving it would be a surprise.
func TestMigrateIgnoresLegacyFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	old, err := legacyDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	mig, err := Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if mig != nil {
		t.Fatalf("Migrate = %+v, want no-op", mig)
	}
	if fi, err := os.Lstat(old); err != nil || fi.IsDir() {
		t.Fatalf("legacy file disturbed: %v", err)
	}
}

// Without XDG_CONFIG_HOME both paths fall back to the platform's own config
// base, which is the case most users will actually hit.
func TestMigrateUsesHomeFallback(t *testing.T) {
	base := setFallbackBase(t)

	old := filepath.Join(base, legacyName)
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "config"), []byte("theme = gruvbox\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	mig, err := Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if mig == nil {
		t.Fatal("Migrate reported no move, want one")
	}
	if want := filepath.Join(base, dirName); mig.To != want {
		t.Fatalf("To = %q, want %q", mig.To, want)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Theme != "gruvbox" {
		t.Fatalf("Theme = %q, want gruvbox", cfg.Theme)
	}
}
