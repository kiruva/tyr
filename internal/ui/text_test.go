package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPadRight(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"ab", "ab    "},
		{"", "      "},
		{"abcdef", "abcdef"},
		{"abcdefgh", "abcdefgh"}, // already wider: never truncated
	} {
		if got := PadRight(tt.in, 6); got != tt.want {
			t.Errorf("PadRight(%q, 6) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Padding is measured in display cells, so a double-width glyph counts twice.
// Counting bytes or runes instead is what makes a column drift out of line.
func TestPadRightCountsDisplayCells(t *testing.T) {
	const wide = "日本" // two runes, four cells
	got := PadRight(wide, 6)
	if w := lipgloss.Width(got); w != 6 {
		t.Errorf("PadRight(%q, 6) is %d cells wide, want 6", wide, w)
	}
}

func TestTruncHeadKeepsTheFront(t *testing.T) {
	for _, tt := range []struct {
		in   string
		w    int
		want string
	}{
		{"abcdef", 10, "abcdef"},
		{"abcdef", 6, "abcdef"},
		{"abcdef", 4, "abc…"},
		{"abcdef", 1, "…"},
		{"abcdef", 0, ""},
		{"abcdef", -1, ""},
	} {
		if got := TruncHead(tt.in, tt.w); got != tt.want {
			t.Errorf("TruncHead(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
		}
	}
}

func TestTruncTailKeepsTheEnd(t *testing.T) {
	for _, tt := range []struct {
		in   string
		w    int
		want string
	}{
		{"abcdef", 10, "abcdef"},
		{"abcdef", 6, "abcdef"},
		{"abcdef", 4, "…def"},
		{"abcdef", 1, "…"},
		{"abcdef", 0, "…"},
	} {
		if got := TruncTail(tt.in, tt.w); got != tt.want {
			t.Errorf("TruncTail(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
		}
	}
}

// The two cut from opposite ends: a name keeps its front, a path keeps its tail.
func TestTruncHeadAndTailCutOppositeEnds(t *testing.T) {
	const s = "/home/someone/documents/notes.txt"
	head, tail := TruncHead(s, 12), TruncTail(s, 12)
	if !strings.HasPrefix(head, "/home") {
		t.Errorf("TruncHead = %q, want it to keep the front", head)
	}
	if !strings.HasSuffix(tail, "notes.txt") {
		t.Errorf("TruncTail = %q, want it to keep the tail", tail)
	}
}

func TestJoinBlocksSeparatesWithABlankLine(t *testing.T) {
	// lipgloss pads every line to the width of the widest, so the separator is
	// blank rather than empty. What matters is that it holds no content.
	lines := strings.Split(JoinBlocks([]string{"a", "b", "c"}), "\n")
	if len(lines) != 5 {
		t.Fatalf("got %d lines (%q), want 5: three blocks and two separators", len(lines), lines)
	}
	for i, want := range []string{"a", "", "b", "", "c"} {
		if strings.TrimSpace(lines[i]) != want {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
		}
	}
	if got := JoinBlocks(nil); got != "" {
		t.Errorf("JoinBlocks(nil) = %q, want empty", got)
	}
}

// The split is chosen so the taller column is as short as it can be, which for
// very uneven blocks is not the halfway point by count.
func TestBalancePointEvensTheColumnHeights(t *testing.T) {
	tall := strings.Repeat("x\n", 9) + "x" // 10 lines
	short := "x"

	// One tall block and three short ones: the break belongs after the tall one.
	if got := BalancePoint([]string{tall, short, short, short}); got != 1 {
		t.Errorf("BalancePoint = %d, want 1 (after the tall block)", got)
	}
	// Four equal blocks split down the middle.
	if got := BalancePoint([]string{short, short, short, short}); got != 2 {
		t.Errorf("BalancePoint = %d, want 2", got)
	}
}

func TestScrollBlockWindowsAndReportsMore(t *testing.T) {
	block := strings.Join([]string{"1", "2", "3", "4", "5"}, "\n")

	if got, more := ScrollBlock(block, 5, 0); got != block || more {
		t.Errorf("ScrollBlock(all) = %q, more=%v; want the whole block and false", got, more)
	}
	got, more := ScrollBlock(block, 2, 1)
	if got != "2\n3" || !more {
		t.Errorf("ScrollBlock(rows=2, offset=1) = %q, more=%v; want \"2\\n3\", true", got, more)
	}
}

// The offset is clamped in the view, because that is the only place that knows
// how tall the content turned out to be.
func TestScrollBlockClampsTheOffset(t *testing.T) {
	block := strings.Join([]string{"1", "2", "3", "4", "5"}, "\n")

	if got, _ := ScrollBlock(block, 2, 999); got != "4\n5" {
		t.Errorf("past the end = %q, want the last window \"4\\n5\"", got)
	}
	if got, _ := ScrollBlock(block, 2, -5); got != "1\n2" {
		t.Errorf("before the start = %q, want the first window \"1\\n2\"", got)
	}
}

func TestOverlayRowsKeepsAFloor(t *testing.T) {
	if got := OverlayRows(30); got != 22 {
		t.Errorf("OverlayRows(30) = %d, want 22", got)
	}
	// A terminal too short for the chrome still gets a usable window rather
	// than a negative one.
	if got := OverlayRows(4); got != 3 {
		t.Errorf("OverlayRows(4) = %d, want the floor of 3", got)
	}
}

func TestTwoColumnsPutsBlocksSideBySide(t *testing.T) {
	got := TwoColumns([]string{"left", "right"})
	line := strings.SplitN(got, "\n", 2)[0]
	if !strings.Contains(line, "left") || !strings.Contains(line, "right") {
		t.Errorf("first line = %q, want both blocks on it", line)
	}
}
