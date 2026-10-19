package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in    string
		host  Host
		path  string
		match bool
	}{
		{"ssh://user@example.com/var/log", Host{User: "user", Name: "example.com"}, "/var/log", true},
		{"ssh://example.com:2222/srv", Host{Name: "example.com", Port: "2222"}, "/srv", true},
		{"ssh://example.com", Host{Name: "example.com"}, "", true},
		{"user@example.com:/var/log", Host{User: "user", Name: "example.com"}, "/var/log", true},
		{"box:", Host{Name: "box"}, "", true},
		{"box:relative/dir", Host{Name: "box"}, "relative/dir", true},

		// Local paths must never be mistaken for hosts.
		{"/etc", Host{}, "", false},
		{"./sub", Host{}, "", false},
		{"~/dev", Host{}, "", false},
		{"plainword", Host{}, "", false},
		{"", Host{}, "", false},
		{"/tmp/a:b", Host{}, "", false},
		{"@host:/x", Host{}, "", false},
	}

	for _, tc := range tests {
		h, p, ok := Parse(tc.in)
		if ok != tc.match {
			t.Fatalf("Parse(%q) matched = %v, want %v", tc.in, ok, tc.match)
		}
		if !ok {
			continue
		}
		if h != tc.host || p != tc.path {
			t.Fatalf("Parse(%q) = %+v %q, want %+v %q", tc.in, h, p, tc.host, tc.path)
		}
	}
}

func TestHostDisplay(t *testing.T) {
	h := Host{User: "kim", Name: "box", Port: "2222"}
	if got := h.Target(); got != "kim@box" {
		t.Fatalf("Target() = %q", got)
	}
	if got := h.String(); got != "kim@box:2222" {
		t.Fatalf("String() = %q", got)
	}
	if got := h.Display("/srv"); got != "kim@box:2222:/srv" {
		t.Fatalf("Display() = %q", got)
	}
	if (Host{}).IsZero() != true {
		t.Fatal("zero host should report IsZero")
	}
}

// TestShQuote guards the boundary where our strings become someone else's
// shell command.
func TestShQuote(t *testing.T) {
	tests := map[string]string{
		"plain":            "'plain'",
		"with space":       "'with space'",
		"it's":             `'it'\''s'`,
		"$(rm -rf /)":      "'$(rm -rf /)'",
		"`whoami`":         "'`whoami`'",
		"a;b&&c|d":         "'a;b&&c|d'",
		"back\\slash":      `'back\slash'`,
		"new\nline":        "'new\nline'",
		"$HOME":            "'$HOME'",
		"quote'and space'": `'quote'\''and space'\'''`,
	}
	for in, want := range tests {
		if got := shQuote(in); got != want {
			t.Fatalf("shQuote(%q) = %s, want %s", in, got, want)
		}
	}

	if got := quoteAll([]string{"a b", "c"}); got != "'a b' 'c'" {
		t.Fatalf("quoteAll = %s", got)
	}
}

func TestJoinAndParent(t *testing.T) {
	if got := Join("/srv/www", "logs"); got != "/srv/www/logs" {
		t.Fatalf("Join relative = %q", got)
	}
	if got := Join("/srv/www", "/etc"); got != "/etc" {
		t.Fatalf("Join absolute = %q", got)
	}
	if got := Join("/srv/www", ".."); got != "/srv" {
		t.Fatalf("Join .. = %q", got)
	}
	if got := Join("/srv", "~/dev"); got != "~/dev" {
		t.Fatalf("Join tilde = %q", got)
	}
	if got := Join("/srv", "~"); got != "~" {
		t.Fatalf("Join home = %q", got)
	}
	// Only ~ and ~/ mean home. An Office lock file is a name in this directory,
	// and treating it as home-relative ran rm -rf on a file in $HOME instead.
	if got := Join("/srv/x", "~$report.docx"); got != "/srv/x/~$report.docx" {
		t.Fatalf("Join ~name = %q", got)
	}
	if got := Child("/srv/x", "~backup"); got != "/srv/x/~backup" {
		t.Fatalf("Child = %q", got)
	}
	if got := Parent("/srv/www"); got != "/srv" {
		t.Fatalf("Parent = %q", got)
	}
	if got := Parent("/"); got != "/" {
		t.Fatalf("Parent(/) = %q", got)
	}
}

