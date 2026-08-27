package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/compare"
	"github.com/kiruva/tyr/internal/pane"
	"github.com/kiruva/tyr/internal/ui"
)

// The sync tool is full-screen: it is a list of paths with a decision each, and
// the decisions are only meaningful next to what they are decided from — the
// two sides' sizes and times. Chrome is a fixed number of lines so the list can
// be sized from the terminal height.
const syncChrome = 6 // header, two roots, rule, status, footer

// syncRowsPerPage is the paging step; the drawn window is sized from the
// terminal, and this only has to be a sensible jump.
const syncRowsPerPage = 15

// renderSync draws whichever stage of the tool is current.
func (m Model) renderSync() string {
	if m.sync.stage == syncStageRunning {
		return overlay(m.width, m.height, ui.Dialog.Width(dialogWidth).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				ui.DialogTitle.Render("Compare"),
				"",
				"walking both directories…",
				"",
				ui.DialogHint.Render("esc")+" cancel",
			)))
	}

	lines := []string{m.syncHeader(), m.syncRoots(), ui.Faint.Render(strings.Repeat("─", max(m.width, 1)))}
	lines = append(lines, m.syncList()...)
	lines = append(lines, m.syncFooter())
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// syncHeader counts what the comparison found and what is about to happen.
func (m Model) syncHeader() string {
	same, differs, onlyLeft, onlyRight := compare.Counts(pairsOf(m.sync.rows))
	toRight, toLeft := m.syncCounts()

	left := fmt.Sprintf(" compare · %d differ · %d only left · %d only right · %d same",
		differs, onlyLeft, onlyRight, same)
	if m.sync.truncated {
		left += fmt.Sprintf(" · stopped at %d", compare.DefaultMax)
	}
	right := fmt.Sprintf("%d → · %d ← ", toRight, toLeft)

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return ui.StatusBar.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

// syncRoots names the two directories being compared.
func (m Model) syncRoots() string {
	half := max(m.width/2-2, 8)
	left := ui.TruncTail(m.sync.leftRoot, half)
	right := ui.TruncTail(m.sync.rightRoot, half)
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-2, 1)
	return " " + ui.Title.Render(left) + strings.Repeat(" ", gap) + ui.Title.Render(right)
}

// syncList draws the rows, scrolled to keep the cursor in view.
func (m Model) syncList() []string {
	visible := m.visibleSyncRows()
	rows := max(m.height-syncChrome, 1)

	if len(visible) == 0 {
		return append([]string{"  " + ui.Faint.Render("nothing to show")}, blankLines(rows-1)...)
	}

	start := 0
	if m.sync.cursor >= rows {
		start = m.sync.cursor - rows + 1
	}
	end := min(start+rows, len(visible))

	out := make([]string, 0, rows)
	for i := start; i < end; i++ {
		out = append(out, m.syncRow(visible[i], i == m.sync.cursor))
	}
	return append(out, blankLines(rows-(end-start))...)
}

// syncRow lays out one path: what the left has, the decision, what the right
// has. The arrow is the only part that changes when a key is pressed, so it
// sits in the middle where the eye already is.
func (m Model) syncRow(index int, selected bool) string {
	r := m.sync.rows[index]

	sideW := max((m.width-8)/4, 10)
	pathW := max(m.width-2*sideW-9, 12)

	path := ui.TruncTail(r.pair.Rel, pathW)
	line := " " + ui.PadRight(path, pathW) + " " +
		ui.PadRight(syncSide(r.pair.Left), sideW) + " " +
		syncArrow(r) + " " +
		ui.PadRight(syncSide(r.pair.Right), sideW)

	switch {
	case selected:
		return ui.Cursor.Width(m.width).Render(line)
	case r.pair.Kind == compare.Same:
		return ui.Faint.Render(line)
	default:
		return line
	}
}

// syncSide is one side's size and time, or nothing when it has no such file.
func syncSide(info *compare.Info) string {
	if info == nil {
		return ui.Faint.Render("—")
	}
	return fmt.Sprintf("%8s %s", pane.HumanSize(info.Size), info.ModTime.Format("2006-01-02 15:04"))
}

// syncArrow is the decision, coloured by whether it does anything.
func syncArrow(r syncRow) string {
	arrow := r.action.arrow()
	if r.action == syncSkip {
		return ui.Faint.Render(arrow)
	}
	return ui.HelpKey.Render(arrow)
}

// syncFooter is the key legend, or whatever the tool has to say.
func (m Model) syncFooter() string {
	if m.sync.status != "" {
		return ui.NoticeBar.Width(m.width).Render(" " + ui.TruncTail(m.sync.status, m.width-1))
	}

	left := " →/← direction · s skip · a reset · e " + onOff("equal", m.sync.showSame) +
		" · . " + onOff("hidden", m.sync.hidden) + " · c " + onOff("by content", m.sync.byContent)
	right := "enter run · esc close "
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return ui.StatusBar.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

func onOff(label string, on bool) string {
	if on {
		return label + ":on"
	}
	return label + ":off"
}

// pairsOf lifts the comparison out of the rows, for counting.
func pairsOf(rows []syncRow) []compare.Pair {
	out := make([]compare.Pair, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.pair)
	}
	return out
}

func blankLines(n int) []string {
	if n <= 0 {
		return nil
	}
	out := make([]string, n)
	return out
}
