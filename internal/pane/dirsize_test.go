package pane

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A measured directory shows its total, and the total survives a refresh.
func TestSetDirSizeShowsTotal(t *testing.T) {
	root := seedDir(t, "sub/")
	if err := os.WriteFile(filepath.Join(root, "sub", "f.bin"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(root)
	m.SetSize(40, 20)

	entry := func() Entry {
		t.Helper()
		for _, e := range m.Entries {
			if e.Name == "sub" {
				return e
			}
		}
		t.Fatal("sub is not listed")
		return Entry{}
	}

	if entry().HasSize {
		t.Error("a directory has no size until one is measured")
	}

	m.SetDirSize("sub", 2048)
	if e := entry(); !e.HasSize || e.Size != 2048 {
		t.Fatalf("entry = %+v, want size 2048", e)
	}
	if !strings.Contains(m.View(true), "2.0KB") {
		t.Error("the measured size is not on screen")
	}

	m.Refresh()
	if e := entry(); !e.HasSize || e.Size != 2048 {
		t.Fatalf("after refresh: %+v, want the total to survive", e)
	}
	if !m.DirSizeKnown("sub") {
		t.Error("DirSizeKnown = false after measuring")
	}
}

// Moving to another directory forgets the totals: the names mean something else
// there.
func TestDirSizesClearedOnNavigate(t *testing.T) {
	root := seedDir(t, "sub/")
	if err := os.Mkdir(filepath.Join(root, "sub", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := New(root)
	m.SetSize(40, 20)
	m.SetDirSize("sub", 4096)

	m.Focus("sub")
	m.Enter()
	if m.DirSizeKnown("sub") {
		t.Error("the total followed the pane into another directory")
	}
}

func TestUnsizedDirs(t *testing.T) {
	root := seedDir(t, "a/", "b/", "file.txt")
	m := New(root)
	m.SetSize(40, 20)

	if got := m.UnsizedDirs(); len(got) != 2 {
		t.Fatalf("UnsizedDirs = %v, want both directories", got)
	}
	m.SetDirSize("a", 10)
	got := m.UnsizedDirs()
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("UnsizedDirs = %v, want [b]", got)
	}
}
