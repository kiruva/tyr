package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Color is one colour in a theme: a hex triple, or an ANSI palette index.
type Color = lipgloss.Color

// Theme is the full colour vocabulary of the UI. Every style in styles.go is
// derived from these eight colours, so adding a theme is a data change only —
// a built-in struct literal below, or a file in <config>/themes.
type Theme struct {
	Name     string
	Accent   Color // active border, cursor, directory names
	Dim      Color // inactive border, faint/secondary text
	Fg       Color // primary text on the status bar
	Title    Color // address bar text
	Mark     Color // entries marked with Space
	Bar      Color // status bar background
	Danger   Color // destructive actions and errors
	CursorFg Color // text drawn on top of Accent
}

// themes is the built-in set, in picker order. The first entry is the default.
var themes = []Theme{
	{
		Name:     "default",
		Accent:   "39",
		Dim:      "240",
		Fg:       "231",
		Title:    "252",
		Mark:     "220",
		Bar:      "237",
		Danger:   "196",
		CursorFg: "231",
	},
	{
		Name:     "nord",
		Accent:   "#88C0D0",
		Dim:      "#4C566A",
		Fg:       "#ECEFF4",
		Title:    "#E5E9F0",
		Mark:     "#EBCB8B",
		Bar:      "#3B4252",
		Danger:   "#BF616A",
		CursorFg: "#2E3440",
	},
	{
		Name:     "dracula",
		Accent:   "#BD93F9",
		Dim:      "#6272A4",
		Fg:       "#F8F8F2",
		Title:    "#F8F8F2",
		Mark:     "#F1FA8C",
		Bar:      "#44475A",
		Danger:   "#FF5555",
		CursorFg: "#282A36",
	},
	{
		Name:     "gruvbox",
		Accent:   "#83A598",
		Dim:      "#665C54",
		Fg:       "#FBF1C7",
		Title:    "#EBDBB2",
		Mark:     "#FABD2F",
		Bar:      "#3C3836",
		Danger:   "#FB4934",
		CursorFg: "#1D2021",
	},
	{
		Name:     "catppuccin",
		Accent:   "#89B4FA",
		Dim:      "#585B70",
		Fg:       "#CDD6F4",
		Title:    "#CDD6F4",
		Mark:     "#F9E2AF",
		Bar:      "#313244",
		Danger:   "#F38BA8",
		CursorFg: "#1E1E2E",
	},
	{
		Name:     "tokyonight",
		Accent:   "#7AA2F7",
		Dim:      "#565F89",
		Fg:       "#C0CAF5",
		Title:    "#C0CAF5",
		Mark:     "#E0AF68",
		Bar:      "#292E42",
		Danger:   "#F7768E",
		CursorFg: "#1A1B26",
	},
	{
		Name:     "solarized",
		Accent:   "#268BD2",
		Dim:      "#586E75",
		Fg:       "#EEE8D5",
		Title:    "#93A1A1",
		Mark:     "#B58900",
		Bar:      "#073642",
		Danger:   "#DC322F",
		CursorFg: "#002B36",
	},
	{
		Name:     "monokai",
		Accent:   "#66D9EF",
		Dim:      "#75715E",
		Fg:       "#F8F8F2",
		Title:    "#F8F8F2",
		Mark:     "#E6DB74",
		Bar:      "#3E3D32",
		Danger:   "#F92672",
		CursorFg: "#272822",
	},
}

// Themes returns the known themes in picker order: the built-ins, then whatever
// LoadUserThemes found.
func Themes() []Theme { return themes }

// ThemeNames lists the known theme names in picker order.
func ThemeNames() []string {
	names := make([]string, 0, len(themes))
	for _, t := range themes {
		names = append(names, t.Name)
	}
	return names
}

// ThemeByName looks a theme up case-insensitively.
func ThemeByName(name string) (Theme, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, t := range themes {
		if t.Name == want {
			return t, true
		}
	}
	return Theme{}, false
}

// ThemeIndex returns the position of a theme in picker order, or 0.
func ThemeIndex(name string) int {
	want := strings.ToLower(strings.TrimSpace(name))
	for i, t := range themes {
		if t.Name == want {
			return i
		}
	}
	return 0
}

// ThemeList renders the known theme names for help and error messages.
func ThemeList() string { return strings.Join(ThemeNames(), ", ") }
