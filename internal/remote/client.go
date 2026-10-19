package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Connections are held in a process-wide registry keyed by Host, so a pane only
// has to remember which host it is browsing. A password, once accepted, stays in
// the registry entry for the lifetime of the process: reconnecting after an idle
// timeout must not interrupt the user with another prompt. It is never written
// anywhere, and Forget drops it. (Dropped, not wiped: a Go string cannot be
// overwritten in place, so there is no wiping to promise.)

// ErrAuthRequired means the non-interactive methods (agent, key files) did not
// get us in and a password is needed.
var ErrAuthRequired = errors.New("password required")

// HostKeyError reports a host key that cannot be verified from known_hosts.
// Changed is the serious case: the host is known but presenting a different key.
type HostKeyError struct {
	Host        Host
	KeyType     string
	Fingerprint string
	Changed     bool
}

// Key is what the user confirms when accepting this error's key: the host it
// was offered by and its fingerprint, to be handed back in Options.TrustedKeys.
func (e *HostKeyError) Key() HostKey {
	return HostKey{Host: e.Host, Fingerprint: e.Fingerprint}
}

// HostKey is one host key the user has seen and confirmed: which host offered
// it, and its SHA256 fingerprint.
type HostKey struct {
	Host        Host
	Fingerprint string
}

func (e *HostKeyError) Error() string {
	if e.Changed {
		return fmt.Sprintf("host key for %s CHANGED (%s %s)", e.Host.String(), e.KeyType, e.Fingerprint)
	}
	return fmt.Sprintf("unknown host %s (%s %s)", e.Host.String(), e.KeyType, e.Fingerprint)
}

// Options carry what the connection modal collected.
type Options struct {
	Password     string // from the modal; never persisted
	IdentityFile string // explicit key, overriding ssh_config

	// TrustedKeys are the unknown keys the user confirmed, each tied to the host
	// that showed it. A key is trusted — and recorded in known_hosts — only if
	// the same host offers exactly that key again. "The user said yes" is not
	// enough: between the attempt that showed the fingerprint and the one that
	// follows the yes, an interceptor can swap in a key of its own.
	TrustedKeys []HostKey
}

type entry struct {
	client   *ssh.Client
	password string
	opts     Options
}

var (
	registryMu sync.Mutex
	registry   = map[Host]*entry{}
)

// Connect establishes a session for h, or reports what is still needed:
// ErrAuthRequired when a password must be collected, or *HostKeyError when the
// host key needs confirming. Calling it again with those supplied completes the
// connection. A host that is already connected is a no-op.
func Connect(h Host, opts Options) error {
	registryMu.Lock()
	if e, ok := registry[h]; ok && e.client != nil {
		registryMu.Unlock()
		return nil
	}
	registryMu.Unlock()

	client, err := dial(h, opts)
	if err != nil {
		return err
	}

	registryMu.Lock()
	// Another goroutine may have connected while we were dialling.
	if e, ok := registry[h]; ok && e.client != nil {
		registryMu.Unlock()
		_ = client.Close()
		return nil
	}
	registry[h] = &entry{client: client, password: opts.Password, opts: opts}
	registryMu.Unlock()
	return nil
}

// Connected reports whether h has a live session.
func Connected(h Host) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	e, ok := registry[h]
	return ok && e.client != nil
}

// Forget closes the session for h and drops its stored password.
func Forget(h Host) {
	registryMu.Lock()
	e, ok := registry[h]
	delete(registry, h)
	registryMu.Unlock()
	if !ok {
		return
	}
	if e.client != nil {
		_ = e.client.Close()
	}
}

// ForgetAll drops every session; called on quit.
func ForgetAll() {
	registryMu.Lock()
	all := registry
	registry = map[Host]*entry{}
	registryMu.Unlock()

	for _, e := range all {
		if e.client != nil {
			_ = e.client.Close()
		}
	}
}

// client returns the live session for h, reconnecting once with the remembered
// credentials if the connection has dropped underneath us.
func client(h Host) (*ssh.Client, error) {
	registryMu.Lock()
	e, ok := registry[h]
	registryMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("not connected to %s", h.String())
	}
	if e.client != nil {
		if _, _, err := e.client.SendRequest("keepalive@openssh.com", true, nil); err == nil {
			return e.client, nil
		}
		_ = e.client.Close()
		e.client = nil
	}

	// The key accepted for this session is in known_hosts by now, so the
	// redial verifies against it like any other; a different key is asked about
	// again rather than taken on the strength of the earlier yes.
	opts := e.opts
	opts.Password = e.password
	fresh, err := dial(h, opts)
	if err != nil {
		return nil, err
	}

	registryMu.Lock()
	e.client = fresh
	registryMu.Unlock()
	return fresh, nil
}

