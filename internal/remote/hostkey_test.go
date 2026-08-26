package remote

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func newEd25519Key(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newECDSAKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newRSAKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// writeKnownHosts points HOME at a temp dir holding a known_hosts with the given
// lines, and returns its path.
func writeKnownHosts(t *testing.T, entries map[string]ssh.PublicKey) string {
	t.Helper()
	home := t.TempDir()
	setHome(t, home)
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "known_hosts")

	var body []byte
	for host, key := range entries {
		body = append(body, []byte(knownhosts.Line([]string{host}, key)+"\n")...)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestKnownHostAlgorithmsPrefersRecordedTypes is the regression for a host that
// verified fine under `ssh` but reported a changed key here: without asking the
// server for the algorithm we already trust, it answers with whichever it prefers
// and every such host looks like a mismatch.
func TestKnownHostAlgorithmsPrefersRecordedTypes(t *testing.T) {
	writeKnownHosts(t, map[string]ssh.PublicKey{"cataclysmic": newEd25519Key(t)})

	got := knownHostAlgorithms("cataclysmic:22")
	if !slices.Contains(got, ssh.KeyAlgoED25519) {
		t.Fatalf("algorithms = %v, want the recorded ed25519", got)
	}
	if slices.Contains(got, ssh.KeyAlgoECDSA256) {
		t.Fatalf("algorithms = %v, should not offer types we have no key for", got)
	}
}

// An RSA entry must also allow the SHA-2 signature algorithms, or servers that
// have retired SHA-1 become unreachable.
func TestKnownHostAlgorithmsExpandsRSA(t *testing.T) {
	writeKnownHosts(t, map[string]ssh.PublicKey{"box": newRSAKey(t)})

	got := knownHostAlgorithms("box:22")
	for _, want := range []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA} {
		if !slices.Contains(got, want) {
			t.Fatalf("algorithms = %v, missing %s", got, want)
		}
	}
}

// A host we have never seen must not constrain anything.
func TestKnownHostAlgorithmsUnknownHost(t *testing.T) {
	writeKnownHosts(t, map[string]ssh.PublicKey{"other": newEd25519Key(t)})

	if got := knownHostAlgorithms("cataclysmic:22"); got != nil {
		t.Fatalf("algorithms = %v, want nil for an unknown host", got)
	}
}

func TestKnownHostAlgorithmsMultipleTypes(t *testing.T) {
	path := writeKnownHosts(t, map[string]ssh.PublicKey{"box": newEd25519Key(t)})
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(knownhosts.Line([]string{"box"}, newECDSAKey(t)) + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got := knownHostAlgorithms("box:22")
	for _, want := range []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256} {
		if !slices.Contains(got, want) {
			t.Fatalf("algorithms = %v, missing %s", got, want)
		}
	}
}

// TestSameTypeMismatch is the second half of the same bug: an ed25519 entry plus
// an offered ecdsa key is a key we have not seen, not a key that changed. Only a
// differing key of the *same* type deserves the alarm.
func TestSameTypeMismatch(t *testing.T) {
	storedEd := newEd25519Key(t)
	otherEd := newEd25519Key(t)
	ecdsaKey := newECDSAKey(t)

	want := []knownhosts.KnownKey{{Key: storedEd}}

	if sameTypeMismatch(want, ecdsaKey) {
		t.Fatal("a different algorithm must not count as a changed key")
	}
	if !sameTypeMismatch(want, otherEd) {
		t.Fatal("a different key of the same algorithm is a changed key")
	}
	if sameTypeMismatch(nil, ecdsaKey) {
		t.Fatal("an unknown host has nothing to mismatch against")
	}
}

// TestHostKeyCallbackClassification drives the callback itself, which is what
// decides between "unknown host, here is the fingerprint" and "CHANGED".
func TestHostKeyCallbackClassification(t *testing.T) {
	stored := newEd25519Key(t)
	writeKnownHosts(t, map[string]ssh.PublicKey{"box": stored})

	h := Host{Name: "box"}
	remoteAddr := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 22}

	cb, err := hostKeyCallback(h, false)
	if err != nil {
		t.Fatal(err)
	}

	// The recorded key verifies silently.
	if err := cb("box:22", remoteAddr, stored); err != nil {
		t.Fatalf("the recorded key should verify: %v", err)
	}

	// A different algorithm is a first sighting, not a change.
	err = cb("box:22", remoteAddr, newECDSAKey(t))
	hk, ok := err.(*HostKeyError)
	if !ok {
		t.Fatalf("err = %v, want a HostKeyError", err)
	}
	if hk.Changed {
		t.Fatal("a new algorithm was reported as a changed key")
	}

	// A different key of the same algorithm is a change.
	err = cb("box:22", remoteAddr, newEd25519Key(t))
	hk, ok = err.(*HostKeyError)
	if !ok {
		t.Fatalf("err = %v, want a HostKeyError", err)
	}
	if !hk.Changed {
		t.Fatal("a replaced ed25519 key should be reported as changed")
	}
}

// Accepting an unknown key records it, and the record is what makes the next
// verification pass.
func TestHostKeyAcceptRecords(t *testing.T) {
	path := writeKnownHosts(t, map[string]ssh.PublicKey{"other": newEd25519Key(t)})

	h := Host{Name: "box"}
	remoteAddr := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 22}
	key := newEd25519Key(t)

	accept, err := hostKeyCallback(h, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := accept("box:22", remoteAddr, key); err != nil {
		t.Fatalf("accepting: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Fatal("nothing was recorded")
	}

	verify, err := hostKeyCallback(h, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := verify("box:22", remoteAddr, key); err != nil {
		t.Fatalf("the recorded key should now verify: %v", err)
	}
	if got := knownHostAlgorithms("box:22"); !slices.Contains(got, ssh.KeyAlgoED25519) {
		t.Fatalf("algorithms after accepting = %v", got)
	}
}
