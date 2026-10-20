package remote

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// execServer is a connected test server that runs commands with the local sh.
func execServer(t *testing.T) Host {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the test server runs commands with sh")
	}
	home := testHome(t)
	key := newClientKey(t, home, "id_ed25519")
	s := newTestServer(t)
	s.keys = append(s.keys, key.PublicKey())
	s.exec = true
	s.start().trust(home)
	if _, err := Connect(context.Background(), s.host(), Options{}); err != nil {
		t.Fatal(err)
	}
	return s.host()
}

// A download whose local tar gives up early — a full disk, say — stopped
// reading, the host's output backed up behind it, and the far tar, blocked on
// a full channel, kept the transfer waiting for ever.
func TestDownloadEndsWhenTheLocalSideGivesUp(t *testing.T) {
	h := execServer(t)
	dir := t.TempDir()
	big := make([]byte, 16<<20)
	if err := os.WriteFile(filepath.Join(dir, "big"), big, 0o644); err != nil {
		t.Fatal(err)
	}

	script := "tar -C " + shQuote(dir) + " -cf - -- ./big"
	local := exec.Command("sh", "-c", "exit 3") // reads nothing, and is gone
	done := make(chan error, 1)
	go func() {
		done <- remoteToLocal(context.Background(), h, script, local, []string{"big"}, func(string) {})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a transfer whose local side failed reported success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the transfer hung after the local side gave up")
	}
}

// And the ordinary case still works end to end.
func TestDownloadAgainstTheTestServer(t *testing.T) {
	h := execServer(t)
	src, dst := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Download(context.Background(), h, []string{filepath.ToSlash(filepath.Join(src, "a.txt"))}, dst, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "hello" {
		t.Fatalf("downloaded %q, %v", got, err)
	}
}
