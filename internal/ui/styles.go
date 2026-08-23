// Package ui holds the Lip Gloss theme shared across tyr.
package ui

import "github.com/charmbracelet/lipgloss"

// Styles are package-level so render code can use them inline. Apply rebuilds
// every one of them from a Theme, which is how theme switching works at
// runtime — the next render simply reads the new values.
var (
	// ActiveBorder wraps the pane that currently has focus.
	ActiveBorder lipgloss.Style

	// InactiveBorder wraps the pane without focus.
	InactiveBorder lipgloss.Style

	// Cursor highlights the selected row in the active pane.
	Cursor lipgloss.Style

	// CursorInactive highlights the selected row in the unfocused pane.
	CursorInactive lipgloss.Style

	// DirName styles directory entries.
	DirName lipgloss.Style

	// Selected styles entries the user has marked with Space.
	Selected lipgloss.Style

	// Title styles the path shown at the top of each pane.
	Title lipgloss.Style

	// AddrEdit styles the pane's address bar while it is being edited.
	AddrEdit lipgloss.Style

	// StatusBar styles the bottom bar.
	StatusBar lipgloss.Style

	// ErrorBar styles the bottom bar when an operation failed.
	ErrorBar lipgloss.Style

	// NoticeBar styles the bottom bar for something worth saying that is not a
	// failure, such as a config directory moved on startup.
	NoticeBar lipgloss.Style

	// Dialog is the bordered box for confirm/progress modals.
	Dialog lipgloss.Style

	// DialogTitle styles the modal heading.
	DialogTitle lipgloss.Style

	// DialogHint styles the [y]/[n] key chips in a modal.
	DialogHint lipgloss.Style

	// Danger styles destructive/overwrite warnings.
	Danger lipgloss.Style

	// Faint styles secondary text (e.g. the current file in progress).
	Faint lipgloss.Style

	// HelpKey styles the key column in the help overlay.
	HelpKey lipgloss.Style

	// BarFilled / BarEmpty render the progress bar.
	BarFilled lipgloss.Style
	BarEmpty  lipgloss.Style
)

// current is the theme the styles were last built from.
var current = themes[0]

func init() { Apply(current) }

// Current returns the active theme.
func Current() Theme { return current }

// Apply rebuilds every style from t. Safe to call at any time; the change is
// visible on the next render.
func Apply(t Theme) {
	current = t

	ActiveBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Accent)

	InactiveBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Dim)

	Cursor = lipgloss.NewStyle().
		Background(t.Accent).
		Foreground(t.CursorFg).
		Bold(true)

	CursorInactive = lipgloss.NewStyle().
		Background(t.Dim).
		Foreground(t.Fg)

	DirName = lipgloss.NewStyle().
		Foreground(t.Accent).
		Bold(true)

	Selected = lipgloss.NewStyle().
		Foreground(t.Mark).
		Bold(true)

	Title = lipgloss.NewStyle().
		Foreground(t.Title).
		Bold(true)

	AddrEdit = lipgloss.NewStyle().
		Foreground(t.Fg).
		Background(t.Bar).
		Bold(true)

	StatusBar = lipgloss.NewStyle().
		Foreground(t.Fg).
		Background(t.Bar)

	ErrorBar = lipgloss.NewStyle().
		Foreground(t.Fg).
		Background(t.Danger).
		Bold(true)

	NoticeBar = lipgloss.NewStyle().
		Foreground(t.CursorFg).
		Background(t.Accent).
		Bold(true)

	Dialog = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Accent).
		Padding(1, 3)

	DialogTitle = lipgloss.NewStyle().
		Foreground(t.Accent).
		Bold(true)

	DialogHint = lipgloss.NewStyle().
		Foreground(t.CursorFg).
		Background(t.Accent).
		Bold(true).
		Padding(0, 1)

	Danger = lipgloss.NewStyle().
		Foreground(t.Danger).
		Bold(true)

	Faint = lipgloss.NewStyle().Foreground(t.Dim)

	HelpKey = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)

	BarFilled = lipgloss.NewStyle().Foreground(t.Accent)
	BarEmpty = lipgloss.NewStyle().Foreground(t.Dim)
}

// Swatch renders a small colour sample of a theme, for the theme picker.
func Swatch(t Theme) string {
	block := "██"
	return lipgloss.NewStyle().Foreground(t.Accent).Render(block) +
		lipgloss.NewStyle().Foreground(t.Mark).Render(block) +
		lipgloss.NewStyle().Foreground(t.Danger).Render(block) +
		lipgloss.NewStyle().Foreground(t.Dim).Render(block)
}
