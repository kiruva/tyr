// Package theme is the theme picker overlay: its state, the keys that drive
// it, and how it draws.
//
// It is the worked example of how a modal is split out of the root model. The
// pieces are always the same three:
//
//   - a Model holding only what this overlay needs, nothing the rest of the app
//     can reach into;
//   - Update, taking a keypress and returning the new Model plus a Result;
//   - View, taking the chrome it does not own (the terminal size) as arguments.
//
// What it deliberately does *not* own is the app's mode. The mode enum is the
// one piece of state every overlay would otherwise have to share, and letting
// each of them write to it is exactly what makes a root model hard to follow.
// So Update reports an Outcome — "I am done" — and the root decides what that
// means. The dependency only ever points one way: this package knows nothing
// about the app, and the app knows only Model, Update, View and Result.
package theme

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/config"
	"github.com/kiruva/tyr/internal/ui"
)

// Outcome is what the root model should do about this overlay now.
type Outcome int

const (
	// Stay means the overlay is still open.
	Stay Outcome = iota
	// Close means it is finished and the root should go back to normal.
	Close
)

// Result is everything one keypress produced besides the new Model. A tea.Cmd
// and an error are kept apart from the Outcome because they are orthogonal: a
// theme can be applied and still fail to save, which closes the picker *and*
// has something to say.
type Result struct {
	Outcome Outcome
	Cmd     tea.Cmd
	// Err is worth showing in the status bar. It is not a reason to stay open.
	Err error
}

// Model is the picker while it is open.
type Model struct {
	cursor int    // highlighted row
	orig   string // theme to put back if the picker is cancelled
}

// New opens the picker on the theme that is currently applied, remembering it
// so Esc can put it back.
func New() Model {
	current := ui.Current().Name
	return Model{cursor: ui.ThemeIndex(current), orig: current}
}

// Cursor is the highlighted row, for the mouse handler in the root model.
func (m Model) Cursor() int { return m.cursor }

// MoveTo puts the cursor on a row and previews that theme, which is what a
// scroll wheel over the picker does. Out-of-range rows are clamped.
func (m Model) MoveTo(index int) Model {
	themes := ui.Themes()
	m.cursor = min(max(index, 0), len(themes)-1)
	ui.Apply(themes[m.cursor])
	return m
}

// Update drives the picker. Moving the cursor applies the theme live, so the
// surrounding UI is the preview; Enter keeps it, Esc puts the old one back.
func (m Model) Update(msg tea.KeyMsg) (Model, Result) {
	themes := ui.Themes()

	switch msg.String() {
	case "up", "k":
		return m.MoveTo(m.cursor - 1), Result{}
	case "down", "j":
		return m.MoveTo(m.cursor + 1), Result{}
	case "home", "g":
		return m.MoveTo(0), Result{}
	case "end", "G":
		return m.MoveTo(len(themes) - 1), Result{}

	case "enter":
		ui.Apply(themes[m.cursor])
		var err error
		if saveErr := config.Save(config.Config{Theme: ui.Current().Name}); saveErr != nil {
			err = &SaveError{Err: saveErr}
		}
		return m, Result{Outcome: Close, Err: err}

	case "esc", "q", "t":
		if t, ok := ui.ThemeByName(m.orig); ok {
			ui.Apply(t)
		}
		return m, Result{Outcome: Close}

	case "ctrl+c":
		return m, Result{Cmd: tea.Quit}
	}
	return m, Result{}
}

// SaveError says the theme took effect but could not be written down, which is
// a different thing from the theme not working.
type SaveError struct{ Err error }

func (e *SaveError) Error() string { return "theme applied but not saved: " + e.Err.Error() }
func (e *SaveError) Unwrap() error { return e.Err }

// View lists the themes with a colour swatch each, above a sample of what the
// highlighted one does to a pane.
func (m Model) View() string {
	const nameW = 12

	lines := []string{ui.DialogTitle.Render("Theme"), ""}
	for i, t := range ui.Themes() {
		name := ui.PadRight(t.Name, nameW)
		if i == m.cursor {
			lines = append(lines, ui.Cursor.Render("▸ "+name)+" "+ui.Swatch(t))
			continue
		}
		lines = append(lines, "  "+name+" "+ui.Swatch(t))
	}

	const sampleW = 24
	sample := lipgloss.JoinVertical(lipgloss.Left,
		ui.DirName.Render(ui.PadRight("  documents/", sampleW)),
		ui.Selected.Render(ui.PadRight("● selected.txt", sampleW)),
		ui.Cursor.Render(ui.PadRight("  cursor.go", sampleW-6)+" 1.2KB"),
		ui.StatusBar.Render(ui.PadRight(" 12 items · sort:name", sampleW)),
	)

	footer := ui.Faint.Render("↑/↓ preview · enter apply · esc cancel")
	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinVertical(lipgloss.Left, lines...), "", sample, "", footer)
	return ui.Dialog.Render(content)
}
