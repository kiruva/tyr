package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// themeDir writes the given files into a fresh directory and returns it.
func themeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// restoreThemes puts the built-in set back after a test has added to it.
func restoreThemes(t *testing.T) {
	t.Helper()
	saved := make([]Theme, len(themes))
	copy(saved, themes)
	t.Cleanup(func() { themes = saved })
}

const rose = `# a theme
accent   = #D3869B
dim      = #665C54
fg       = #FBF1C7
title    = #EBDBB2
mark     = #FABD2F
bar      = #3C3836
danger   = #FB4934
cursorfg = #1D2021
`

func TestLoadUserTheme(t *testing.T) {
	restoreThemes(t)
	dir := themeDir(t, map[string]string{"rose.theme": rose})

	loaded, problems := LoadUserThemes(dir)
	if loaded != 1 || len(problems) != 0 {
		t.Fatalf("loaded %d, problems %v", loaded, problems)
	}

	theme, ok := ThemeByName("rose")
	if !ok {
		t.Fatal("the theme is not in the picker")
	}
	if theme.Accent != "#D3869B" || theme.CursorFg != "#1D2021" {
		t.Fatalf("theme = %+v, want the colours from the file", theme)
	}
	if names := ThemeNames(); names[len(names)-1] != "rose" {
		t.Errorf("picker order = %v, want the new theme last", names)
	}
}

// A file named after a built-in replaces it in place, rather than adding a
// second theme under the same name.
func TestUserThemeReplacesBuiltIn(t *testing.T) {
	restoreThemes(t)
	before := len(themes)
	dir := themeDir(t, map[string]string{"nord.theme": rose})

	if loaded, _ := LoadUserThemes(dir); loaded != 1 {
		t.Fatalf("loaded %d, want 1", loaded)
	}
	if len(themes) != before {
		t.Fatalf("themes = %d, want the built-in replaced rather than added to", len(themes))
	}
	theme, _ := ThemeByName("nord")
	if theme.Accent != "#D3869B" {
		t.Errorf("nord accent = %s, want the file's", theme.Accent)
	}
}

// The name line wins over the file name.
func TestUserThemeName(t *testing.T) {
	restoreThemes(t)
	dir := themeDir(t, map[string]string{"whatever.theme": "name = midnight\n" + rose})

	LoadUserThemes(dir)
	if _, ok := ThemeByName("midnight"); !ok {
		t.Fatal("the theme did not take the name from the file")
	}
	if _, ok := ThemeByName("whatever"); ok {
		t.Error("it is also under the file's name")
	}
}

// A broken file costs its own theme and nothing else.
func TestBadThemesAreReportedAndSkipped(t *testing.T) {
	restoreThemes(t)
	dir := themeDir(t, map[string]string{
		"good.theme":    rose,
		"colour.theme":  strings.Replace(rose, "#D3869B", "mauve", 1),
		"missing.theme": "accent = #ffffff\n",
		"line.theme":    "accent #ffffff\n",
	})

	loaded, problems := LoadUserThemes(dir)
	if loaded != 1 {
		t.Fatalf("loaded %d, want only the good one", loaded)
	}
	if len(problems) != 3 {
		t.Fatalf("problems = %v, want one per broken file", problems)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"colour.theme", "missing.theme", "line.theme", "accent", "not a colour"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems do not mention %q:\n%s", want, joined)
		}
	}
}

// An ANSI index is a colour too, which is what the default theme is made of.
func TestAnsiIndexColours(t *testing.T) {
	restoreThemes(t)
	body := `accent = 39
dim = 240
fg = 231
title = 252
mark = 220
bar = 237
danger = 196
cursorfg = 231
`
	dir := themeDir(t, map[string]string{"ansi.theme": body})
	if loaded, problems := LoadUserThemes(dir); loaded != 1 {
		t.Fatalf("loaded %d: %v", loaded, problems)
	}
	if theme, _ := ThemeByName("ansi"); theme.Accent != "39" {
		t.Errorf("accent = %q, want 39", theme.Accent)
	}
}

func TestParseColorRejectsNonsense(t *testing.T) {
	for _, bad := range []string{"mauve", "#12", "#gggggg", "300", "", "#1234567"} {
		if _, err := parseColor(bad); err == nil {
			t.Errorf("parseColor(%q) was accepted", bad)
		}
	}
	for _, good := range []string{"#fff", "#FFAA00", "0", "255"} {
		if _, err := parseColor(good); err != nil {
			t.Errorf("parseColor(%q): %v", good, err)
		}
	}
}

// A missing themes directory is the normal case, not a problem to report.
func TestNoThemesDirectory(t *testing.T) {
	loaded, problems := LoadUserThemes(filepath.Join(t.TempDir(), "nothing-here"))
	if loaded != 0 || problems != nil {
		t.Fatalf("loaded %d, problems %v — want silence", loaded, problems)
	}
}

// The template writes a file the loader can read back.
func TestWriteThemeTemplateRoundTrip(t *testing.T) {
	restoreThemes(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.theme")

	if err := WriteThemeTemplate(path); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, problems := LoadUserThemes(dir)
	if loaded != 1 || len(problems) != 0 {
		t.Fatalf("loaded %d, problems %v", loaded, problems)
	}

	written, ok := ThemeByName("mine")
	if !ok {
		t.Fatal("the written template is not loadable")
	}
	if written.Accent != Current().Accent {
		t.Errorf("accent = %s, want the current theme's %s", written.Accent, Current().Accent)
	}
}

// Files that are not themes are left alone.
func TestOnlyThemeFilesAreRead(t *testing.T) {
	restoreThemes(t)
	dir := themeDir(t, map[string]string{"notes.txt": "nothing", "rose.theme": rose})

	loaded, problems := LoadUserThemes(dir)
	if loaded != 1 || len(problems) != 0 {
		t.Fatalf("loaded %d, problems %v", loaded, problems)
	}
}
