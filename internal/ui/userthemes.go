package ui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A theme is eight colours and a name, so a theme file is eight lines. They
// live one per file in <config>/themes, which makes one shareable — sending
// someone a theme is sending them a file, and dropping it in is installing it:
//
//	# ~/.config/tyr/themes/rose.theme
//	accent   = #D3869B
//	dim      = #665C54
//	fg       = #FBF1C7
//	title    = #EBDBB2
//	mark     = #FABD2F
//	bar      = #3C3836
//	danger   = #FB4934
//	cursorfg = #1D2021
//
// The file name is the theme name unless a "name =" line says otherwise. A file
// named after a built-in replaces it, which is how a built-in gets adjusted
// rather than duplicated under a second name.

// ThemeExt is the extension a theme file must have to be loaded, and ThemeDir
// the directory under the config directory they are read from.
const (
	ThemeExt = ".theme"
	ThemeDir = "themes"
)

// colorPattern is what a colour may be: a hex triple, or an ANSI palette index.
var colorPattern = regexp.MustCompile(`^(#[0-9a-fA-F]{3}|#[0-9a-fA-F]{6}|[0-9]{1,3})$`)

// LoadUserThemes reads every theme file in dir and adds it to the picker,
// replacing a built-in of the same name. A file that cannot be read or does not
// describe a theme is reported and skipped: one bad file must not cost the user
// the other themes, or the app's ability to start.
func LoadUserThemes(dir string) (loaded int, problems []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil // no themes directory is the normal case, not a problem
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ThemeExt) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		theme, err := readTheme(filepath.Join(dir, name))
		if err != nil {
			problems = append(problems, name+": "+err.Error())
			continue
		}
		addTheme(theme)
		loaded++
	}
	return loaded, problems
}

// addTheme installs a theme, replacing a same-named one in place so the picker
// order stays stable across a reload.
func addTheme(t Theme) {
	for i := range themes {
		if themes[i].Name == t.Name {
			themes[i] = t
			return
		}
	}
	themes = append(themes, t)
}

// readTheme parses one theme file.
func readTheme(path string) (Theme, error) {
	f, err := os.Open(path)
	if err != nil {
		return Theme{}, err
	}
	defer f.Close()

	values := map[string]string{}
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return Theme{}, fmt.Errorf("line %d is not \"key = value\"", line)
		}
		values[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	if err := sc.Err(); err != nil {
		return Theme{}, err
	}

	name := values["name"]
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	theme := Theme{Name: strings.ToLower(name)}
	if theme.Name == "" {
		return Theme{}, fmt.Errorf("no name")
	}

	fields := []struct {
		key   string
		field *Color
	}{
		{"accent", &theme.Accent},
		{"dim", &theme.Dim},
		{"fg", &theme.Fg},
		{"title", &theme.Title},
		{"mark", &theme.Mark},
		{"bar", &theme.Bar},
		{"danger", &theme.Danger},
		{"cursorfg", &theme.CursorFg},
	}
	for _, f := range fields {
		raw, ok := values[f.key]
		if !ok {
			return Theme{}, fmt.Errorf("missing %s", f.key)
		}
		colour, err := parseColor(raw)
		if err != nil {
			return Theme{}, fmt.Errorf("%s: %w", f.key, err)
		}
		*f.field = colour
	}
	return theme, nil
}

// parseColor accepts "#rgb", "#rrggbb", or an ANSI index from 0 to 255.
func parseColor(raw string) (Color, error) {
	value := strings.TrimSpace(raw)
	if !colorPattern.MatchString(value) {
		return "", fmt.Errorf("%q is not a colour — use #rrggbb or 0-255", raw)
	}
	if !strings.HasPrefix(value, "#") {
		n, err := strconv.Atoi(value)
		if err != nil || n > 255 {
			return "", fmt.Errorf("%q is not a colour — an ANSI index runs 0 to 255", raw)
		}
	}
	return Color(value), nil
}

// WriteThemeTemplate writes the active theme to path as a starting point for a
// custom one, so the format does not have to be looked up.
func WriteThemeTemplate(path string) error {
	t := Current()
	body := fmt.Sprintf(`# a tyr theme: eight colours, as #rrggbb or an ANSI index 0-255
name     = %s
accent   = %s
dim      = %s
fg       = %s
title    = %s
mark     = %s
bar      = %s
danger   = %s
cursorfg = %s
`,
		strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		t.Accent, t.Dim, t.Fg, t.Title, t.Mark, t.Bar, t.Danger, t.CursorFg)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}
