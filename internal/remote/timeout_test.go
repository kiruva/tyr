package remote

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// shortTimeouts makes the dial and keepalive limits small enough to wait out.
func shortTimeouts(t *testing.T) {
	t.Helper()
	dial, keep := dialTimeout, keepaliveTimeout
	dialTimeout, keepaliveTimeout = 300*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { dialTimeout, keepaliveTimeout = dial, keep })
}

// A host that takes the connection and then says nothing: the handshake had
// no deadline, so the connect — and the modal — waited for ever.
func TestConnectGivesUpOnASilentHost(t *testing.T) {
	testHome(t)
	shortTimeouts(t)
	s := newTestServer(t)
	s.stall = true
	s.start()

	start := time.Now()
	_, err := Connect(context.Background(), s.host(), Options{})
	if err == nil || !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("err = %v, want it to say the host did not answer", err)
	}
	if waited := time.Since(start); waited > 3*time.Second {
		t.Errorf("gave up after %s", waited)
	}
}

// Esc in the modal cancels the connect rather than leaving it to finish.
func TestConnectCanBeCancelled(t *testing.T) {
	testHome(t)
	s := newTestServer(t)
	s.stall = true
	s.start()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	if _, err := Connect(ctx, s.host(), Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if Connected(s.host()) {
		t.Error("a cancelled connect left a session behind")
	}
}

// A session whose host has stopped answering is taken for dead and redialled,
// not waited on for ever.
func TestSilentSessionIsRedialled(t *testing.T) {
	home := testHome(t)
	shortTimeouts(t)
	key := newClientKey(t, home, "id_ed25519")
	s := newTestServer(t)
	s.keys = append(s.keys, key.PublicKey())
	s.noKeepalive = true
	s.start().trust(home)

	if _, err := Connect(context.Background(), s.host(), Options{}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := client(s.host()); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("client: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client waited on a keepalive that never came")
	}
	if n := s.conns.Load(); n != 2 {
		t.Errorf("connections = %d, want the silent one redialled", n)
	}
}

// Many operations on one host at once, with the session going away under
// them: run with -race. The session was read and replaced outside any lock.
func TestClientIsSafeToShare(t *testing.T) {
	home := testHome(t)
	key := newClientKey(t, home, "id_ed25519")
	s := newTestServer(t)
	s.keys = append(s.keys, key.PublicKey())
	s.start().trust(home)
	h := s.host()

	if _, err := Connect(context.Background(), h, Options{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if c, err := client(h); err == nil && i%4 == 0 {
				_ = c.Close() // a dropped session, for the next caller to redial
			}
			_ = Connected(h)
		}(i)
	}
	wg.Add(1)
	go func() { defer wg.Done(); time.Sleep(5 * time.Millisecond); Forget(h) }()
	wg.Wait()
	ForgetAll()
}

// A listing or a mkdir that gets no answer gives up, rather than leaving the
// pane loading for good.
func TestShortCommandsHaveADeadline(t *testing.T) {
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

	was := roundTripTimeout
	roundTripTimeout = 300 * time.Millisecond
	t.Cleanup(func() { roundTripTimeout = was })

	start := time.Now()
	_, err := quickOutput(s.host(), "sleep 5")
	if err == nil || !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("err = %v, want it to say the host did not answer", err)
	}
	if waited := time.Since(start); waited > 3*time.Second {
		t.Errorf("gave up after %s", waited)
	}
}
