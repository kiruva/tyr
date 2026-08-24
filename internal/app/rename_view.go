package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/rename"
	"github.com/kiruva/tyr/internal/ui"
)

// The batch tool is full-screen: the form is small, and everything else is the
// preview, which is the part worth the space. Chrome is a fixed number of lines
// so the preview window can be sized from the terminal height.
const renameChrome = 10 // header, blank, 4 form rows, status, rule, 2 footers

// renameFieldWidth is the width an input is built with. The tool re-sizes them
// from the terminal when it draws, but a textinput needs some width to start.
func renameFieldWidth(f renameField) int {
	switch f {
	case renFind, renReplace:
		return 38
	case renNameMask:
		return 18
	case renExtMask:
		return 10
	default:
		return 5
	}
}

// fieldWidth is how wide the input is actually drawn, so the form rows fit the
// terminal instead of wrapping. The find and replace fields absorb the change;
// the masks and counters shrink only when they have to.
func (m Model) fieldWidth(f renameField) int {
	narrow := m.width < 78
	switch f {
	case renFind, renReplace:
		// label column, the two chips after the field, and the margins
		return min(max(m.width-32, 10), 38)
	case renNameMask:
		if narrow {
			return 10
		}
		return 18
	case renExtMask:
		if narrow {
			return 6
		}
		return 10
	default:
		if narrow {
			return 4
		}
		return 5
	}
}

// renameRows is how many preview rows fit under the form.
func (m Model) renameRows() int { return max(m.height-renameChrome, 1) }