// dial resolves h through ssh_config, assembles the auth chain and connects.
func dial(h Host, opts Options) (*ssh.Client, error) {
	cfg := lookupSSHConfig(h.Name)
	target := h
	if target.User == "" {
		target.User = cfg.User
	}
	if target.User == "" {
		target.User = currentUser()
	}
	addr := net.JoinHostPort(firstNonEmpty(cfg.HostName, h.Name), portOf(h, cfg))

	hostKeys, err := hostKeyCallback(h, opts.TrustedKeys)
	if err != nil {
		return nil, err
	}

	identities := cfg.IdentityFiles
	if opts.IdentityFile != "" {
		identities = []string{opts.IdentityFile}
	}

	clientCfg := &ssh.ClientConfig{
		User:              target.User,
		Auth:              authMethods(opts.Password, identities),
		HostKeyCallback:   hostKeys,
		HostKeyAlgorithms: knownHostAlgorithms(addr),
		Timeout:           15 * time.Second,
	}

	conn, err := dialThroughJump(cfg, addr, clientCfg, opts.TrustedKeys)
	if err != nil {
		return nil, translateAuthError(h, opts, err)
	}
	return conn, nil
}

// dialThroughJump honours a ProxyJump entry from ssh_config. The jump host is
// reached with the non-interactive methods only — prompting for two passwords in
// sequence is not something the modal models.
func dialThroughJump(cfg sshHostConfig, addr string, clientCfg *ssh.ClientConfig, trusted []HostKey) (*ssh.Client, error) {
	if cfg.ProxyJump == "" {
		return ssh.Dial("tcp", addr, clientCfg)
	}

	jumpHost, _, ok := Parse(cfg.ProxyJump + ":")
	if !ok {
		return nil, fmt.Errorf("cannot parse ProxyJump %q from ssh config", cfg.ProxyJump)
	}
	jumpCfg := lookupSSHConfig(jumpHost.Name)
	jumpUser := firstNonEmpty(jumpHost.User, jumpCfg.User, currentUser())
	// The jump host's key is checked like the target's: an unknown one comes
	// back as a HostKeyError naming the jump host, for the modal to ask about.
	jumpKeys, err := hostKeyCallback(jumpHost, trusted)
	if err != nil {
		return nil, err
	}

	jumpAddr := net.JoinHostPort(firstNonEmpty(jumpCfg.HostName, jumpHost.Name), portOf(jumpHost, jumpCfg))
	jump, err := ssh.Dial("tcp", jumpAddr,
		&ssh.ClientConfig{
			User:              jumpUser,
			Auth:              authMethods("", jumpCfg.IdentityFiles),
			HostKeyCallback:   jumpKeys,
			HostKeyAlgorithms: knownHostAlgorithms(jumpAddr),
			Timeout:           15 * time.Second,
		})
	if err != nil {
		return nil, fmt.Errorf("jump host %s: %w — it must accept key or agent auth", cfg.ProxyJump, err)
	}

	tunnel, err := jump.Dial("tcp", addr)
	if err != nil {
		_ = jump.Close()
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(tunnel, addr, clientCfg)
	if err != nil {
		_ = jump.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// authMethods builds the chain: agent first, then explicit keys, then the
// password (which also answers keyboard-interactive, as that is how many servers
// ask for one).
func authMethods(password string, identities []string) []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	if a := agentSigners(); a != nil {
		methods = append(methods, a)
	}
	if signers := loadKeys(identities); len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}
	if password != "" {
		methods = append(methods, ssh.Password(password))
		methods = append(methods, ssh.KeyboardInteractive(passwordOnly(password)))
	}
	return methods
}

// passwordOnly answers a keyboard-interactive exchange with the password —
// once, and only to a single hidden prompt, which is how a PAM password login
// asks. Anything else is a question the modal never put to the user: a second
// factor, a code, a username. Answering those with the password would send it
// as the reply to "Verification code:", so they are refused with a reason.
func passwordOnly(password string) ssh.KeyboardInteractiveChallenge {
	given := false
	return func(_, _ string, questions []string, echos []bool) ([]string, error) {
		switch {
		case len(questions) == 0:
			return nil, nil // an informational round: nothing to answer
		case len(questions) == 1 && !echos[0] && !given:
			given = true
			return []string{password}, nil
		}
		return nil, fmt.Errorf("the host asks %q, which tyr cannot answer — "+
			"log in with a key or ssh-agent for hosts that want a second factor", strings.TrimSpace(questions[0]))
	}
}

// agentSigners exposes the running ssh-agent, if there is one.
func agentSigners() ssh.AuthMethod {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	ag := agent.NewClient(conn)
	if keys, err := ag.List(); err != nil || len(keys) == 0 {
		_ = conn.Close()
		return nil
	}
	return ssh.PublicKeysCallback(ag.Signers)
}

// loadKeys reads the unencrypted private keys among identities. An encrypted key
// is skipped rather than fatal: the agent or a password can still get us in.
func loadKeys(identities []string) []ssh.Signer {
	var signers []ssh.Signer
	for _, path := range identities {
		data, err := os.ReadFile(expandHome(path))
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}
	return signers
}

// defaultIdentities are the key files ssh itself would try.
func defaultIdentities() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []string
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		p := filepath.Join(home, ".ssh", name)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// translateAuthError turns "unable to authenticate" into the signal the modal
// acts on, so a host that needs a password says so instead of just failing.
func translateAuthError(h Host, opts Options, err error) error {
	var hk *HostKeyError
	if errors.As(err, &hk) {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, "unable to authenticate") && !strings.Contains(msg, "no supported methods") {
		return fmt.Errorf("%s: %w", h.String(), err)
	}
	if opts.Password == "" {
		return ErrAuthRequired
	}
	return fmt.Errorf("%s: authentication failed — wrong password, or the host does not allow it", h.String())
}

func portOf(h Host, cfg sshHostConfig) string {
	return firstNonEmpty(h.Port, cfg.Port, "22")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("LOGNAME")
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

// knownHostsPath is where accepted keys are recorded.
func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "known_hosts"), nil
}

