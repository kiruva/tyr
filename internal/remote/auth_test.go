package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// A key in ~/.ssh gets in.
func TestConnectWithAKeyFile(t *testing.T) {
	home := testHome(t)
	key := newClientKey(t, home, "id_ed25519")
	s := newTestServer(t)
	s.keys = append(s.keys, key.PublicKey())
	s.start().trust(home)

	if _, err := Connect(context.Background(), s.host(), Options{}); err != nil {
		t.Fatal(err)
	}
	if !Connected(s.host()) {
		t.Error("not connected")
	}
}

// The agent's keys failing must not stop the key files being tried. The two
// went in as two "publickey" methods, and the ssh library does not try a
// method name twice: with an agent running, a key file was never offered.
func TestKeyFileTriedAfterTheAgent(t *testing.T) {
	home := testHome(t)
	key := newClientKey(t, home, "id_ed25519")
	startAgent(t, nil) // one key the server does not know
	s := newTestServer(t)
	s.keys = append(s.keys, key.PublicKey())
	s.start().trust(home)

	if _, err := Connect(context.Background(), s.host(), Options{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

// An agent holding more keys than the server allows attempts must not use up
// the attempts before the key file — nor before the password.
func TestManyAgentKeysDoNotCrowdOutTheRest(t *testing.T) {
	home := testHome(t)
	key := newClientKey(t, home, "id_ed25519")
	startAgent(t, nil, nil, nil, nil, nil, nil, nil, nil)
	s := newTestServer(t)
	s.keys = append(s.keys, key.PublicKey())
	s.start().trust(home)

	if _, err := Connect(context.Background(), s.host(), Options{}); err != nil {
		t.Fatalf("key file after eight agent keys: %v", err)
	}
}

func TestPasswordAfterManyAgentKeys(t *testing.T) {
	home := testHome(t)
	startAgent(t, nil, nil, nil, nil, nil, nil, nil, nil)
	s := newTestServer(t)
	s.password = "hunter2"
	s.start().trust(home)

	if _, err := Connect(context.Background(), s.host(), Options{}); !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("without a password: %v, want ErrAuthRequired", err)
	}
	if _, err := Connect(context.Background(), s.host(), Options{Password: "hunter2"}); err != nil {
		t.Fatalf("with the password: %v", err)
	}
}

// The agent is hung up on once the handshake is over. Each connect opened a
// socket to it and kept it for the life of the process.
func TestAgentConnectionIsClosed(t *testing.T) {
	home := testHome(t)
	key := newAgentKey(t)
	open := startAgent(t, key)
	s := newTestServer(t)
	s.keys = append(s.keys, key.PublicKey())
	s.start().trust(home)

	if _, err := Connect(context.Background(), s.host(), Options{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for open.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := open.Load(); n != 0 {
		t.Errorf("%d agent connections still open after the connect", n)
	}
}

// A key file with a passphrase cannot be used here. It was skipped without a
// word, and the user asked for a password with no idea why their key did not
// work; the refusal now names it.
func TestEncryptedKeyIsNamed(t *testing.T) {
	home := testHome(t)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(home, ".ssh", "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	pub, _ := ssh.NewPublicKey(priv.Public())
	s := newTestServer(t)
	s.keys = append(s.keys, pub)
	s.start().trust(home)

	_, err = Connect(context.Background(), s.host(), Options{})
	var auth *AuthRequiredError
	if !errors.Is(err, ErrAuthRequired) || !errors.As(err, &auth) {
		t.Fatalf("err = %v, want ErrAuthRequired with the key named", err)
	}
	if len(auth.Encrypted) != 1 || auth.Encrypted[0] != keyPath {
		t.Errorf("Encrypted = %v, want [%s]", auth.Encrypted, keyPath)
	}
}

// IdentitiesOnly keeps the agent's keys to itself: only the key files the
// config names are offered, so a host that would take an agent key is not
// logged in to with one the config never chose for it.
func TestIdentitiesOnlyLeavesTheAgentOut(t *testing.T) {
	for _, only := range []bool{false, true} {
		home := testHome(t)
		newClientKey(t, home, "id_other") // a key the server does not know
		agentKey := newAgentKey(t)
		startAgent(t, agentKey)
		s := newTestServer(t)
		s.keys = append(s.keys, agentKey.PublicKey())
		s.start().trust(home)

		cfg := "Host target\n  HostName 127.0.0.1\n  Port " + s.port + "\n  User tester\n  IdentityFile ~/.ssh/id_other\n"
		if only {
			cfg += "  IdentitiesOnly yes\n"
		}
		appendFile(t, filepath.Join(home, ".ssh", "config"), cfg)

		_, err := Connect(context.Background(), Host{Name: "target"}, Options{})
		switch {
		case only && !errors.Is(err, ErrAuthRequired):
			t.Errorf("IdentitiesOnly yes: %v, want ErrAuthRequired (the agent key offered anyway)", err)
		case !only && err != nil:
			t.Errorf("without IdentitiesOnly: %v", err)
		}
		ForgetAll()
	}
}

// The agent's copy of a configured key still counts under IdentitiesOnly: a
// key with a passphrase is loaded into the agent so that it can be used, and
// its file's .pub says which agent key it is.
func TestIdentitiesOnlyUsesTheAgentsCopyOfAConfiguredKey(t *testing.T) {
	home := testHome(t)
	key := newAgentKey(t)
	startAgent(t, newAgentKey(t), key)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(agentKeys[key], "", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(home, ".ssh", "id_locked")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath+".pub", ssh.MarshalAuthorizedKey(key.PublicKey()), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t)
	s.keys = append(s.keys, key.PublicKey())
	s.maxTries = 1 // the other agent key, offered first, would use the one try up
	s.start().trust(home)
	appendFile(t, filepath.Join(home, ".ssh", "config"),
		"Host target\n  HostName 127.0.0.1\n  Port "+s.port+"\n  User tester\n  IdentityFile ~/.ssh/id_locked\n  IdentitiesOnly yes\n")

	if _, err := Connect(context.Background(), Host{Name: "target"}, Options{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
}
