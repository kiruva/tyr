package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Fitting text to a terminal is measured in display cells, not bytes and not
// runes: a CJK glyph is two cells wide and a combining mark is none. Everything
// here goes through lipgloss.Width for that reason, and lives in ui rather than
// in one overlay so the overlays that were split out of the app package share
// one implementation instead of each carrying a copy.

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
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

// TruncTail cuts s down to w cells, keeping the end. It is for paths, where the
// last component is the part worth seeing.
func TruncTail(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	if w <= 1 {
		return "…"
	}
	return "…" + string(r[len(r)-(w-1):])
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

// OverlayRows is how many lines of a full-height overlay fit on a terminal of
// this height, after the border, the padding, the header and the footer have
// had theirs.
func OverlayRows(height int) int { return max(height-8, 3) }
