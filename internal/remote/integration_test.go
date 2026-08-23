package remote

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// These exercise the real ssh path. They need a host that accepts key-based
// logins, so they skip unless one is named:
//
//	TYR_TEST_SSH=ssh://host:port/scratch/dir go test ./internal/remote/
//
// TYR_TEST_SSH_KEY optionally names the private key to authenticate with;
// otherwise the agent and the standard ~/.ssh key names are tried. Password
// authentication is not covered here: it needs a server configured to accept one,
// which a test cannot conjure.
func testHost(t *testing.T) (Host, string) {
	t.Helper()
	target := os.Getenv("TYR_TEST_SSH")
	if target == "" {
		t.Skip("set TYR_TEST_SSH=<host> to run the ssh integration tests")
	}
	h, base, ok := Parse(target)
	if !ok {
		t.Fatalf("TYR_TEST_SSH=%q is not a valid target", target)
	}
	if base == "" {
		base = "/tmp/tyr-test"
	}

	connectForTest(t, h)
	if err := run(h, "rm -rf -- "+shQuote(base)+" && mkdir -p -- "+shQuote(base)); err != nil {
		t.Fatalf("prepare remote scratch dir: %v", err)
	}
	t.Cleanup(func() { _ = run(h, "rm -rf -- "+shQuote(base)) })
	return h, base
}

// connectForTest opens a session, accepting the host key — these run against a
// throwaway server whose key is not in the user's known_hosts.
func connectForTest(t *testing.T, h Host) {
	t.Helper()
	t.Cleanup(func() { Forget(h) })

	opts := Options{IdentityFile: os.Getenv("TYR_TEST_SSH_KEY"), AcceptHostKey: true}
	if err := Connect(h, opts); err != nil {
		t.Fatalf("connect to %s: %v", h.String(), err)
	}
}