func TestParseListLine(t *testing.T) {
	tests := []struct {
		line  string
		name  string
		isDir bool
		size  int64
		epoch int64
		ok    bool
	}{
		{"drwxr-xr-x 2 1000 1000 4096 1712345678 projects", "projects", true, 4096, 1712345678, true},
		{"-rw-r--r-- 1 1000 1000  512 1712345678 notes.txt", "notes.txt", false, 512, 1712345678, true},
		{"-rw-r--r-- 1 1000 1000  512 1712345678 two words.txt", "two words.txt", false, 512, 1712345678, true},
		{"lrwxrwxrwx 1 0 0 7 1712345678 link -> /etc/passwd", "link", false, 7, 1712345678, true},
		{"crw-rw-rw- 1 0 0 1, 3 1712345678 null", "null", false, 0, 1712345678, true},
		{"drwxr-xr-x 4 501 20 128 Apr  5 09:31 Documents", "Documents", true, 128, 0, true},
		{"total 24", "", false, 0, 0, false},
		{"", "", false, 0, 0, false},
		{"garbage", "", false, 0, 0, false},
	}

	names := map[string]bool{}
	for _, tc := range tests {
		names[tc.name] = tc.name != ""
	}
	for _, tc := range tests {
		e, ok := parseListLine(tc.line, names)
		if ok != tc.ok {
			t.Fatalf("parseListLine(%q) ok = %v, want %v", tc.line, ok, tc.ok)
		}
		if !ok {
			continue
		}
		if e.Name != tc.name || e.IsDir != tc.isDir || e.Size != tc.size {
			t.Fatalf("parseListLine(%q) = %+v", tc.line, e)
		}
		if tc.epoch != 0 && !e.ModTime.Equal(time.Unix(tc.epoch, 0)) {
			t.Fatalf("parseListLine(%q) mtime = %v", tc.line, e.ModTime)
		}
	}
}

// A name is only believed when it is one the directory really has. Each of
// these rows reads as one name and is another — or reads as two.
func TestParseListLineNeedsARealName(t *testing.T) {
	names := map[string]bool{
		" data":     true, // leading space
		"data":      true,
		"two  gaps": true,
		"a -> b":    true, // a symlink whose own name has an arrow in it
		"a":         true,
		"old":       true,
		"x -> y":    true,
		"x":         false,
	}
	for _, tt := range []struct {
		line, want string
		ok         bool
	}{
		{"drwxr-xr-x 2 0 0 64 1712345678  data", " data", true},
		{"drwxr-xr-x 2 0 0 64 1712345678 data", "data", true},
		{"-rw-r--r-- 1 0 0 1 1712345678 two  gaps", "two  gaps", true},
		{"lrwxrwxrwx 1 0 0 3 1712345678 old -> new", "old", true},
		// "a -> b -> c" splits as a (-> b -> c) or as "a -> b" (-> c): both are
		// names here, so neither is guessed.
		{"lrwxrwxrwx 1 0 0 1 1712345678 a -> b -> c", "", false},
		// Not a symlink, so the arrow is part of the name — and no such name.
		{"drwxr-xr-x 2 0 0 64 1712345678 data -> old", "", false},
		// The second line of a name with a newline in it.
		{"-rw-r--r-- 1 0 0 1 1712345678 ghost", "", false},
	} {
		e, ok := parseListLine(tt.line, names)
		if ok != tt.ok || (ok && e.Name != tt.want) {
			t.Errorf("parseListLine(%q) = %q, %v; want %q, %v", tt.line, e.Name, ok, tt.want, tt.ok)
		}
	}
}

