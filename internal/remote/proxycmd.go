package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ProxyCommand is the other way ssh_config reaches a host: a command whose
// standard input and output are the connection, as `ssh -W host:port bastion`
// or a corporate connector provides. The command comes from the user's own
// ~/.ssh/config, so it is trusted as much as ssh trusts it — never from
// anything typed into the address bar or the connection form.

// proxyDialer returns a dial function that starts command and talks ssh over
// its standard input and output. The command already knows where it goes;
// addr is only what the connection says it is connected to, which is what
// known_hosts is checked and written against.
func proxyDialer(command string) func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, addr string) (net.Conn, error) {
		return startProxyCommand(ctx, command, addr)
	}
}

// proxyConn is a running proxy command seen as a net.Conn. Closing it closes
// the command's input and stops the command, so a connection that is given up
// on — a cancel, a timeout, a Forget — does not leave it running.
type proxyConn struct {
	cmd    *exec.Cmd
	stdin  *os.File // our end: what we write, the command reads
	stdout *os.File // our end: what the command writes, we read
	stderr *stderrTail
	addr   proxyAddr

	exited    chan struct{} // closed once the command has been waited for
	closeOnce sync.Once
}

// startProxyCommand runs command through the shell and wires it up. Plain
// os.Pipe ends rather than exec's StdinPipe/StdoutPipe: Wait closes those, and
// the command is waited for in the background while the connection is still
// being read.
func startProxyCommand(ctx context.Context, command, addr string) (*proxyConn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = inR.Close()
		_ = inW.Close()
		return nil, err
	}

	name, args := proxyShell(command)
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout = inR, outW
	errTail := &stderrTail{}
	cmd.Stderr = errTail
	startErr := cmd.Start()
	// The child has its own copies now; ours would keep the pipes open after
	// it exits, and a read would then wait for ever instead of seeing EOF.
	_ = inR.Close()
	_ = outW.Close()
	if startErr != nil {
		_ = inW.Close()
		_ = outR.Close()
		return nil, fmt.Errorf("ProxyCommand: %w", startErr)
	}

	c := &proxyConn{cmd: cmd, stdin: inW, stdout: outR, stderr: errTail, addr: proxyAddr(addr), exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(c.exited)
	}()
	return c, nil
}

// Read is the command's output. When it ends, what the command said on its
// way out is the better error: "ssh: handshake failed: EOF" says nothing a
// bastion's "no route to host" does not say better.
func (c *proxyConn) Read(p []byte) (int, error) {
	n, err := c.stdout.Read(p)
	if errors.Is(err, io.EOF) {
		select {
		case <-c.exited:
		case <-time.After(proxyExitGrace):
		}
		if msg := c.stderr.last(); msg != "" {
			return n, fmt.Errorf("ProxyCommand: %s", msg)
		}
	}
	return n, err
}

func (c *proxyConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }

// Close ends the command: its input is closed, which is how a relay knows to
// stop, and the process is killed in case it does not.
func (c *proxyConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = c.stdout.Close()
	})
	return nil
}

// proxyExitGrace is how long a read that hit the end of the command's output
// waits for the command to finish saying why.
const proxyExitGrace = 500 * time.Millisecond

func (c *proxyConn) LocalAddr() net.Addr              { return proxyAddr("proxy-command:0") }
func (c *proxyConn) RemoteAddr() net.Addr             { return c.addr }
func (c *proxyConn) SetDeadline(time.Time) error      { return nil }
func (c *proxyConn) SetReadDeadline(time.Time) error  { return nil }
func (c *proxyConn) SetWriteDeadline(time.Time) error { return nil }

// proxyAddr stands in for the addresses a pipe does not have. The remote one
// is the host:port the command was asked to reach, so known_hosts reads it
// like any other.
type proxyAddr string

func (proxyAddr) Network() string  { return "pipe" }
func (a proxyAddr) String() string { return string(a) }

// stderrTail keeps the end of what a command writes to stderr, for an error message.
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
}

// tailBytes is how much of a command's stderr is kept.
const tailBytes = 4 << 10

func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - tailBytes; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

// last is the last line the command wrote that has something in it.
func (t *stderrTail) last() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := bytes.Split(bytes.TrimSpace(t.buf), []byte("\n"))
	return strings.TrimSpace(string(lines[len(lines)-1]))
}
