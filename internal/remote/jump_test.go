package remote

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jumpSetup starts a target and the given number of jump hosts, all taking one
// key, all trusted, and writes ~/.ssh/config with proxyJump for "target" — in
// which JUMPn stands for the n-th jump host's user@host:port.
func jumpSetup(t *testing.T, jumps int, proxyJump string) (target *testServer, hops []*testServer) {
	t.Helper()
	home := testHome(t)
	key := newClientKey(t, home, "id_ed25519")

	start := func() *testServer {
		s := newTestServer(t)
		s.keys = append(s.keys, key.PublicKey())
		s.start().trust(home)
		return s
	}
	target = start()
	for i := 0; i < jumps; i++ {
		hops = append(hops, start())
	}
	for i := len(hops) - 1; i >= 0; i-- {
		proxyJump = strings.ReplaceAll(proxyJump, "JUMP"+string(rune('0'+i)), "tester@127.0.0.1:"+hops[i].port)
	}
	cfg := "Host target\n  HostName 127.0.0.1\n  Port " + target.port + "\n  User tester\n"
	if proxyJump != "" {
		cfg += "  ProxyJump " + proxyJump + "\n"
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return target, hops
}

// A jump host with a port: "user@host:port" lost the port, so the hop was
// dialled on 22.
func TestProxyJumpKeepsThePort(t *testing.T) {
	_, hops := jumpSetup(t, 1, "JUMP0")
	if _, err := Connect(context.Background(), Host{Name: "target"}, Options{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if n := hops[0].tunnels.Load(); n != 1 {
		t.Errorf("tunnels through the jump host = %d, want 1", n)
	}
}

// "a,b" is a chain: through a, then b, then the target.
func TestProxyJumpChain(t *testing.T) {
	_, hops := jumpSetup(t, 2, "JUMP0,JUMP1")
	if _, err := Connect(context.Background(), Host{Name: "target"}, Options{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	for i, h := range hops {
		if n := h.tunnels.Load(); n != 1 {
			t.Errorf("tunnels through hop %d = %d, want 1", i, n)
		}
	}
}

// "none" means no jump at all.
func TestProxyJumpNone(t *testing.T) {
	jumpSetup(t, 0, "none")
	if _, err := Connect(context.Background(), Host{Name: "target"}, Options{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

// Closing a session closes the jump host's under it, which nothing ever did.
func TestForgetClosesTheJumpHost(t *testing.T) {
	_, hops := jumpSetup(t, 2, "JUMP0,JUMP1")
	h := Host{Name: "target"}
	if _, err := Connect(context.Background(), h, Options{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	Forget(h)
	for i, hop := range hops {
		if !eventually(func() bool { return hop.live.Load() == 0 }) {
			t.Errorf("hop %d still has a connection up", i)
		}
	}
}
