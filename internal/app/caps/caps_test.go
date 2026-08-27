package caps

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/ui"
)

// The overlay is a fixed table in a bordered box: it has to stay inside the
// window at any width, or the border breaks across lines.
func TestCapsOverlayFitsWindow(t *testing.T) {
	for _, w := range []int{60, 80, 100, 140, 200} {
		dialog := View(w, ui.OverlayRows(30), 0)
		if got := lipgloss.Width(dialog); got > w {
			t.Errorf("width %d: overlay is %d columns wide", w, got)
		}
	}
}

func TestCapSummary(t *testing.T) {
	tests := []struct {
		name string
		caps []fileops.Capability
		want string
	}{
		{"all ok", []fileops.Capability{{Name: "a"}}, "all available"},
		{
			"one missing",
			[]fileops.Capability{{Name: "a"}, {Name: "b", Needs: []string{"7z"}, Missing: []string{"7z"}}},
			"1 unavailable",
		},
		{
			"one optional",
			[]fileops.Capability{{Name: "a", Needs: []string{"xz"}, Missing: []string{"xz"}, Optional: true}},
			"1 optional tool missing",
		},
		{
			"both",
			[]fileops.Capability{
				{Name: "a", Needs: []string{"7z"}, Missing: []string{"7z"}},
				{Name: "b", Needs: []string{"xz"}, Missing: []string{"xz"}, Optional: true},
				{Name: "c", Needs: []string{"zstd"}, Missing: []string{"zstd"}, Optional: true},
			},
			"1 unavailable · 2 optional tools missing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := capSummary(tt.caps); got != tt.want {
				t.Fatalf("capSummary = %q, want %q", got, tt.want)
			}
		})
	}
}
