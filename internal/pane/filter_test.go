package pane

import (
	"os"
	"path/filepath"
	"testing"
)

// seedDir writes the named empty files (and "dir/" entries) into a new temp dir.
func seedDir(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		if dir, ok := cutDirName(n); ok {
			if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(root, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func cutDirName(n string) (string, bool) {
	if len(n) > 0 && n[len(n)-1] == '/' {
		return n[:len(n)-1], true
	}
	return "", false
}

// names is what the pane is currently showing.
func names(m Model) []string {
	out := make([]string, 0, len(m.Entries))
	for _, e := range m.Entries {
		out = append(out, e.Name)
	}
	return out
}

func TestMatchName(t *testing.T) {
	cases := []struct {
		name, pattern string
		want          bool
	}{
		{"main.go", "go", true},       // substring
		{"main.go", "*.go", true},     // glob
		{"main.go", "*.md", false},    // glob that misses
		{"README.md", "readme", true}, // case-insensitive substring
		{"README.md", "*.MD", true},   // case-insensitive glob
		{"notes.txt", "", true},       // no pattern matches everything
		{"notes.txt", "note?.txt", true},
		{"a.go", "[ab].go", true},
		{"c.go", "[ab].go", false},
		{"weird[.txt", "weird[", false}, // a broken glob matches nothing
	}
	for _, c := range cases {
		if got := MatchName(c.name, c.pattern); got != c.want {
			t.Errorf("MatchName(%q, %q) = %v, want %v", c.name, c.pattern, got, c.want)
		}
	}
}

// A filter narrows the listing without moving the pane, and ".." survives it so
// the directory can still be left.
func TestFilterNarrowsListing(t *testing.T) {
	root := seedDir(t, "main.go", "util.go", "README.md", "sub/")
	m := New(root)
	m.SetSize(40, 20)

	m.SetFilter("*.go")
	got := names(m)
	want := []string{"..", "main.go", "util.go"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
	if m.Path != root {
		t.Errorf("Path = %q, want %q — a filter does not move the pane", m.Path, root)
	}

	m.ClearFilter()
	if len(m.Entries) != 5 {
		t.Errorf("after clearing: %v, want all five entries back", names(m))
	}
}

// Leaving the directory drops the filter: it described what was in the old one.
func TestFilterClearedOnNavigate(t *testing.T) {
	root := seedDir(t, "keep.txt", "sub/")
	m := New(root)
	m.SetSize(40, 20)
	m.SetFilter("keep")

	m.Focus("sub")
	m.Enter()
	if m.Filter() != "" {
		t.Errorf("Filter = %q after entering a directory, want empty", m.Filter())
	}
	if len(m.Entries) == 0 {
		t.Error("the new directory came up empty")
	}
}

// The cursor cannot be left pointing past the end of a narrowed listing.
func TestFilterClampsCursor(t *testing.T) {
	root := seedDir(t, "a.go", "b.go", "c.go", "d.md")
	m := New(root)
	m.SetSize(40, 20)
	m.Bottom()

	m.SetFilter("d.md")
	if m.Cursor >= len(m.Entries) {
		t.Fatalf("Cursor = %d with %d entries", m.Cursor, len(m.Entries))
	}
	if cur, ok := m.Current(); !ok || cur.Name != "d.md" {
		t.Errorf("cursor on %+v, want d.md", cur)
	}
}
