package remote

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
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

	got := knownHostsFor(sshHostConfig{}).algorithms("cataclysmic:22")
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

	got := knownHostsFor(sshHostConfig{}).algorithms("box:22")
	for _, want := range []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA} {
		if !slices.Contains(got, want) {
			t.Fatalf("algorithms = %v, missing %s", got, want)
		}
	}
}

// A host we have never seen must not constrain anything.
func TestKnownHostAlgorithmsUnknownHost(t *testing.T) {
	writeKnownHosts(t, map[string]ssh.PublicKey{"other": newEd25519Key(t)})

	if got := knownHostsFor(sshHostConfig{}).algorithms("cataclysmic:22"); got != nil {
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

	got := knownHostsFor(sshHostConfig{}).algorithms("box:22")
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

	cb, err := knownHostsFor(sshHostConfig{}).callback(h, nil)
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

	accept, err := knownHostsFor(sshHostConfig{}).callback(h, []HostKey{{Host: h, Fingerprint: ssh.FingerprintSHA256(key)}})
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

	verify, err := knownHostsFor(sshHostConfig{}).callback(h, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := verify("box:22", remoteAddr, key); err != nil {
		t.Fatalf("the recorded key should now verify: %v", err)
	}
	if got := knownHostsFor(sshHostConfig{}).algorithms("box:22"); !slices.Contains(got, ssh.KeyAlgoED25519) {
		t.Fatalf("algorithms after accepting = %v", got)
	}
}

// Saying yes to a fingerprint trusts that fingerprint, not whatever the host
// offers next. An interceptor that let the first attempt through to the real
// server — so the fingerprint the user checked was genuine — and then answers
// the retry with its own key must be asked about, not recorded and logged in to.
func TestHostKeyAcceptOnlyTrustsTheKeyShown(t *testing.T) {
	path := writeKnownHosts(t, map[string]ssh.PublicKey{"other": newEd25519Key(t)})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	h := Host{Name: "box"}
	remoteAddr := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 22}
	shown, swapped := newEd25519Key(t), newEd25519Key(t)

	cb, err := knownHostsFor(sshHostConfig{}).callback(h, []HostKey{{Host: h, Fingerprint: ssh.FingerprintSHA256(shown)}})
	if err != nil {
		t.Fatal(err)
	}
	err = cb("box:22", remoteAddr, swapped)
	var hk *HostKeyError
	if !errors.As(err, &hk) {
		t.Fatalf("a key other than the one confirmed was let in: %v", err)
	}
	if hk.Fingerprint != ssh.FingerprintSHA256(swapped) {
		t.Errorf("the error shows %s, want the key actually offered", hk.Fingerprint)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("the swapped key was recorded in known_hosts")
	}
}

// A confirmation is for the host that showed the key. The same fingerprint
// offered under another name — a jump host, say — is asked about on its own.
func TestHostKeyTrustIsPerHost(t *testing.T) {
	writeKnownHosts(t, map[string]ssh.PublicKey{"other": newEd25519Key(t)})
	key := newEd25519Key(t)
	remoteAddr := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 22}

	cb, err := knownHostsFor(sshHostConfig{}).callback(Host{Name: "jump"}, []HostKey{{Host: Host{Name: "box"}, Fingerprint: ssh.FingerprintSHA256(key)}})
	if err != nil {
		t.Fatal(err)
	}
	var hk *HostKeyError
	if err := cb("jump:22", remoteAddr, key); !errors.As(err, &hk) || hk.Host.Name != "jump" {
		t.Fatalf("err = %v, want a HostKeyError for jump", err)
	}
}

// The password goes to the one hidden prompt a password login asks, once. A
// second factor's "Verification code:" — hidden too — must not get it as well.
func TestPasswordOnlyAnswersThePasswordPrompt(t *testing.T) {
	answer := passwordOnly("hunter2")

	if got, err := answer("", "", nil, nil); err != nil || len(got) != 0 {
		t.Fatalf("an informational round = %v, %v", got, err)
	}
	got, err := answer("", "", []string{"Password: "}, []bool{false})
	if err != nil || len(got) != 1 || got[0] != "hunter2" {
		t.Fatalf("password prompt = %v, %v", got, err)
	}
	if _, err := answer("", "", []string{"Verification code: "}, []bool{false}); err == nil {
		t.Fatal("the password was offered a second time, to a code prompt")
	}

	for _, tt := range []struct {
		qs    []string
		echos []bool
	}{
		{[]string{"Username: "}, []bool{true}},
		{[]string{"Password: ", "OTP: "}, []bool{false, false}},
	} {
		if _, err := passwordOnly("hunter2")("", "", tt.qs, tt.echos); err == nil {
			t.Errorf("%q was answered with the password", tt.qs)
		}
	}
}
