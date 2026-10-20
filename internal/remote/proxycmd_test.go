package remote

import (
	"context"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"
)

// TestHelperProxy is not a test: it is the proxy command the tests below
// configure, the test binary run again. It relays its stdin and stdout to
// the host and port after "--", the way `nc` or `ssh -W` would.
func TestHelperProxy(t *testing.T) {
	if os.Getenv("TYR_PROXY_HELPER") != "1" {
		t.Skip("run as a proxy command only")
	}
	args := flag.Args()
	if len(args) != 2 {
		_, _ = os.Stderr.WriteString("helper: want host and port\n")
		os.Exit(2)
	}
	c, err := net.Dial("tcp", net.JoinHostPort(args[0], args[1]))
	if err != nil {
		_, _ = os.Stderr.WriteString("helper: " + err.Error() + "\n")
		os.Exit(1)
	}
	go func() { _, _ = io.Copy(c, os.Stdin); _ = c.Close() }()
	_, _ = io.Copy(os.Stdout, c)
	os.Exit(0)
}

func needSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the tests' proxy commands are sh command lines")
	}
}

// proxyCommandSetup starts a server and writes a config for "target" with
// the given ProxyCommand, in which BIN stands for the test binary. The
// target's HostName does not resolve, so only the command can reach it.
func proxyCommandSetup(t *testing.T, command string) *testServer {
	t.Helper()
	needSh(t)
	home := testHome(t)
	key := newClientKey(t, home, "id_ed25519")
	s := newTestServer(t)
	s.keys = append(s.keys, key.PublicKey())
	s.start().trust(home)
	t.Setenv("TYR_PROXY_HELPER", "1")

	bin, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	command = strings.ReplaceAll(command, "BIN", "'"+bin+"' '-test.run=^TestHelperProxy$' --")
	appendFile(t, filepath.Join(home, ".ssh", "known_hosts"),
		knownhosts.Line([]string{knownhosts.Normalize("box.invalid:" + s.port)}, s.hostKey.PublicKey())+"\n")
	appendFile(t, filepath.Join(home, ".ssh", "config"),
		"Host target\n  HostName box.invalid\n  Port "+s.port+"\n  User tester\n  ProxyCommand "+command+"\n")
	return s
}

// The connection goes through the command, with %h and %p filled in.
func TestProxyCommandCarriesTheConnection(t *testing.T) {
	s := proxyCommandSetup(t, "BIN 127.0.0.1 %p")
	h := Host{Name: "target"}
	if _, err := Connect(context.Background(), h, Options{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if n := s.conns.Load(); n != 1 {
		t.Errorf("connections to the server = %d, want 1", n)
	}
	Forget(h)
	if !eventually(func() bool { return s.live.Load() == 0 }) {
		t.Error("the connection through the command is still up after Forget")
	}
}

// A command that fails says why, in its own words.
func TestProxyCommandFailureIsExplained(t *testing.T) {
	proxyCommandSetup(t, "echo 'bastion: no route to host' >&2; exit 3")
	_, err := Connect(context.Background(), Host{Name: "target"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "no route to host") {
		t.Fatalf("err = %v, want the command's own message", err)
	}
}

// A command that never answers is given up on like a host that never
// answers, and does not outlive the attempt.
func TestProxyCommandThatHangsIsStopped(t *testing.T) {
	proxyCommandSetup(t, "sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Connect(ctx, Host{Name: "target"}, Options{}); err == nil {
		t.Fatal("connected through sleep")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("gave up after %s", d)
	}
}

// ProxyCommand and ProxyJump: the first one in the file wins, as in ssh.
func TestProxyCommandOrProxyJumpFirstWins(t *testing.T) {
	cases := []struct {
		body    string
		command string
		jump    string
	}{
		{"ProxyCommand nc relay 22\nProxyJump bastion\n", "nc relay 22", ""},
		{"ProxyJump bastion\nProxyCommand nc relay 22\n", "", "bastion"},
		{"ProxyCommand none\nProxyJump bastion\n", "none", ""},
	}
	for _, c := range cases {
		home := testHome(t)
		appendFile(t, filepath.Join(home, ".ssh", "config"), "Host target\n"+c.body)
		cfg := lookupSSHConfig("target")
		if cfg.ProxyCommand != c.command || cfg.ProxyJump != c.jump {
			t.Errorf("%q: command %q jump %q, want %q %q", c.body, cfg.ProxyCommand, cfg.ProxyJump, c.command, c.jump)
		}
	}
}

func TestProxyCommandTokens(t *testing.T) {
	home := testHome(t)
	appendFile(t, filepath.Join(home, ".ssh", "config"),
		"Host target\n  HostName box.example\n  Port 2200\n  User kim\n  ProxyCommand ssh -W %h:%p -l %r jump # %n %%\n")
	if got, want := lookupSSHConfig("target").ProxyCommand, "ssh -W box.example:2200 -l kim jump # target %"; got != want {
		t.Errorf("ProxyCommand = %q, want %q", got, want)
	}
}
