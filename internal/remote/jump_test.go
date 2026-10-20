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

// A jump host given as an IPv6 literal keeps its port: "[::1]:2222" was cut
// at the first colon.
func TestParseProxyJumpIPv6(t *testing.T) {
	hops, err := parseProxyJump("tester@[::1]:2222, ssh://[fe80::2]")
	if err != nil {
		t.Fatal(err)
	}
	want := []Host{{User: "tester", Name: "::1", Port: "2222"}, {Name: "fe80::2"}}
	if len(hops) != len(want) || hops[0] != want[0] || hops[1] != want[1] {
		t.Fatalf("hops = %+v, want %+v", hops, want)
	}
}

// nestedJumpSetup starts the target and two jump hosts, B and C, all taking
// one key, and returns the home and the servers. The config is the caller's.
func nestedJumpSetup(t *testing.T) (home string, target, b, c *testServer) {
	t.Helper()
	home = testHome(t)
	key := newClientKey(t, home, "id_ed25519")
	start := func() *testServer {
		s := newTestServer(t)
		s.keys = append(s.keys, key.PublicKey())
		s.start().trust(home)
		return s
	}
	return home, start(), start(), start()
}

func hostBlock(alias string, s *testServer, extra string) string {
	return "Host " + alias + "\n  HostName 127.0.0.1\n  Port " + s.port + "\n  User tester\n" + extra
}

// A jump host that is itself behind a jump host: the target names B, and B's
// own entry names C. B's ProxyJump was ignored, so B was dialled directly.
func TestProxyJumpOfAJumpHostIsFollowed(t *testing.T) {
	home, target, b, c := nestedJumpSetup(t)
	appendFile(t, filepath.Join(home, ".ssh", "config"),
		hostBlock("target", target, "  ProxyJump hopB\n")+
			hostBlock("hopB", b, "  ProxyJump hopC\n")+
			hostBlock("hopC", c, ""))

	if _, err := Connect(context.Background(), Host{Name: "target"}, Options{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if n := c.tunnels.Load(); n != 1 {
		t.Errorf("tunnels through C = %d, want 1 (to B)", n)
	}
	if n := b.tunnels.Load(); n != 1 {
		t.Errorf("tunnels through B = %d, want 1 (to the target)", n)
	}
}

// In a chain "a,b" only a is reached on its own terms; b is reached through a,
// as the chain says, whatever b's own entry would have it go through. That is
// how ssh reads it.
func TestProxyJumpChainIgnoresALaterHopsOwnJump(t *testing.T) {
	home, target, b, c := nestedJumpSetup(t)
	appendFile(t, filepath.Join(home, ".ssh", "config"),
		hostBlock("target", target, "  ProxyJump hopC,hopB\n")+
			hostBlock("hopB", b, "  ProxyJump nowhere.invalid\n")+
			hostBlock("hopC", c, ""))

	if _, err := Connect(context.Background(), Host{Name: "target"}, Options{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

// Jump hosts that send each other round in a circle are refused by name,
// rather than dialled until something runs out.
func TestProxyJumpLoopIsRefused(t *testing.T) {
	home, target, b, c := nestedJumpSetup(t)
	appendFile(t, filepath.Join(home, ".ssh", "config"),
		hostBlock("target", target, "  ProxyJump hopB\n")+
			hostBlock("hopB", b, "  ProxyJump hopC\n")+
			hostBlock("hopC", c, "  ProxyJump hopB\n"))

	_, err := Connect(context.Background(), Host{Name: "target"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "loop") || !strings.Contains(err.Error(), "hopB") {
		t.Fatalf("err = %v, want a loop naming hopB", err)
	}
	if n := b.conns.Load() + c.conns.Load(); n != 0 {
		t.Errorf("%d connections made before the loop was seen", n)
	}
}
