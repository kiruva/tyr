package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// An ssh server in the test process, for what the integration tests cannot
// cover without a host of their own: which keys the client offers and in what
// order, jump hosts, a host that stops answering, sessions that race.

// testServer is one listening server. Configure it before start.
type testServer struct {
	t *testing.T

	keys     []ssh.PublicKey // accepted for public key auth
	password string          // accepted for password auth, when not ""
	maxTries int             // ssh's MaxAuthTries; 0 = the library default (6)

	stall       bool // accept connections and never say a word
	noKeepalive bool // never answer a global request
	exec        bool // run exec requests through the local sh (Unix only)

	hostKey ssh.Signer
	addr    string
	port    string

	offered atomic.Int32 // public keys offered, across connections
	conns   atomic.Int32 // connections accepted
	live    atomic.Int32 // ssh connections up right now
	tunnels atomic.Int32 // direct-tcpip channels opened through it

	mu   sync.Mutex
	open []net.Conn
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return &testServer{t: t, hostKey: signer}
}

func (s *testServer) start() *testServer {
	s.t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.t.Fatal(err)
	}
	s.addr = ln.Addr().String()
	_, s.port, _ = net.SplitHostPort(s.addr)
	s.t.Cleanup(func() {
		_ = ln.Close()
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, c := range s.open {
			_ = c.Close()
		}
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.conns.Add(1)
			s.mu.Lock()
			s.open = append(s.open, c)
			s.mu.Unlock()
			go s.serve(c)
		}
	}()
	return s
}

// host is how the client names this server.
func (s *testServer) host() Host { return Host{Name: "127.0.0.1", Port: s.port, User: "tester"} }

// trust writes the server's key into the test home's known_hosts.
func (s *testServer) trust(home string) {
	s.t.Helper()
	appendFile(s.t, filepath.Join(home, ".ssh", "known_hosts"),
		knownhosts.Line([]string{knownhosts.Normalize(s.addr)}, s.hostKey.PublicKey())+"\n")
}

func (s *testServer) serve(c net.Conn) {
	if s.stall {
		_, _ = io.Copy(io.Discard, c)
		return
	}
	cfg := &ssh.ServerConfig{MaxAuthTries: s.maxTries}
	cfg.AddHostKey(s.hostKey)
	cfg.PublicKeyCallback = func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		s.offered.Add(1)
		for _, k := range s.keys {
			if string(k.Marshal()) == string(key.Marshal()) {
				return nil, nil
			}
		}
		return nil, errors.New("unknown key")
	}
	if s.password != "" {
		cfg.PasswordCallback = func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if string(pw) == s.password {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		}
	}

	sc, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		_ = c.Close()
		return
	}
	defer func() { _ = sc.Close() }()
	s.live.Add(1)
	defer s.live.Add(-1)
	go func() {
		for r := range reqs {
			if s.noKeepalive {
				continue // never answered: the client's wait is the test
			}
			_ = r.Reply(true, nil)
		}
	}()
	for nc := range chans {
		switch nc.ChannelType() {
		case "session":
			go s.session(nc)
		case "direct-tcpip":
			go s.tunnel(nc)
		default:
			_ = nc.Reject(ssh.UnknownChannelType, "no")
		}
	}
}

// session runs one exec request through the local shell.
func (s *testServer) session(nc ssh.NewChannel) {
	ch, reqs, err := nc.Accept()
	if err != nil {
		return
	}
	defer func() { _ = ch.Close() }()
	for r := range reqs {
		if r.Type != "exec" || !s.exec || runtime.GOOS == "windows" {
			_ = r.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if err := ssh.Unmarshal(r.Payload, &payload); err != nil {
			_ = r.Reply(false, nil)
			continue
		}
		_ = r.Reply(true, nil)

		cmd := exec.Command("sh", "-c", payload.Command)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = ch, ch, ch.Stderr()
		status := 0
		if err := cmd.Run(); err != nil {
			status = 1
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				status = exit.ExitCode()
			}
		}
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(status))
		_, _ = ch.SendRequest("exit-status", false, b[:])
		return
	}
}

// tunnel is a jump host's half of ProxyJump: connect where asked, and pipe.
func (s *testServer) tunnel(nc ssh.NewChannel) {
	var target struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(nc.ExtraData(), &target); err != nil {
		_ = nc.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	out, err := net.Dial("tcp", net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))))
	if err != nil {
		_ = nc.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		_ = out.Close()
		return
	}
	s.tunnels.Add(1)
	go ssh.DiscardRequests(reqs)
	go func() { _, _ = io.Copy(out, ch); _ = out.Close() }()
	_, _ = io.Copy(ch, out)
	_ = ch.Close()
}

// testHome gives the test a home of its own, with an empty .ssh, and no agent
// of the user's to pick keys from. It returns the home.
func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("USER", "tester")
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ForgetAll)
	return home
}

// newClientKey makes a key pair, writes the private half to home/.ssh/name, and
// returns the signer.
func newClientKey(t *testing.T, home, name string) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", name), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// startAgent serves an agent holding keys on a socket, and points
// SSH_AUTH_SOCK at it. It returns how many connections the agent has open.
func startAgent(t *testing.T, keys ...ssh.Signer) *atomic.Int32 {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the agent is a Unix socket")
	}
	ring := agent.NewKeyring()
	for _, k := range keys {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if k != nil {
			priv = agentKeys[k]
		}
		if err := ring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := os.MkdirTemp("", "tyr-agent-") // short: a socket path has a length limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "a")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	t.Setenv("SSH_AUTH_SOCK", sock)

	var open atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			open.Add(1)
			go func() {
				_ = agent.ServeAgent(ring, c)
				open.Add(-1)
			}()
		}
	}()
	return &open
}

// agentKeys lets startAgent load a key the test also holds the signer for.
var agentKeys = map[ssh.Signer]ed25519.PrivateKey{}

// newAgentKey makes a key for startAgent, returning its signer.
func newAgentKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	agentKeys[signer] = priv
	return signer
}

func appendFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}
}

// eventually waits up to two seconds for cond, for what happens on the far
// side of a socket close.
func eventually(cond func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}
