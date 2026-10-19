package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/kiruva/tyr/internal/termsafe"
)

// Fitting text to a terminal is measured in display cells, not bytes and not
// runes: a CJK glyph is two cells wide and a combining mark is none. Everything
// here goes through lipgloss.Width for that reason, and lives in ui rather than
// in one overlay so the overlays that were split out of the app package share
// one implementation instead of each carrying a copy.
//
// The truncators are also where outside text is made safe to draw. What they cut
// is nearly always a name, a path or a message from somewhere else, so they
// escape control characters first (termsafe.Line) — which is also what makes
// the width they cut to the width that is drawn. Escaping is idempotent, so
// text that was escaped already comes through unchanged. They are for plain
// text only: styled text would have its own sequences escaped.

// PadRight pads s out to w display cells. A string already that wide or wider
// is returned unchanged, never truncated — use TruncHead or TruncTail to cut.
func PadRight(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// TruncHead cuts s down to w cells, keeping the front and marking the cut with
// an ellipsis. It is for names, where what distinguishes one from another is
// usually at the start.
func TruncHead(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(termsafe.Line(s), w, "…")
}

// TruncTail cuts s down to w cells, keeping the end. It is for paths, where the
// last component is the part worth seeing.
//
// ansi.TruncateLeft is not used: it counts the cells to drop, and a wide glyph
// straddling that count is kept whole, so the result can be a cell too wide.
// Counting what is kept, from the end, cannot overshoot.
func TruncTail(s string, w int) string {
	s = termsafe.Line(s)
	if lipgloss.Width(s) <= w {
		return s
	}
	keep, used := len(s), 0
	for keep > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:keep])
		cw := runewidth.RuneWidth(r)
		if used+cw > w-1 {
			break
		}
		used += cw
		keep -= size
	}
	return "…" + s[keep:]
}

// JoinBlocks stacks blocks vertically with a blank line between them.
func JoinBlocks(blocks []string) string {
	spaced := make([]string, 0, len(blocks)*2)
	for i, b := range blocks {
		if i > 0 {
			spaced = append(spaced, "")
		}
		spaced = append(spaced, b)
	}
	return lipgloss.JoinVertical(lipgloss.Left, spaced...)
}

// BalancePoint is the block to break two columns at, chosen so the taller
// column is as short as it can be. Balancing on line count rather than block
// count matters because the groups are very different sizes: splitting them
// evenly by number leaves one column twice the height of the other.
func BalancePoint(blocks []string) int {
	total := 0
	for _, b := range blocks {
		total += lipgloss.Height(b) + 1 // + the blank line between groups
	}

	best, bestDiff, run := 1, total, 0
	for i, b := range blocks {
		run += lipgloss.Height(b) + 1
		diff := 2*run - total // left minus right
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			best, bestDiff = i+1, diff
		}
	}
	return best
}

// TwoColumns lays blocks out side by side, balanced by height.
func TwoColumns(blocks []string) string {
	mid := BalancePoint(blocks)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		JoinBlocks(blocks[:mid]), "     ", JoinBlocks(blocks[mid:]))
}

// ScrollBlock windows a rendered block to rows lines, starting at offset, and
// reports whether anything was left out. The offset is clamped here rather than
// where the key was pressed: the view is the only place that knows how tall the
// content turned out to be.
func ScrollBlock(block string, rows, offset int) (string, bool) {
	lines := strings.Split(block, "\n")
	if len(lines) <= rows {
		return block, false
	}

	offset = min(max(offset, 0), len(lines)-rows)
	window := lines[offset : offset+rows]
	return strings.Join(window, "\n"), true
}
