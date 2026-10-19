package termsafe

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestLine(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"plain.txt", "plain.txt"},
		{"日本語.txt", "日本語.txt"},
		{"a\x1b[31mb\nc\u202ed\xff", `a\u001b[31mb\nc\u202ed` + "\ufffd"},
		{"tab\there", `tab\there`},
		{"osc\x1b]52;c;aGVsbG8=\x07", `osc\u001b]52;c;aGVsbG8=\u0007`},
		{"c1\u009b2J", `c1\u009b2J`},
		{"sep\u2028arator", `sep\u2028arator`},
		{"del\x7f", `del\u007f`},
	} {
		if got := Line(tt.in); got != tt.want {
			t.Errorf("Line(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Text keeps the layout characters a file's contents are made of and escapes
// the rest exactly as Line does.
func TestText(t *testing.T) {
	in := "col1\tcol2\nline\x1b]0;PWNED\x07two\r"
	want := "col1\tcol2\nline" + `\u001b]0;PWNED\u0007two\u000d`
	if got := Text(in); got != want {
		t.Errorf("Text(%q) = %q, want %q", in, got, want)
	}
}

// Plain text is the common case, and is handed back as it came.
func TestLineLeavesPlainTextAlone(t *testing.T) {
	s := strings.Repeat("ordinary name ", 10)
	if Line(s) != s || Text(s) != s {
		t.Error("plain text was changed")
	}
}

func TestFrame(t *testing.T) {
	for _, tt := range []struct{ name, in, want string }{
		{"plain", "a\nb\tc", "a\nb\tc"},
		{"sgr kept", "\x1b[1;38;5;12mbold\x1b[0m", "\x1b[1;38;5;12mbold\x1b[0m"},
		{"sgr colon form kept", "\x1b[38:2:1:2:3mx\x1b[m", "\x1b[38:2:1:2:3mx\x1b[m"},
		{"osc 52 bel", "a\x1b]52;c;aGVsbG8=\x07b", "ab"},
		{"osc st", "a\x1b]0;title\x1b\\b", "ab"},
		{"osc c1 st", "a\x1b]0;title\u009cb", "ab"},
		{"osc cut short stops at newline", "a\x1b]0;tit\nnext", "a\nnext"},
		{"dcs", "a\x1bPq#0;2;0;0;0\x1b\\b", "ab"},
		{"cursor move", "a\x1b[2J\x1b[10;10Hb", "ab"},
		{"private csi", "a\x1b[?1049hb", "ab"},
		{"csi with intermediate", "a\x1b[1 qb", "ab"},
		{"bare esc", "a\x1bb", "a"}, // ESC b is a two-byte sequence
		{"trailing esc", "a\x1b", "a"},
		{"charset", "a\x1b(0b", "ab"},
		{"c0", "a\a\b\rb", "ab"},
		{"del", "a\x7fb", "ab"},
		{"c1 runes", "a\u009b2J\u009db", "a2Jb"},
		{"bidi", "a\u202eb\u2066c", "abc"},
		{"lone 0x9b byte", "a\x9b2Jb", "a\ufffd2Jb"},
		{"wide text kept", "日本\x1b]0;x\x07語", "日本語"},
	} {
		if got := Frame(tt.in); got != tt.want {
			t.Errorf("%s: Frame(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

// What lipgloss renders is the frame Frame sees most: it must come through
// byte for byte, or the filter would be eating tyr's own styling.
func TestFrameKeepsLipglossOutput(t *testing.T) {
	withColour(t)
	style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).
		Background(lipgloss.Color("#202020")).Underline(true).Reverse(true).
		Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(20)
	out := style.Render("name.txt\n日本語")
	if !strings.Contains(out, "\x1b[") {
		t.Fatal("no styling rendered: the test is not testing anything")
	}
	if got := Frame(out); got != out {
		t.Errorf("Frame changed lipgloss output:\n got %q\nwant %q", got, out)
	}
}

// Whatever Frame drops was counted as zero cells wide by lipgloss in the first
// place, so filtering never changes how wide a line is.
func TestFrameKeepsWidth(t *testing.T) {
	in := "ab\x1b]52;c;aGVsbG8=\x07cd\x1b[2Jef"
	if got, want := lipgloss.Width(Frame(in)), lipgloss.Width(in); got != want {
		t.Errorf("width %d after Frame, %d before", got, want)
	}
}

// Line's output is inert: nothing in it is a control character any more, so
// Frame has nothing to remove.
func FuzzLineIsInert(f *testing.F) {
	f.Add("a\x1b]52;c;aGVsbG8=\x07b")
	f.Add("\u009b\u202e\xff\n\t")
	f.Fuzz(func(t *testing.T, s string) {
		if l := Line(s); Frame(l) != l {
			t.Fatalf("Frame(Line(%q)) changed it", s)
		}
	})
}

// Frame's output holds no escape other than SGR, and filtering twice changes
// nothing.
func FuzzFrame(f *testing.F) {
	f.Add("a\x1b]52;c;x\x07b\x1b[31mc\x1b[0m")
	f.Add("\x1b[\x1b]\x1bP\x9b\u009b")
	f.Fuzz(func(t *testing.T, s string) {
		out := Frame(s)
		if Frame(out) != out {
			t.Fatalf("Frame not idempotent on %q", s)
		}
		for i := 0; i < len(out); i++ {
			if out[i] != 0x1b {
				continue
			}
			n, sgr := sequence(out[i:])
			if !sgr {
				t.Fatalf("non-SGR sequence %q left in Frame(%q)", out[i:i+n], s)
			}
			i += n - 1
		}
	})
}

// withColour renders in true colour for the rest of the test. Tests run without
// a terminal, where lipgloss draws no colour at all — and a check that styling
// survives Frame would then pass without a single SGR sequence in the frame.
func withColour(t *testing.T) {
	t.Helper()
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(before) })
}