// parseListing reads the whole reply: directory, the NUL-separated names, then
// the rows.
func TestParseListing(t *testing.T) {
	out := "/srv/x\x00 data\x00data\x00multi\nline\x00\x00" +
		"total 8\n" +
		"drwxr-xr-x 2 0 0 64 1712345678  data\n" +
		"drwxr-xr-x 2 0 0 64 1712345678 data\n" +
		"-rw-r--r-- 1 0 0 5 1712345678 multi\n" +
		"line\n"
	l, err := parseListing(out)
	if err != nil {
		t.Fatal(err)
	}
	if l.Dir != "/srv/x" {
		t.Errorf("dir = %q", l.Dir)
	}
	var got []string
	for _, e := range l.Entries {
		got = append(got, e.Name)
	}
	// The name with a newline in it matches no row, and is listed by its
	// exact name rather than as the two halves ls printed.
	if strings.Join(got, "|") != " data|data|multi\nline" {
		t.Errorf("entries = %q, want the three real names and not the halves of the third", got)
	}

	if _, err := parseListing(""); err == nil {
		t.Error("empty output was accepted")
	}
}

func TestParseModeFlags(t *testing.T) {
	if m := parseMode("drwxr-xr-x"); m&os.ModeDir == 0 {
		t.Fatal("directory bit missing")
	}
	if m := parseMode("lrwxrwxrwx"); m&os.ModeSymlink == 0 {
		t.Fatal("symlink bit missing")
	}
	if m := parseMode("-rwsr-xr-x"); m.Perm()&0o100 == 0 {
		t.Fatal("setuid execute bit should still count as executable")
	}
	if got := parseMode("-rw-r--r--").Perm(); got != 0o644 {
		t.Fatalf("perm = %o, want 644", got)
	}
}

func TestParseTarVerbose(t *testing.T) {
	tests := map[string]string{
		"x home/kim/notes.txt":                     "home/kim/notes.txt",
		"a src/main.go":                            "src/main.go",
		"docs/readme.md":                           "docs/readme.md",
		"tar: Removing leading '/' from names":     "",
		"Warning: Permanently added host to known": "",
	}
	for in, want := range tests {
		if got := parseTarVerbose(in); got != want {
			t.Fatalf("parseTarVerbose(%q) = %q, want %q", in, got, want)
		}
	}
}

// shPath leaves the home directory for the shell to expand and quotes the rest,
// which shQuote alone cannot do: '~/dev' is a directory called ~ in the cwd.
func TestShPath(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"~", `"$HOME"`},
		{"~/dev", `"$HOME"/'dev'`},
		{"~/it's", `"$HOME"/'it'\''s'`},
		{"/srv/x", `'/srv/x'`},
		{"~backup", `'~backup'`},
	} {
		if got := shPath(tt.in); got != tt.want {
			t.Errorf("shPath(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

// The listing script, run by the local shells against a directory of awkward
// names, round-trips every one of them — under zsh too, whose unmatched-glob
// error would otherwise kill it in an empty directory.
func TestListScriptAgainstLocalShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	dir := t.TempDir()
	weird := []string{" data", "data", "two  gaps", "a -> b", "~$lock.docx", "it's", "multi\nline", ".hidden"}
	for _, n := range weird {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("data", filepath.Join(dir, "link -> me")); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()

	for _, shell := range []string{"sh", "bash", "zsh"} {
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		for _, d := range []string{dir, empty} {
			out, err := exec.Command(shell, "-c", listScript("cd -- "+shQuote(d))).Output()
			if err != nil {
				t.Fatalf("%s: %v", shell, err)
			}
			l, err := parseListing(string(out))
			if err != nil {
				t.Fatalf("%s: %v", shell, err)
			}
			got := map[string]bool{}
			for _, e := range l.Entries {
				got[e.Name] = true
			}
			if d == empty {
				if len(got) != 0 {
					t.Errorf("%s: empty dir listed %v", shell, got)
				}
				continue
			}
			for _, n := range append(weird, "link -> me") {
				if !got[n] {
					t.Errorf("%s: %q missing from %v", shell, n, got)
				}
			}
		}
	}
}
