package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// acceptKey connects the way the modal does: an unknown key comes back, and the
// user confirms exactly that one. It returns the warning, if any.
func acceptKey(t *testing.T, h Host) string {
	t.Helper()
	_, err := Connect(context.Background(), h, Options{})
	var hk *HostKeyError
	if !errors.As(err, &hk) {
		t.Fatalf("first connect: %v, want a host key to confirm", err)
	}
	warning, err := Connect(context.Background(), h, Options{TrustedKeys: []HostKey{hk.Key()}})
	if err != nil {
		t.Fatalf("connect after confirming: %v", err)
	}
	return warning
}

func keyServer(t *testing.T, home string) *testServer {
	t.Helper()
	key := newClientKey(t, home, "id_ed25519")
	s := newTestServer(t)
	s.keys = append(s.keys, key.PublicKey())
	return s.start()
}

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// HashKnownHosts yes: what tyr records is hashed, as ssh would write it — the
// user asked for no host names in the clear.
func TestAcceptedKeyIsHashedWhenAsked(t *testing.T) {
	home := testHome(t)
	writeConfig(t, home, "Host *\n  HashKnownHosts yes\n")
	s := keyServer(t, home)
	acceptKey(t, s.host())

	body, err := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "127.0.0.1") || !strings.HasPrefix(string(body), "|1|") {
		t.Errorf("known_hosts is not hashed:\n%s", body)
	}

	// And the hashed line is one tyr reads back.
	ForgetAll()
	if _, err := Connect(context.Background(), s.host(), Options{}); err != nil {
		t.Errorf("reconnect against the hashed line: %v", err)
	}
}

// UserKnownHostsFile is where keys are read from and recorded to.
func TestUserKnownHostsFileIsHonoured(t *testing.T) {
	home := testHome(t)
	other := filepath.Join(home, "elsewhere", "hosts")
	writeConfig(t, home, "Host *\n  UserKnownHostsFile "+other+"\n")
	s := keyServer(t, home)
	acceptKey(t, s.host())

	if _, err := os.Stat(other); err != nil {
		t.Fatalf("the key was not recorded in UserKnownHostsFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "known_hosts")); err == nil {
		t.Error("the key went to ~/.ssh/known_hosts instead")
	}
}

// A key the system knows, in GlobalKnownHostsFile, is trusted without asking.
func TestGlobalKnownHostsFileIsRead(t *testing.T) {
	home := testHome(t)
	global := filepath.Join(home, "global_known_hosts")
	writeConfig(t, home, "Host *\n  GlobalKnownHostsFile "+global+"\n")
	s := keyServer(t, home)
	s.trust(home)
	if err := os.Rename(filepath.Join(home, ".ssh", "known_hosts"), global); err != nil {
		t.Fatal(err)
	}

	if _, err := Connect(context.Background(), s.host(), Options{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

// A known_hosts that cannot be written does not stop a connect the user has
// just confirmed. It says so instead.
func TestUnwritableKnownHostsWarns(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to")
	}
	home := testHome(t)
	s := keyServer(t, home)
	ssh := filepath.Join(home, ".ssh")
	if err := os.Chmod(ssh, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ssh, 0o700) })

	warning := acceptKey(t, s.host())
	if !strings.Contains(warning, "could not be saved") {
		t.Errorf("warning = %q, want it to say the key was not saved", warning)
	}
}