// hostKeyCallback verifies against known_hosts. An unknown key becomes a
// HostKeyError carrying the fingerprint for the modal to show; when the user has
// confirmed that very key for this host (it is in trusted), it is appended to
// known_hosts so ssh itself trusts it too.
func hostKeyCallback(h Host, trusted []HostKey) (ssh.HostKeyCallback, error) {
	path, err := knownHostsPath()
	if err != nil {
		return nil, err
	}
	// knownhosts.New fails on a missing file; an empty one behaves as "nothing
	// is trusted yet", which is what we want on a fresh machine.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_ = f.Close()
		}
	}
	verify, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("read known_hosts: %w", err)
	}

	return func(hostname string, addr net.Addr, key ssh.PublicKey) error {
		err := verify(hostname, addr, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err
		}

		// "Changed" means this host is known under a *different key of the same
		// type* — the case worth alarming about. Entries of other types are not
		// a mismatch: a host that has an ed25519 line in known_hosts and offers
		// an ecdsa key is presenting a key we simply have not seen yet, which is
		// how OpenSSH reads it too.
		offered := &HostKeyError{
			Host:        h,
			KeyType:     key.Type(),
			Fingerprint: ssh.FingerprintSHA256(key),
			Changed:     sameTypeMismatch(keyErr.Want, key),
		}
		if offered.Changed || !slices.Contains(trusted, offered.Key()) {
			return offered
		}
		return appendKnownHost(path, hostname, addr, key)
	}, nil
}

// sameTypeMismatch reports whether any known key for this host has the same
// algorithm as the one offered — i.e. a genuine key change rather than a first
// sighting of a different algorithm.
func sameTypeMismatch(want []knownhosts.KnownKey, offered ssh.PublicKey) bool {
	for _, w := range want {
		if w.Key.Type() == offered.Type() {
			return true
		}
	}
	return false
}

// knownHostAlgorithms lists the host key algorithms already recorded for addr,
// so the server offers one we can verify instead of whichever it prefers.
// Without this, a host with only an ed25519 line in known_hosts that answers
// with its ecdsa key looks like a mismatch, and every such host would prompt.
//
// Returning nil leaves the default preference in place, which is right for a
// host we have never seen.
func knownHostAlgorithms(addr string) []string {
	path, err := knownHostsPath()
	if err != nil {
		return nil
	}
	verify, err := knownhosts.New(path)
	if err != nil {
		return nil
	}

	// The library exposes the keys it holds for a host only through the error
	// from a failed check, so ask it about a key it cannot possibly know.
	probe := probeKey()
	if probe == nil {
		return nil
	}
	err = verify(addr, placeholderAddr, probe)

	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) || len(keyErr.Want) == 0 {
		return nil
	}

	var algos []string
	for _, w := range keyErr.Want {
		for _, a := range signatureAlgorithms(w.Key.Type()) {
			if !slices.Contains(algos, a) {
				algos = append(algos, a)
			}
		}
	}
	return algos
}

// signatureAlgorithms expands a stored key type into the signature algorithms a
// server may use with it. An RSA host key is the only one that differs: modern
// servers sign with SHA-2 and many refuse the SHA-1 "ssh-rsa" algorithm, so
// asking for that alone would fail against exactly the hosts that are up to date.
func signatureAlgorithms(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

// placeholderAddr stands in for the remote address when probing known_hosts:
// the check needs a host:port, but the hostname we pass takes precedence.
var placeholderAddr = &net.TCPAddr{IP: net.IPv4zero, Port: 0}

var (
	probeKeyOnce  sync.Once
	probeKeyValue ssh.PublicKey
)

// probeKey is a throwaway public key used only to ask known_hosts what it holds
// for a host. It is generated once and never sent anywhere.
func probeKey() ssh.PublicKey {
	probeKeyOnce.Do(func() {
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return
		}
		if k, err := ssh.NewPublicKey(pub); err == nil {
			probeKeyValue = k
		}
	})
	return probeKeyValue
}

// appendKnownHost records a newly accepted key.
func appendKnownHost(path, hostname string, addr net.Addr, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	// Record both the name we asked for and the address it resolved to, which is
	// what ssh does; knownhosts.Line handles the [host]:port form for us.
	addrs := []string{knownhosts.Normalize(hostname)}
	if a := knownhosts.Normalize(addr.String()); a != addrs[0] {
		addrs = append(addrs, a)
	}
	if _, err := fmt.Fprintln(f, knownhosts.Line(addrs, key)); err != nil {
		return err
	}
	return nil
}
