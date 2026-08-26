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

func TestSaveSessionRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Save(Config{Theme: "nord"}); err != nil {
		t.Fatalf("save theme: %v", err)
	}
	state := [2]PaneState{
		{Path: "/tmp/left", Sort: "size", Hidden: true},
		{Path: "/tmp/right", Sort: "time"},
	}
	if err := SaveSession(state); err != nil {
		t.Fatalf("save session: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Theme != "nord" {
		t.Errorf("Theme = %q, want nord — saving a session must not disturb it", cfg.Theme)
	}
	if cfg.Panes[0] != state[0] {
		t.Errorf("left = %+v, want %+v", cfg.Panes[0], state[0])
	}
	if cfg.Panes[1] != state[1] {
		t.Errorf("right = %+v, want %+v", cfg.Panes[1], state[1])
	}
}

// A pane with no path to report — one that ended the run on a remote host —
// leaves the saved path alone rather than blanking it.
func TestSaveSessionKeepsPathWhenUnreported(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := SaveSession([2]PaneState{{Path: "/tmp/left", Sort: "name"}, {}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := SaveSession([2]PaneState{{Sort: "size"}, {}}); err != nil {
		t.Fatalf("save again: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Panes[0].Path != "/tmp/left" {
		t.Errorf("left path = %q, want the earlier one kept", cfg.Panes[0].Path)
	}
	if cfg.Panes[0].Sort != "size" {
		t.Errorf("left sort = %q, want size", cfg.Panes[0].Sort)
	}
}

func TestRestorePaths(t *testing.T) {
	if (Config{}).RestorePaths() {
		t.Error("an empty config restores paths, want cwd by default")
	}
	if !(Config{Startup: "LAST"}).RestorePaths() {
		t.Error("startup = LAST should restore paths")
	}
	if (Config{Startup: "cwd"}).RestorePaths() {
		t.Error("startup = cwd should not restore paths")
	}
}

func TestHiddenSpellings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, _ := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "left.hidden = yes\nright.hidden = 0\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Panes[0].Hidden {
		t.Error("left.hidden = yes did not read as true")
	}
	if cfg.Panes[1].Hidden {
		t.Error("right.hidden = 0 read as true")
	}
}

func TestBookmarksRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Save(Config{Theme: "nord"}); err != nil {
		t.Fatalf("save theme: %v", err)
	}
	for _, b := range []Bookmark{{Name: "src", Path: "/home/kim/src"}, {Name: "dl", Path: "/home/kim/downloads"}} {
		if err := SaveBookmark(b); err != nil {
			t.Fatalf("save %s: %v", b.Name, err)
		}
	}

	marks, err := Bookmarks()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(marks) != 2 || marks[0].Name != "dl" || marks[1].Name != "src" {
		t.Fatalf("bookmarks = %+v, want dl then src", marks)
	}

	cfg, _ := Load()
	if cfg.Theme != "nord" {
		t.Errorf("Theme = %q, want the bookmark writes to have left it alone", cfg.Theme)
	}

	if err := DeleteBookmark("src"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	marks, _ = Bookmarks()
	if len(marks) != 1 || marks[0].Name != "dl" {
		t.Fatalf("after deleting src: %+v", marks)
	}
}

// Deleting one bookmark must not take a longer name that starts the same way.
func TestDeleteBookmarkIsExact(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for _, b := range []Bookmark{{Name: "src", Path: "/a"}, {Name: "srcold", Path: "/b"}} {
		if err := SaveBookmark(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := DeleteBookmark("src"); err != nil {
		t.Fatal(err)
	}

	marks, _ := Bookmarks()
	if len(marks) != 1 || marks[0].Name != "srcold" {
		t.Fatalf("bookmarks = %+v, want only srcold left", marks)
	}
}

func TestSaveBookmarkRejectsBadInput(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for _, b := range []Bookmark{
		{Name: "", Path: "/a"},
		{Name: "two words", Path: "/a"},
		{Name: "with=equals", Path: "/a"},
		{Name: "ok", Path: ""},
	} {
		if err := SaveBookmark(b); err == nil {
			t.Errorf("SaveBookmark(%+v) was accepted", b)
		}
	}
}

func TestTrashDeletesSetting(t *testing.T) {
	if !(Config{}).TrashDeletes() {
		t.Error("an unset delete setting should use the trash")
	}
	if (Config{Delete: "REMOVE"}).TrashDeletes() {
		t.Error("delete = remove should unlink")
	}
	if !(Config{Delete: "trash"}).TrashDeletes() {
		t.Error("delete = trash should use the trash")
	}
	if !(Config{Delete: "nonsense"}).TrashDeletes() {
		t.Error("an unreadable value should keep the reversible behaviour")
	}
}
