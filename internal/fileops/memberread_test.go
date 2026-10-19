package fileops

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type entry struct {
	name string
	body []byte
}

// writeTar builds a tar (gzipped when the name says so) without any tool.
func writeTar(t *testing.T, path string, entries ...entry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var w io.Writer = f
	if filepath.Ext(path) == ".tgz" {
		gz := gzip.NewWriter(f)
		defer func() { _ = gz.Close() }()
		w = gz
	}
	tw := tar.NewWriter(w)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.name[len(e.name)-1] == '/' {
			hdr.Typeflag, hdr.Mode, hdr.Size = tar.TypeDir, 0o755, 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, path string, entries ...entry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// A member is opened by its exact name. Read by tar or unzip, the name was a
// pattern: a[1].txt matched a1.txt, and a*.txt matched both.
func TestReadMemberByExactName(t *testing.T) {
	dir := t.TempDir()
	entries := []entry{
		{"a1.txt", []byte("one")},
		{"a[1].txt", []byte("bracketed")},
		{"a*.txt", []byte("starred")},
		{"sub/", nil},
		{"sub/deep.txt", []byte("deep")},
	}
	for _, name := range []string{"a.tar", "a.tgz", "a.zip"} {
		arc := filepath.Join(dir, name)
		if filepath.Ext(name) == ".zip" {
			writeZip(t, arc, entries...)
		} else {
			writeTar(t, arc, entries...)
		}
		for _, e := range entries {
			if e.body == nil {
				continue
			}
			got, err := ReadMember(arc, e.name)
			if err != nil || string(got) != string(e.body) {
				t.Errorf("%s: ReadMember(%q) = %q, %v; want %q", name, e.name, got, err, e.body)
			}
		}
		if _, err := ReadMember(arc, "missing.txt"); err == nil {
			t.Errorf("%s: a missing member was read", name)
		}
	}
}

// Listing reads the sizes too: every file inside a browsed archive showed 0 B.
func TestListMembersHasSizes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.tar", "a.zip"} {
		arc := filepath.Join(dir, name)
		entries := []entry{{"sub/", nil}, {"sub/f.txt", []byte("12345")}}
		if filepath.Ext(name) == ".zip" {
			writeZip(t, arc, entries...)
		} else {
			writeTar(t, arc, entries...)
		}
		ms, err := ListMembers(arc)
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) != 2 || ms[0].Path != "sub" || !ms[0].IsDir || ms[1].Path != "sub/f.txt" || ms[1].Size != 5 || ms[1].IsDir {
			t.Errorf("%s: members = %+v, want sub/ and sub/f.txt of 5 bytes", name, ms)
		}
	}
}

// A member bigger than the cap is refused, however small the archive is.
func TestReadMemberIsCapped(t *testing.T) {
	arc := filepath.Join(t.TempDir(), "bomb.tgz")
	writeTar(t, arc, entry{"zeros", make([]byte, MaxMemberBytes+1)})
	if _, err := ReadMember(arc, "zeros"); !errors.Is(err, ErrMemberTooLarge) {
		t.Fatalf("err = %v, want ErrMemberTooLarge", err)
	}
}

// Adding a file into an archive that already holds one of that name asks, as
// a copy would. Both tar and zip replaced it without a word.
func TestAddToArchiveAsksBeforeReplacing(t *testing.T) {
	for _, name := range []string{"a.tgz", "a.zip"} {
		bin := "tar"
		if filepath.Ext(name) == ".zip" {
			bin = "zip"
		}
		if _, err := exec.LookPath(bin); err != nil {
			t.Logf("%s: no %s on PATH", name, bin)
			continue
		}
		for _, tt := range []struct {
			answer ConflictAction
			notes  string // what notes.txt holds afterwards
			extra  string // the keep-both copy, if any
		}{
			{ConflictSkip, "archived", ""},
			{ConflictOverwrite, "incoming", ""},
			{ConflictKeepBoth, "archived", "docs/notes (2).txt"},
		} {
			dir := t.TempDir()
			arc := filepath.Join(dir, name)
			old := []entry{{"docs/", nil}, {"docs/notes.txt", []byte("archived")}}
			if bin == "zip" {
				writeZip(t, arc, old...)
			} else {
				writeTar(t, arc, old...)
			}
			src := filepath.Join(dir, "notes.txt")
			seed(t, src, "incoming", 0)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			res, asked := answerWith(t, Run(ctx, Job{Op: OpAddToArchive, Srcs: []string{src}, Dest: arc, VDir: "docs"}), Resolution{Action: tt.answer})
			cancel()
			if res.Err != nil {
				t.Fatalf("%s %v: %v", name, tt.answer, res.Err)
			}
			if asked != 1 {
				t.Errorf("%s %v: asked %d times, want once", name, tt.answer, asked)
			}
			if got, _ := ReadMember(arc, "docs/notes.txt"); string(got) != tt.notes {
				t.Errorf("%s %v: notes.txt holds %q, want %q", name, tt.answer, got, tt.notes)
			}
			if tt.extra != "" {
				if got, _ := ReadMember(arc, tt.extra); string(got) != "incoming" {
					t.Errorf("%s %v: %s holds %q, want the incoming file", name, tt.answer, tt.extra, got)
				}
			}
		}
	}
}

// A member's name is the archive's to choose, so it is never taken as the tool
// saying something: encrypted_notes.txt in the progress, then a full disk, is
// a full disk — not a password to ask for, again and again.
func TestNeedsPasswordIgnoresNames(t *testing.T) {
	for _, tt := range []struct {
		lines []string
		want  bool
	}{
		{[]string{"ERROR: No space left on device"}, false},
		{[]string{"Can not open encrypted archive. Wrong password?"}, true},
		{[]string{"ERROR: Data Error in encrypted file. Wrong password? : a.txt"}, true},
		{[]string{"skipping: a.txt  unable to get password"}, true},
		{[]string{"Headers Error"}, false}, // a damaged archive, not a locked one
	} {
		if got := needsPassword(tt.lines); got != tt.want {
			t.Errorf("needsPassword(%q) = %v, want %v", tt.lines, got, tt.want)
		}
	}

	// And the progress lines, which name members, never reach it.
	r := &reporter{ctx: context.Background(), ch: make(chan Event, 16)}
	p, err := newProgressPipe(r, parse7zLine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.w.WriteString("- encrypted_notes.txt\nERROR: No space left on device\n"); err != nil {
		t.Fatal(err)
	}
	p.startedChildren()
	err = p.finish("7z", errors.New("exit status 2"))
	if errors.Is(err, ErrNeedPassword) || err == nil || !strings.Contains(err.Error(), "No space left") {
		t.Errorf("finish = %v, want the disk error", err)
	}
}

// A tool's bare listing has names and nothing else, which the members say.
func TestParseMembersHasNoSizes(t *testing.T) {
	ms := parseMembers([]byte("dir/\ndir/a.txt\n"))
	if len(ms) != 2 {
		t.Fatalf("got %d members, want 2", len(ms))
	}
	for _, m := range ms {
		if !m.NoSize {
			t.Errorf("%s: NoSize = false, want true", m.Path)
		}
	}
}
