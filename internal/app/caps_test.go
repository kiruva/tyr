package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
)

func TestCapsOverlayOpensAndCloses(t *testing.T) {
	m := newSized(t)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	if m.(Model).mode != modeCaps {
		t.Fatal("'C' should open the capabilities overlay")
	}

	view := m.View()
	for _, want := range []string{"capabilities", ".7z", ".rar", "Remote (SSH)"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay does not mention %q", want)
		}
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.(Model).mode != modeNormal {
		t.Fatal("any key should close the overlay")
	}
}

// The two overlays are each other's escape hatch: the footers advertise it.
func TestHelpAndCapsSwap(t *testing.T) {
	m := newSized(t)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !strings.Contains(m.View(), "C capabilities") {
		t.Error("help overlay does not point at the capabilities overlay")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	if m.(Model).mode != modeCaps {
		t.Fatal("'C' in help should switch to capabilities")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if m.(Model).mode != modeHelp {
		t.Fatal("'?' in capabilities should switch back to help")
	}
}

// 'C' must not be swallowed by the copy binding on 'c'.
func TestCapsKeyIsNotCopy(t *testing.T) {
	m := newSized(t)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.(Model).mode == modeCaps {
		t.Fatal("'c' should be copy, not capabilities")
	}
}

// The overlay is a fixed table in a bordered box: it has to stay inside the
// window at any width, or the border breaks across lines.
func TestCapsOverlayFitsWindow(t *testing.T) {
	for _, w := range []int{60, 80, 100, 140, 200} {
		var m tea.Model = New()
		m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})

		dialog := m.(Model).renderCaps()
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
