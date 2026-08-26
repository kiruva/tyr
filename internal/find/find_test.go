package find

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// tree builds a small directory tree: keys are relative paths, values contents.
// A path ending in "/" is an empty directory.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		full := filepath.Join(root, name)
		if body == "" && name[len(name)-1] == '/' {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// rels is the sorted relative path of every hit.
func rels(res []Result) []string {
	out := make([]string, 0, len(res))
	for _, r := range res {
		out = append(out, filepath.ToSlash(r.Rel))
	}
	sort.Strings(out)
	return out
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestFindByGlobRecurses(t *testing.T) {
	root := tree(t, map[string]string{
		"main.go":            "package main",
		"internal/util.go":   "package internal",
		"internal/notes.txt": "nothing here",
		"README.md":          "# hi",
	})

	res, truncated, err := Run(context.Background(), Options{Root: root, Name: "*.go"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if truncated {
		t.Error("truncated = true on a four-file tree")
	}
	if got, want := rels(res), []string{"internal/util.go", "main.go"}; !equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

// A pattern without a wildcard matches anywhere in the name, ignoring case.
func TestFindBySubstring(t *testing.T) {
	root := tree(t, map[string]string{"Makefile": "all:", "notes.txt": "x"})

	res, _, err := Run(context.Background(), Options{Root: root, Name: "make"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := rels(res), []string{"Makefile"}; !equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}

	res, _, err = Run(context.Background(), Options{Root: root, Name: "make", CaseSensitive: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("case-sensitive hits = %v, want none", rels(res))
	}
}

// A content search reports the line it matched on, and skips what cannot match.
func TestFindByContent(t *testing.T) {
	root := tree(t, map[string]string{
		"a.txt":  "first line\nneedle here\n",
		"b.txt":  "nothing\n",
		"bin.db": "head\x00needle\n", // binary: not searched
		"sub/":   "",
	})

	res, _, err := Run(context.Background(), Options{Root: root, Content: "needle"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := rels(res), []string{"a.txt"}; !equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
	if res[0].Line != 2 || res[0].Text != "needle here" {
		t.Errorf("hit = line %d %q, want line 2 \"needle here\"", res[0].Line, res[0].Text)
	}
}

// Name and content together must both match.
func TestFindNameAndContent(t *testing.T) {
	root := tree(t, map[string]string{
		"one.go":   "// todo: fix\n",
		"two.go":   "// done\n",
		"three.md": "todo: fix\n",
	})

	res, _, err := Run(context.Background(), Options{Root: root, Name: "*.go", Content: "todo"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := rels(res), []string{"one.go"}; !equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

// Dotfiles and dot-directories stay out of the way unless asked for.
func TestFindHidden(t *testing.T) {
	root := tree(t, map[string]string{
		".env":        "SECRET=1",
		".git/config": "[core]",
		"visible.txt": "x",
	})

	res, _, err := Run(context.Background(), Options{Root: root, Name: "*"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := rels(res), []string{"visible.txt"}; !equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}

	res, _, err = Run(context.Background(), Options{Root: root, Name: "*", Hidden: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := rels(res); len(got) != 4 { // .env, .git, .git/config, visible.txt
		t.Fatalf("hits with hidden on = %v, want all four", got)
	}
}

// The cap stops the walk and says so, rather than returning everything.
func TestFindCapReportsTruncation(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 20; i++ {
		files[string(rune('a'+i))+".txt"] = "x"
	}
	root := tree(t, files)

	res, truncated, err := Run(context.Background(), Options{Root: root, Name: "*.txt", Max: 5})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res) != 5 {
		t.Fatalf("hits = %d, want the cap of 5", len(res))
	}
	if !truncated {
		t.Error("truncated = false, want true once the cap is hit")
	}
}

func TestFindEmptyQuery(t *testing.T) {
	_, _, err := Run(context.Background(), Options{Root: t.TempDir()})
	if err != ErrEmptyQuery {
		t.Fatalf("err = %v, want ErrEmptyQuery", err)
	}
}

// A cancelled search comes back with the context's error, not a partial answer
// presented as a complete one.
func TestFindCancelled(t *testing.T) {
	root := tree(t, map[string]string{"a.txt": "x", "b.txt": "y"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := Run(ctx, Options{Root: root, Name: "*"})
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
