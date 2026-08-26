package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// click builds a press of one button at a point on the screen.
func click(button tea.MouseButton, x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button}
}

// wheel builds one notch of the wheel at a point.
func wheel(up bool, x, y int) tea.MouseMsg {
	button := tea.MouseButtonWheelDown
	if up {
		button = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{X: x, Y: y, Button: button}
}

// mouseApp lays out a known tree in both panes and sizes the window.
func mouseApp(t *testing.T) (tea.Model, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := newInDir(t, dir)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return m, dir
}

// The entry rows start below the border and the address bar, so row 2 is the
// first one: "..", then the directory, then the files.
func TestClickMovesTheCursor(t *testing.T) {
	m, _ := mouseApp(t)

	m, _ = m.Update(click(tea.MouseButtonLeft, 4, 4)) // third row: a.txt
	app := m.(Model)

	if app.active != 0 {
		t.Errorf("active pane = %d, want the one clicked", app.active)
	}
	cur, ok := app.panes[0].Current()
	if !ok || cur.Name != "a.txt" {
		t.Fatalf("cursor on %+v, want a.txt", cur)
	}
}

// Clicking in the other pane makes it the active one.
func TestClickSwitchesPane(t *testing.T) {
	m, _ := mouseApp(t)

	m, _ = m.Update(click(tea.MouseButtonLeft, 70, 3))
	app := m.(Model)
	if app.active != 1 {
		t.Fatalf("active pane = %d, want the right one", app.active)
	}
	if cur, _ := app.panes[1].Current(); cur.Name != "sub" {
		t.Errorf("cursor on %+v, want sub", cur)
	}
}

// Two clicks in quick succession on the same entry open it.
func TestDoubleClickOpens(t *testing.T) {
	m, dir := mouseApp(t)

	m, _ = m.Update(click(tea.MouseButtonLeft, 4, 3)) // sub
	m, _ = m.Update(click(tea.MouseButtonLeft, 4, 3))

	app := m.(Model)
	if got, want := app.panes[0].Path, filepath.Join(dir, "sub"); got != want {
		t.Fatalf("pane at %q, want %q", got, want)
	}
}

// Two clicks far apart in time are two clicks, not one double.
func TestSlowClicksDoNotOpen(t *testing.T) {
	m, dir := mouseApp(t)

	m, _ = m.Update(click(tea.MouseButtonLeft, 4, 3))
	app := m.(Model)
	app.lastClick.when = time.Now().Add(-2 * time.Second)

	var next tea.Model = app
	next, _ = next.Update(click(tea.MouseButtonLeft, 4, 3))
	if got := next.(Model).panes[0].Path; got != dir {
		t.Fatalf("pane moved to %q on two slow clicks", got)
	}
}

// The right button marks an entry, and leaves the cursor where it was clicked.
func TestRightClickSelects(t *testing.T) {
	m, _ := mouseApp(t)

	m, _ = m.Update(click(tea.MouseButtonRight, 4, 4)) // a.txt
	app := m.(Model)

	names := app.panes[0].SelectedNames()
	if len(names) != 1 || names[0] != "a.txt" {
		t.Fatalf("selected = %v, want [a.txt]", names)
	}
	if cur, _ := app.panes[0].Current(); cur.Name != "a.txt" {
		t.Errorf("cursor walked to %+v; a mouse mark should not move it on", cur)
	}
}

// The wheel moves the cursor in whichever pane it is over.
func TestWheelScrollsThePaneUnderIt(t *testing.T) {
	m, _ := mouseApp(t)

	m, _ = m.Update(wheel(false, 70, 5)) // down, over the right pane
	app := m.(Model)
	if app.active != 1 {
		t.Fatalf("active pane = %d, want the one under the wheel", app.active)
	}
	if got := app.panes[1].Cursor; got != wheelStep {
		t.Errorf("cursor = %d, want %d", got, wheelStep)
	}

	var back tea.Model = app
	back, _ = back.Update(wheel(true, 70, 5))
	if got := back.(Model).panes[1].Cursor; got != 0 {
		t.Errorf("cursor = %d after scrolling back up, want 0", got)
	}
}

// Clicking the pane's top line opens the address bar on it.
func TestClickAddressBar(t *testing.T) {
	m, _ := mouseApp(t)

	m, _ = m.Update(click(tea.MouseButtonLeft, 10, 1))
	if got := m.(Model).mode; got != modeAddress {
		t.Fatalf("mode = %v, want modeAddress", got)
	}
}

// A click on the status bar, or below the entries, does nothing.
func TestClickOutsideTheEntries(t *testing.T) {
	m, dir := mouseApp(t)

	for _, y := range []int{0, 23, 30} {
		m, _ = m.Update(click(tea.MouseButtonLeft, 4, y))
	}
	app := m.(Model)
	if app.mode != modeNormal {
		t.Errorf("mode = %v, want nothing to have happened", app.mode)
	}
	if app.panes[0].Path != dir {
		t.Errorf("the pane moved to %q", app.panes[0].Path)
	}
	if cur, _ := app.panes[0].Current(); cur.Name != ".." {
		t.Errorf("the cursor moved to %+v", cur)
	}
}

// With a dialog open the pointer is aiming at the dialog, so the panes are left
// alone; the wheel still moves through the list in front.
func TestMouseIgnoresPanesWhileADialogIsOpen(t *testing.T) {
	m, _ := mouseApp(t)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}) // help

	m, _ = m.Update(click(tea.MouseButtonLeft, 4, 4))
	app := m.(Model)
	if app.mode != modeHelp {
		t.Fatalf("mode = %v, want the overlay still open", app.mode)
	}
	if cur, _ := app.panes[0].Current(); cur.Name != ".." {
		t.Errorf("a click reached the pane behind the overlay: cursor on %+v", cur)
	}

	var scrolled tea.Model = app
	scrolled, _ = scrolled.Update(wheel(false, 50, 10))
	if got := scrolled.(Model).overlayScroll; got != wheelStep {
		t.Errorf("overlay scrolled to %d, want %d", got, wheelStep)
	}
}

// In the viewer the wheel scrolls the file.
func TestWheelInViewer(t *testing.T) {
	dir := t.TempDir()
	body := ""
	for i := 0; i < 200; i++ {
		body += "line\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	m := focusEntry(t, newInDir(t, dir), "long.txt")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m, _ = m.Update(wheel(false, 40, 10))

	if got := m.(Model).viewport.YOffset; got == 0 {
		t.Error("the wheel did not scroll the viewer")
	}
}
