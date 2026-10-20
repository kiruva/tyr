package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFreeName(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.txt")

	got, err := FreeName(notes)
	if err != nil || got != notes {
		t.Fatalf("FreeName on a free path = %q, %v; want the path itself", got, err)
	}

	for _, want := range []string{notes, filepath.Join(dir, "notes (2).txt"), filepath.Join(dir, "notes (3).txt")} {
		got, err := FreeName(notes)
		if err != nil {
			t.Fatalf("FreeName: %v", err)
		}
		if got != want {
			t.Fatalf("FreeName = %q, want %q", got, want)
		}
		if err := os.WriteFile(got, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNthNameKeepsTheExtension(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"notes.txt", "notes (2).txt"},
		{"archive.tar.gz", "archive.tar (2).gz"},
		{".bashrc", ".bashrc (2)"},
		{"README", "README (2)"},
	} {
		if got := NthName(c.in, 2); got != c.want {
			t.Errorf("NthName(%q, 2) = %q, want %q", c.in, got, c.want)
		}
	}
}