func (m Model) renderRename() string {
	lines := []string{
		m.renameHeader(),
		"",
		"  " + renameLabel("find") + m.renField(renFind) + "  " +
			chip(m.ren.mode.String(), true) + ui.Faint.Render(" · ") + chip("ignore case", m.ren.ignore),
		"  " + renameLabel("replace") + m.renField(renReplace),
		"  " + renameLabel("name") + m.renField(renNameMask) + "  " +
			inlineLabel("ext") + m.renField(renExtMask) + "  " +
			inlineLabel("counter") + m.renField(renCounter) + " " +
			inlineLabel("step") + m.renField(renStep),
		"  " + renameToggles(m.ren),
		m.renameStatusLine(),
		ui.Faint.Render(strings.Repeat("─", max(m.width, 1))),
	}
	lines = append(lines, m.renamePreview()...)
	lines = append(lines, m.renameFooters()...)

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renameHeader is the top bar: what the batch is, and what it would do.
func (m Model) renameHeader() string {
	s := m.ren.summary
	parts := []string{"scanning…"}
	if !m.ren.loading {
		parts = []string{
			fmt.Sprintf("%d %s", s.Total, items(s.Total)),
			fmt.Sprintf("%d to rename", s.Renamed),
		}
		if s.Conflicts > 0 {
			parts = append(parts, fmt.Sprintf("%d blocked", s.Conflicts))
		}
		if m.ren.truncated {
			parts = append(parts, fmt.Sprintf("stopped at %d", rename.DefaultLimit))
		}
	}

	left := " multi-rename · " + strings.Join(parts, " · ")
	right := truncTail(m.ren.root, max(m.width-lipgloss.Width(left)-2, 8)) + " "
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return ui.StatusBar.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

// renField draws one input, highlighting the focused one.
func (m Model) renField(f renameField) string {
	w := m.fieldWidth(f)
	ti := m.ren.fields[f]
	ti.Width = w // a copy: the render decides the width, the state keeps the text
	view := ti.View()
	if m.ren.focus == f {
		return ui.AddrEdit.Width(w).Render(view)
	}
	return lipgloss.NewStyle().Width(w).Render(view)
}

// renameLabel keeps the form's left-hand labels in one column. The width has to
// come from the style: lipgloss trims the trailing spaces off a rendered line.
func renameLabel(s string) string { return ui.Faint.Width(9).Render(s) }

// inlineLabel names a field sharing a row with others, where lining up with the
// column above would only spread the row out.
func inlineLabel(s string) string { return ui.Faint.Render(s + " ") }

// chip renders a state as on (accented) or off (faint).
func chip(label string, on bool) string {
	if on {
		return ui.HelpKey.Render(label)
	}
	return ui.Faint.Render(label)
}

// renameToggles is the line of scope and post-processing switches.
func renameToggles(st renameState) string {
	return strings.Join([]string{
		chip("case "+st.kase.String(), st.kase != rename.CaseNone),
		chip("trim", st.trim),
		chip("recursive", st.opt.Recursive),
		chip("rename dirs", st.opt.Dirs),
	}, ui.Faint.Render(" · "))
}

func (m Model) renameStatusLine() string {
	if m.ren.status == "" {
		return ""
	}
	return "  " + ui.Danger.Render(truncTail(m.ren.status, max(m.width-4, 8)))
}

// renamePreview draws the visible slice of the plan, one row per candidate.
func (m Model) renamePreview() []string {
	rows := m.renameRows()

	if m.ren.loading {
		return padRows([]string{"  " + ui.Faint.Render("scanning the selection…")}, rows)
	}
	if len(m.ren.changes) == 0 {
		return padRows([]string{"  " + ui.Faint.Render("nothing selected to rename")}, rows)
	}

	// The left column is as wide as the longest source name, so the arrows line
	// up close to the names instead of across an empty gutter.
	avail := max(m.width-6, 20)
	fromW := min(max(longestSource(m.ren.changes)+1, 12), avail*60/100)
	toW := avail - fromW

	end := min(m.ren.scroll+rows, len(m.ren.changes))
	out := make([]string, 0, rows)
	for _, c := range m.ren.changes[m.ren.scroll:end] {
		out = append(out, renameRow(c, fromW, toW))
	}
	return padRows(out, rows)
}

// longestSource is the widest source path in the plan, for the column width.
func longestSource(changes []rename.Change) int {
	w := 0
	for _, c := range changes {
		w = max(w, lipgloss.Width(filepath.ToSlash(c.Rel)))
	}
	return w
}

// renameRow is one "old → new" line, in the style its outcome deserves.
func renameRow(c rename.Change, fromW, toW int) string {
	from, to := renameDisplay(c)
	left := padRight(truncTail(from, fromW), fromW)

	switch {
	case c.Problem != "":
		right := truncTail(to, max(toW-lipgloss.Width(c.Problem)-3, 4))
		return "  " + left + ui.Faint.Render(" → ") +
			right + " " + ui.Danger.Render("✗ "+c.Problem)
	case !c.Changed:
		return "  " + ui.Faint.Render(left+"   (unchanged)")
	default:
		return "  " + left + ui.Faint.Render(" → ") + ui.Selected.Render(truncTail(to, toW))
	}
}

// padRows fills the preview area so the footer stays pinned to the bottom.
func padRows(lines []string, rows int) []string {
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return lines[:rows]
}

// renameFooters are the two key lines. The second one turns into the token
// legend while a mask field has focus, which is when it is wanted.
func (m Model) renameFooters() []string {
	nav := " tab field · ↑/↓ scroll · enter apply · esc cancel"
	if n := len(m.ren.changes); n > m.renameRows() {
		nav += fmt.Sprintf(" · %d–%d of %d", m.ren.scroll+1, min(m.ren.scroll+m.renameRows(), n), n)
	}

	keys := " ctrl+o mode · alt+i ignore case · ctrl+t case · ctrl+p trim · ctrl+r recursive · ctrl+y dirs"
	if m.ren.focus == renReplace || m.ren.focus == renNameMask || m.ren.focus == renExtMask {
		keys = " [N] name · [N2-5] part of it · [E] ext · [C] counter · [C3] padded · [P] parent · [d] date · [t] time"
	}

	return []string{
		ui.StatusBar.Width(m.width).Render(truncHead(nav, m.width)),
		ui.StatusBar.Width(m.width).Render(truncHead(keys, m.width)),
	}
}

// renderRenameOne draws the single-entry prompt, sized like the other modals.
func (m Model) renderRenameOne() string {
	what := "Rename"
	if m.renOne.isDir {
		what = "Rename folder"
	}

	lines := []string{
		ui.DialogTitle.Render(what),
		"",
		ui.Faint.Render(truncTail(m.renOne.orig, contentWidth)),
		ui.AddrEdit.Width(contentWidth).Render(m.renOne.input.View()),
	}
	if m.renOne.status != "" {
		lines = append(lines, "", ui.Danger.Render(truncTail(m.renOne.status, contentWidth)))
	}
	lines = append(lines, "",
		ui.DialogHint.Render("enter")+" rename    "+ui.DialogHint.Render("esc")+" cancel"+
			ui.Faint.Render("    M for a batch"))

	return ui.Dialog.Width(dialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