// TestIntegrationUnknownHostKeyIsReported checks the signal the modal keys off:
// an unrecognised host must come back as a HostKeyError carrying a fingerprint,
// not as an opaque failure, and accepting it must then work.
func TestIntegrationUnknownHostKeyIsReported(t *testing.T) {
	target := os.Getenv("TYR_TEST_SSH")
	if target == "" {
		t.Skip("set TYR_TEST_SSH=<host> to run the ssh integration tests")
	}
	h, _, ok := Parse(target)
	if !ok {
		t.Fatalf("TYR_TEST_SSH=%q is not a valid target", target)
	}

	// A fresh HOME means an empty known_hosts, so this host is unknown again.
	key := os.Getenv("TYR_TEST_SSH_KEY")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Cleanup(func() { Forget(h) })

	err := Connect(h, Options{IdentityFile: key})
	var hk *HostKeyError
	if !errors.As(err, &hk) {
		t.Fatalf("first connect = %v, want a HostKeyError", err)
	}
	if !strings.HasPrefix(hk.Fingerprint, "SHA256:") {
		t.Fatalf("fingerprint = %q", hk.Fingerprint)
	}
	if hk.KeyType == "" {
		t.Fatal("key type is empty")
	}
	if hk.Changed {
		t.Fatal("a first sighting is not a changed key")
	}

	// Accepting records it, and the recorded entry is what makes the next
	// connection succeed without asking again.
	if err := Connect(h, Options{IdentityFile: key, AcceptHostKey: true}); err != nil {
		t.Fatalf("connect after accepting: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		t.Fatalf("known_hosts: %v", err)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		t.Fatal("accepting the key did not record it")
	}

	Forget(h)
	if Connected(h) {
		t.Fatal("Forget did not close the session")
	}
	if err := Connect(h, Options{IdentityFile: key}); err != nil {
		t.Fatalf("reconnect against the recorded key: %v", err)
	}
}

func TestIntegrationListHome(t *testing.T) {
	h, _ := testHost(t)

	l, err := List(h, "")
	if err != nil {
		t.Fatalf("list home: %v", err)
	}
	if !filepath.IsAbs(l.Dir) {
		t.Fatalf("home resolved to %q, want an absolute path", l.Dir)
	}
}

// TestIntegrationRoundTrip uploads a tree, reads it back over ls, downloads it
// again and compares. The awkward names are the point: they go through a remote
// shell, so quoting bugs show up here.
func TestIntegrationRoundTrip(t *testing.T) {
	h, base := testHost(t)

	src := t.TempDir()
	files := map[string]string{
		"plain.txt":       "hello",
		"with space.txt":  "spaced",
		"it's quoted.txt": "quoted",
		"sub/nested.txt":  "nested",
	}
	for name, body := range files {
		full := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var uploaded []string
	srcs := []string{
		filepath.Join(src, "plain.txt"),
		filepath.Join(src, "with space.txt"),
		filepath.Join(src, "it's quoted.txt"),
		filepath.Join(src, "sub"),
	}
	if err := Upload(h, srcs, base, func(n string) { uploaded = append(uploaded, n) }); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if len(uploaded) == 0 {
		t.Fatal("upload reported no progress steps")
	}

	l, err := List(h, base)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]Entry{}
	for _, e := range l.Entries {
		got[e.Name] = e
	}
	for _, want := range []string{"plain.txt", "with space.txt", "it's quoted.txt", "sub"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("listing is missing %q: %+v", want, l.Entries)
		}
	}
	if !got["sub"].IsDir {
		t.Fatal("sub should be listed as a directory")
	}
	if got["plain.txt"].Size != int64(len("hello")) {
		t.Fatalf("plain.txt size = %d", got["plain.txt"].Size)
	}
	if got["plain.txt"].ModTime.IsZero() {
		t.Fatal("plain.txt has no mtime")
	}

	// AnyExist / Stat agree with the listing.
	exists, isDir, err := Stat(h, path.Join(base, "sub"))
	if err != nil || !exists || !isDir {
		t.Fatalf("Stat(sub) = %v %v %v", exists, isDir, err)
	}
	any, err := AnyExist(h, base, []string{"nope", "with space.txt"})
	if err != nil || !any {
		t.Fatalf("AnyExist = %v %v", any, err)
	}

	// Download everything back and compare contents.
	dst := t.TempDir()
	remoteSrcs := []string{
		path.Join(base, "plain.txt"),
		path.Join(base, "with space.txt"),
		path.Join(base, "it's quoted.txt"),
		path.Join(base, "sub"),
	}
	var downloaded []string
	if err := Download(h, remoteSrcs, dst, func(n string) { downloaded = append(downloaded, n) }); err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(downloaded) == 0 {
		t.Fatal("download reported no progress steps")
	}
	for name, body := range files {
		data, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil {
			t.Fatalf("read back %q: %v", name, err)
		}
		if string(data) != body {
			t.Fatalf("%q = %q, want %q", name, data, body)
		}
	}
}

func TestIntegrationTransferAndDelete(t *testing.T) {
	h, base := testHost(t)

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Upload(h, []string{filepath.Join(src, "a file.txt")}, base, func(string) {}); err != nil {
		t.Fatalf("upload: %v", err)
	}

	// Copy within the host.
	dest := path.Join(base, "copied")
	if err := Transfer(h, []string{path.Join(base, "a file.txt")}, dest, false, func(string) {}); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if exists, _, _ := Stat(h, path.Join(dest, "a file.txt")); !exists {
		t.Fatal("copy did not land")
	}
	if exists, _, _ := Stat(h, path.Join(base, "a file.txt")); !exists {
		t.Fatal("copy removed the source")
	}

	// Move within the host.
	moved := path.Join(base, "moved")
	if err := Transfer(h, []string{path.Join(base, "a file.txt")}, moved, true, func(string) {}); err != nil {
		t.Fatalf("transfer move: %v", err)
	}
	if exists, _, _ := Stat(h, path.Join(base, "a file.txt")); exists {
		t.Fatal("move left the source behind")
	}

	// Delete.
	var deleted []string
	if err := Delete(h, []string{moved, dest}, func(n string) { deleted = append(deleted, n) }); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("delete steps = %v", deleted)
	}
	if exists, _, _ := Stat(h, moved); exists {
		t.Fatal("delete did not remove the directory")
	}
}

func TestIntegrationErrorsAreReadable(t *testing.T) {
	h, base := testHost(t)

	_, err := List(h, path.Join(base, "does-not-exist"))
	if err == nil {
		t.Fatal("listing a missing directory should fail")
	}
	if len(err.Error()) < 10 {
		t.Fatalf("error is not informative: %v", err)
	}
}
