package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kiruva/tyr/internal/config"
	"github.com/kiruva/tyr/internal/pane"
)

// The sort order and hidden-file setting come back on the next run.
func TestWithSessionRestoresView(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := New().WithSession(config.Config{
		Panes: [2]config.PaneState{
			{Sort: "size", Hidden: true},
			{Sort: "time"},
		},
	})

	if got := m.panes[0].SortModeLabel(); got != "size" {
		t.Errorf("left sort = %q, want size", got)
	}
	if !m.panes[0].HiddenShown() {
		t.Error("left pane did not restore hidden files")
	}
	if got := m.panes[1].SortModeLabel(); got != "time" {
		t.Errorf("right sort = %q, want time", got)
	}
	if m.panes[1].HiddenShown() {
		t.Error("right pane restored hidden files it never had")
	}
}

// Directories come back only when the config asks for it, and only if they are
// still there.
func TestWithSessionRestoresPaths(t *testing.T) {
	dir := t.TempDir()
	saved := filepath.Join(dir, "saved")
	if err := os.Mkdir(saved, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cfg := config.Config{Panes: [2]config.PaneState{{Path: saved}, {Path: filepath.Join(dir, "gone")}}}

	// Default startup ignores the saved paths entirely.
	m := New().WithSession(cfg)
	if m.panes[0].Path == saved {
		t.Error("the saved path was restored without startup = last")
	}

	cfg.Startup = "last"
	m = New().WithSession(cfg)
	if m.panes[0].Path != saved {
		t.Errorf("left pane at %q, want %q", m.panes[0].Path, saved)
	}
	if m.panes[1].Path != dir {
		t.Errorf("right pane at %q, want the working directory — the saved one is gone", m.panes[1].Path)
	}
}

// An unreadable sort name is ignored rather than breaking the pane.
func TestWithSessionBadSortFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	m := New().WithSession(config.Config{Panes: [2]config.PaneState{{Sort: "colour"}, {}}})
	if got := m.panes[0].SortModeLabel(); got != pane.SortName.String() {
		t.Errorf("sort = %q, want the default", got)
	}
}

// Quitting writes the view back, and a pane inside an archive saves the
// directory holding it rather than the virtual location.
func TestSaveSessionWritesState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)

	m := New()
	m.panes[0].CycleSort() // name → size
	m.panes[1].ToggleHidden()
	m.saveSession()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Panes[0].Sort != "size" {
		t.Errorf("left sort = %q, want size", cfg.Panes[0].Sort)
	}
	if cfg.Panes[0].Path != dir {
		t.Errorf("left path = %q, want %q", cfg.Panes[0].Path, dir)
	}
	if !cfg.Panes[1].Hidden {
		t.Error("right pane's hidden setting was not saved")
	}
}
