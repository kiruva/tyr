package pane

import (
	"sort"
	"testing"
)

// selectedNames is the marked set, ordered so it can be compared.
func selectedNames(m *Model) []string {
	out := m.SelectedNames()
	sort.Strings(out)
	return out
}

func TestSelectAllSkipsParent(t *testing.T) {
	root := seedDir(t, "a.txt", "b.txt", "sub/")
	m := New(root)
	m.SetSize(40, 20)

	if n := m.SelectAll(); n != 3 {
		t.Fatalf("SelectAll = %d, want 3", n)
	}
	got := selectedNames(&m)
	want := []string{"a.txt", "b.txt", "sub"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selected = %v, want %v", got, want)
		}
	}

	// Running it again marks nothing new.
	if n := m.SelectAll(); n != 0 {
		t.Errorf("second SelectAll = %d, want 0", n)
	}
}

func TestInvertSelection(t *testing.T) {
	root := seedDir(t, "a.txt", "b.txt", "c.txt")
	m := New(root)
	m.SetSize(40, 20)

	m.Focus("a.txt")
	m.ToggleSelect()

	if n := m.InvertSelection(); n != 2 {
		t.Fatalf("InvertSelection = %d, want 2", n)
	}
	got := selectedNames(&m)
	want := []string{"b.txt", "c.txt"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selected = %v, want %v", got, want)
		}
	}
}

func TestSelectMatchingMask(t *testing.T) {
	root := seedDir(t, "main.go", "util.go", "README.md", "notes.txt")
	m := New(root)
	m.SetSize(40, 20)

	if n := m.SelectMatching("*.go;*.md", true); n != 3 {
		t.Fatalf("SelectMatching = %d, want 3", n)
	}
	if n := m.SelectMatching("*.go", false); n != 2 {
		t.Fatalf("deselect = %d, want 2", n)
	}
	got := selectedNames(&m)
	if len(got) != 1 || got[0] != "README.md" {
		t.Fatalf("selected = %v, want [README.md]", got)
	}

	// A mask matching nothing changes nothing.
	if n := m.SelectMatching("*.rs", true); n != 0 {
		t.Errorf("mask with no matches = %d, want 0", n)
	}
}

// A mask only reaches what the pane is showing, so a filter bounds it.
func TestSelectMatchingRespectsFilter(t *testing.T) {
	root := seedDir(t, "one.go", "two.go", "three.md")
	m := New(root)
	m.SetSize(40, 20)
	m.SetFilter("one")

	if n := m.SelectMatching("*", true); n != 1 {
		t.Fatalf("SelectMatching under a filter = %d, want 1", n)
	}
	if got := selectedNames(&m); len(got) != 1 || got[0] != "one.go" {
		t.Fatalf("selected = %v, want [one.go]", got)
	}
}
