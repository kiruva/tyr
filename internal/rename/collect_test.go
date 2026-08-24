package rename

import (
	"path/filepath"
	"testing"
)

func rels(cands []Candidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, filepath.ToSlash(c.Rel))
	}
	return out
}

func TestCollect(t *testing.T) {
	files := []string{
		"top.txt",
		"dir/one.txt",
		"dir/.hidden.txt",
		"dir/sub/two.txt",
		"other/three.txt",
	}

	tests := []struct {
		name  string
		names []string
		opt   Options
		want  []string
	}{
		{
			name:  "flat selection of files",
			names: []string{"top.txt"},
			want:  []string{"top.txt"},
		},
		{
			name:  "a directory alone contributes nothing to rename",
			names: []string{"dir"},
			want:  nil,
		},
		{
			name:  "a directory with dirs on contributes its own name",
			names: []string{"dir"},
			opt:   Options{Dirs: true},
			want:  []string{"dir"},
		},
		{
			name:  "recursive descends into the selection",
			names: []string{"dir"},
			opt:   Options{Recursive: true},
			want:  []string{"dir/one.txt", "dir/sub/two.txt"},
		},
		{
			name:  "recursive with dirs includes the directories",
			names: []string{"dir"},
			opt:   Options{Recursive: true, Dirs: true},
			want:  []string{"dir", "dir/one.txt", "dir/sub", "dir/sub/two.txt"},
		},
		{
			name:  "hidden files are opt-in",
			names: []string{"dir"},
			opt:   Options{Recursive: true, Hidden: true},
			want:  []string{"dir/.hidden.txt", "dir/one.txt", "dir/sub/two.txt"},
		},
		{
			name:  "several selected entries, sorted",
			names: []string{"other", "top.txt", "dir"},
			opt:   Options{Recursive: true},
			want:  []string{"dir/one.txt", "dir/sub/two.txt", "other/three.txt", "top.txt"},
		},
		{
			name:  "the parent entry is ignored",
			names: []string{"..", "top.txt"},
			want:  []string{"top.txt"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := tree(t, files...)
			got, truncated, err := Collect(root, tc.names, tc.opt)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if truncated {
				t.Error("unexpectedly truncated")
			}
			if !equal(rels(got), tc.want) {
				t.Errorf("got %v, want %v", rels(got), tc.want)
			}
		})
	}
}

func TestCollectRecordsModTimeAndKind(t *testing.T) {
	root := tree(t, "dir/one.txt")

	got, _, err := Collect(root, []string{"dir"}, Options{Recursive: true, Dirs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2", len(got))
	}
	if !got[0].IsDir {
		t.Error("dir should be marked as a directory")
	}
	if got[1].IsDir {
		t.Error("one.txt should not be marked as a directory")
	}
	if got[1].Mod.IsZero() {
		t.Error("a candidate should carry its mtime, for the [d] token")
	}
}

func TestCollectStopsAtTheLimit(t *testing.T) {
	root := tree(t, "dir/a", "dir/b", "dir/c", "dir/d")

	got, truncated, err := Collect(root, []string{"dir"}, Options{Recursive: true, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Error("want truncated = true once the limit is hit")
	}
	if len(got) != 2 {
		t.Errorf("got %d candidates, want the limit of 2", len(got))
	}
}

func TestCollectMissingNameIsAnError(t *testing.T) {
	root := tree(t, "a.txt")
	if _, _, err := Collect(root, []string{"gone.txt"}, Options{}); err == nil {
		t.Fatal("want an error for a name that is not there")
	}
}
