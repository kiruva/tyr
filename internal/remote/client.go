package remote

import (
	"context"
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

// AuthRequiredError is ErrAuthRequired with the key files that could not be
// tried because they have a passphrase: tyr does not ask for one, and a user
// whose key "did not work" should hear that it is ssh-agent's to unlock.
type AuthRequiredError struct {
	Encrypted []string
}

func (e *AuthRequiredError) Error() string        { return ErrAuthRequired.Error() }
func (e *AuthRequiredError) Is(target error) bool { return target == ErrAuthRequired }

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

// entry is one host in the registry. mu is held across everything that looks
// at conn and may replace it — the liveness check, a redial, the store — so two
// operations on one host never dial twice or race on the field. It is taken
// after registryMu is released, never while holding it.
type entry struct {
	mu       sync.Mutex
	conn     *conn // nil until connected, and after a drop until the redial
	ready    bool  // Connect succeeded once; client may redial from opts
	password string
	opts     Options
}

var (
	registryMu sync.Mutex
	registry   = map[Host]*entry{}
)

// errForgotten is what an operation gets when the host was forgotten — the app
// quit, or the pane let go of it — while it was connecting.
var errForgotten = errors.New("the connection was closed")

// dialTimeout bounds a connect from the first packet to the end of the
// handshake, and keepaliveTimeout how long a live-looking session has to answer
// before it is taken for dead. Variables, for the tests.
var (
	dialTimeout      = 15 * time.Second
	keepaliveTimeout = 10 * time.Second
)

// Connect establishes a session for h, or reports what is still needed:
// ErrAuthRequired when a password must be collected, or *HostKeyError when the
// host key needs confirming. Calling it again with those supplied completes the
// connection. A host that is already connected is a no-op. Cancelling ctx
// abandons the attempt. The warning, when there is one, is about a connect
// that worked and is for the user to read.
func Connect(ctx context.Context, h Host, opts Options) (warning string, err error) {
	registryMu.Lock()
	e, ok := registry[h]
	if !ok {
		e = &entry{}
		registry[h] = e
	}
	registryMu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.conn != nil {
		return "", nil
	}
	c, warning, err := dial(ctx, h, opts)
	if err != nil {
		return "", err
	}
	if !registered(h, e) {
		_ = c.Close()
		return "", errForgotten
	}
	e.conn, e.ready, e.password, e.opts = c, true, opts.Password, opts
	return warning, nil
}

// Connected reports whether h has a session. One being set up or redialled
// right now counts as not yet.
func Connected(h Host) bool {
	registryMu.Lock()
	e, ok := registry[h]
	registryMu.Unlock()
	if !ok || !e.mu.TryLock() {
		return false
	}
	defer e.mu.Unlock()
	return e.conn != nil
}

// Forget closes the session for h and drops its stored password.
func Forget(h Host) {
	registryMu.Lock()
	e, ok := registry[h]
	delete(registry, h)
	registryMu.Unlock()
	if ok {
		e.shut()
	}
}

// ForgetAll drops every session; called on quit.
func ForgetAll() {
	registryMu.Lock()
	all := registry
	registry = map[Host]*entry{}
	registryMu.Unlock()

	for _, e := range all {
		e.shut()
	}
}

// shut closes a forgotten entry's session. One that is busy — dialling, or
// checking it is alive — is closed once it is done, off the caller's thread:
// quitting must not wait out somebody's dial timeout. Whatever the busy holder
// makes in the meantime it closes itself, finding the entry no longer
// registered.
func (e *entry) shut() {
	if e.mu.TryLock() {
		e.closeLocked()
		e.mu.Unlock()
		return
	}
	go func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.closeLocked()
	}()
}

func (e *entry) closeLocked() {
	if e.conn != nil {
		_ = e.conn.Close()
		e.conn = nil
	}
	e.password, e.opts = "", Options{}
}

// registered reports whether e is still h's entry. Called with e.mu held.
func registered(h Host, e *entry) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	return registry[h] == e
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

	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.ready {
		return nil, fmt.Errorf("not connected to %s", h.String())
	}
	if e.conn != nil {
		if alive(e.conn.Client) {
			if !registered(h, e) {
				return nil, errForgotten
			}
			return e.conn.Client, nil
		}
		_ = e.conn.Close()
		e.conn = nil
	}

	// The key accepted for this session is in known_hosts by now, so the
	// redial verifies against it like any other; a different key is asked about
	// again rather than taken on the strength of the earlier yes.
	opts := e.opts
	opts.Password = e.password
	fresh, _, err := dial(context.Background(), h, opts)
	if err != nil {
		return nil, err
	}
	if !registered(h, e) {
		_ = fresh.Close()
		return nil, errForgotten
	}
	e.conn = fresh
	return fresh.Client, nil
}

// alive asks the session for a keepalive reply. A host that has gone quiet
// without the TCP connection noticing never answers, and the request would
// wait for ever; past keepaliveTimeout it is taken for dead. The caller closes
// it, which is what ends the request still waiting.
func alive(c *ssh.Client) bool {
	done := make(chan error, 1)
	go func() {
		_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(keepaliveTimeout):
		return false
	}
}

// conn is a session and the jump hosts it runs through, which go when it goes.
type conn struct {
	*ssh.Client
	hops []*ssh.Client // in the order they were dialled
}

// Close closes the session, then the hops from the innermost out. The jump
// clients were never closed at all, one more for every connect and redial.
func (c *conn) Close() error {
	var err error
	if c.Client != nil {
		err = c.Client.Close()
	}
	for i := len(c.hops) - 1; i >= 0; i-- {
		_ = c.hops[i].Close()
	}
	return err
}

// dial resolves h through ssh_config, assembles the auth chain and connects.
// The warning is something worth telling the user about a connect that
// worked: a host key accepted that could not be saved.
func dial(ctx context.Context, h Host, opts Options) (*conn, string, error) {
	cfg := lookupSSHConfig(h.Name)
	target := h
	if target.User == "" {
		target.User = cfg.User
	}
	if target.User == "" {
		target.User = currentUser()
	}
	addr := net.JoinHostPort(firstNonEmpty(cfg.HostName, h.Name), portOf(h, cfg))

	known := knownHostsFor(cfg)
	hostKeys, err := known.callback(h, opts.TrustedKeys)
	if err != nil {
		return nil, "", err
	}

	identities := cfg.IdentityFiles
	if opts.IdentityFile != "" {
		identities = []string{opts.IdentityFile}
	}
	keys := openKeyring(identities, cfg.IdentitiesOnly)
	defer keys.close() // the agent is needed for the handshake only

	clientCfg := &ssh.ClientConfig{
		User:              target.User,
		Auth:              authMethods(opts.Password, keys),
		HostKeyCallback:   hostKeys,
		HostKeyAlgorithms: known.algorithms(addr),
	}

	var c *conn
	var warning string
	if cfg.ProxyCommand != "" && !strings.EqualFold(cfg.ProxyCommand, "none") {
		c = &conn{}
		c.Client, err = handshake(ctx, proxyDialer(cfg.ProxyCommand), addr, clientCfg)
	} else {
		var hops []Host
		if hops, err = jumpChain(h, cfg.ProxyJump); err != nil {
			return nil, "", err
		}
		c, warning, err = dialChain(ctx, hops, addr, clientCfg, opts.TrustedKeys)
	}
	if err != nil {
		err = translateAuthError(h, opts, err)
		if errors.Is(err, ErrAuthRequired) && len(keys.encrypted) > 0 {
			err = &AuthRequiredError{Encrypted: keys.encrypted}
		}
		return nil, "", err
	}
	return c, firstNonEmpty(known.warning, warning), nil
}

// maxJumps bounds a chain of jump hosts, nested ones included.
const maxJumps = 8

// jumpChain lists the jump hosts between here and target, in the order they
// are dialled, from target's ProxyJump. The first host of a ProxyJump list is
// reached on its own terms, so its own ProxyJump goes in front of it, and so
// on down; the later ones are reached through the ones before them, as the
// list says, whatever their own entries would have — which is how ssh reads
// it. A host met twice is a loop, refused before anything is dialled.
func jumpChain(target Host, proxyJump string) ([]Host, error) {
	seen := map[string]bool{target.String(): true}
	var expand func(value string) ([]Host, error)
	expand = func(value string) ([]Host, error) {
		hops, err := parseProxyJump(value)
		if err != nil || len(hops) == 0 {
			return hops, err
		}
		for _, h := range hops {
			if seen[h.String()] {
				return nil, fmt.Errorf("ProxyJump loop in ssh config: %s is reached through itself", h.String())
			}
			seen[h.String()] = true
		}
		before, err := expand(lookupSSHConfig(hops[0].Name).ProxyJump)
		if err != nil {
			return nil, err
		}
		chain := append(before, hops...)
		if len(chain) > maxJumps {
			return nil, fmt.Errorf("ProxyJump in ssh config goes through more than %d jump hosts", maxJumps)
		}
		return chain, nil
	}
	return expand(proxyJump)
}

// dialChain connects to addr through the jump hosts, if there are any.
// The jump hosts are reached with the non-interactive methods only —
// prompting for a password per hop is not something the modal models.
func dialChain(ctx context.Context, hops []Host, addr string, clientCfg *ssh.ClientConfig, trusted []HostKey) (*conn, string, error) {
	warning := ""
	var err error

	c := &conn{}
	next := func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
	for _, hop := range hops {
		hc, hopWarning, err := dialHop(ctx, hop, next, trusted)
		if err != nil {
			_ = c.Close()
			return nil, "", fmt.Errorf("jump host %s: %w — it must accept key or agent auth", hop.String(), err)
		}
		warning = firstNonEmpty(warning, hopWarning)
		c.hops = append(c.hops, hc)
		next = func(ctx context.Context, addr string) (net.Conn, error) {
			return hc.DialContext(ctx, "tcp", addr)
		}
	}

	c.Client, err = handshake(ctx, next, addr, clientCfg)
	if err != nil {
		_ = c.Close()
		return nil, "", err
	}
	return c, warning, nil
}

// dialHop connects to one jump host, configured from its own ssh_config entry
// — all of it but its ProxyJump, which jumpChain has already taken into the
// chain.
// Its key is checked like the target's: an unknown one comes back as a
// HostKeyError naming the jump host, for the modal to ask about.
func dialHop(ctx context.Context, hop Host, next func(context.Context, string) (net.Conn, error), trusted []HostKey) (*ssh.Client, string, error) {
	hopCfg := lookupSSHConfig(hop.Name)
	known := knownHostsFor(hopCfg)
	hostKeys, err := known.callback(hop, trusted)
	if err != nil {
		return nil, "", err
	}
	addr := net.JoinHostPort(firstNonEmpty(hopCfg.HostName, hop.Name), portOf(hop, hopCfg))
	keys := openKeyring(hopCfg.IdentityFiles, hopCfg.IdentitiesOnly)
	defer keys.close()

	c, err := handshake(ctx, next, addr, &ssh.ClientConfig{
		User:              firstNonEmpty(hop.User, hopCfg.User, currentUser()),
		Auth:              authMethods("", keys),
		HostKeyCallback:   hostKeys,
		HostKeyAlgorithms: known.algorithms(addr),
	})
	return c, known.warning, err
}

// parseProxyJump reads a ProxyJump value: "none", or hops separated by commas,
// each [ssh://][user@]host[:port].
func parseProxyJump(value string) ([]Host, error) {
	if value == "" || strings.EqualFold(value, "none") {
		return nil, nil
	}
	var hops []Host
	for _, raw := range strings.Split(value, ",") {
		hop, ok := parseHostPart(strings.TrimPrefix(strings.TrimSpace(raw), "ssh://"), true)
		if !ok {
			return nil, fmt.Errorf("cannot parse ProxyJump %q from ssh config", value)
		}
		hops = append(hops, hop)
	}
	return hops, nil
}

// handshake opens a connection to addr with dialNext and runs the ssh
// handshake over it, all within dialTimeout. ssh.ClientConfig.Timeout covers
// the TCP connect only: a host that accepts the connection and then says
// nothing kept the connect — and the modal — waiting for ever. Cancelling ctx
// gives up the same way.
func handshake(ctx context.Context, dialNext func(context.Context, string) (net.Conn, error), addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	nc, err := dialNext(ctx, addr)
	if err != nil {
		return nil, gaveUp(ctx, addr, err)
	}
	// Nothing in the handshake watches ctx; closing the connection under it is
	// what makes it return.
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })
	sc, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if !stop() {
		if err == nil {
			_ = sc.Close()
		}
		return nil, gaveUp(ctx, addr, ctx.Err())
	}
	if err != nil {
		_ = nc.Close()
		return nil, err
	}
	return ssh.NewClient(sc, chans, reqs), nil
}

// gaveUp names why a dial stopped when it was the clock or a cancel; any other
// error is passed on as it is.
func gaveUp(ctx context.Context, addr string, err error) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s: no answer within %s", addr, dialTimeout)
	case errors.Is(ctx.Err(), context.Canceled):
		return ctx.Err()
	}
	return err
}

// authMethods builds the chain. The public keys go in as one method: the ssh
// library tries a method name once, so the agent's keys and the key files as
// two "publickey" methods meant the files were never offered once the agent's
// had failed. With a password in hand the password goes first — it is only
// asked for once the keys have failed, and a host allows a handful of attempts
// in all, which an agent with many keys would otherwise use up before it.
// (The password also answers keyboard-interactive, as that is how many servers
// ask for one.)
func authMethods(password string, keys *keyring) []ssh.AuthMethod {
	var methods []ssh.AuthMethod
	if password != "" {
		methods = append(methods, ssh.Password(password))
		methods = append(methods, ssh.KeyboardInteractive(passwordOnly(password)))
	}
	return append(methods, ssh.PublicKeysCallback(keys.signers))
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

// keyring is the public keys one dial offers: the key files first, in the
// order configured — they are what ssh_config named for this host — then
// whatever the agent holds that is not one of them. Under IdentitiesOnly the
// agent's keys are offered only when they are one of those files: a key with
// a passphrase is put in the agent precisely so it can be used.
type keyring struct {
	files     []ssh.Signer
	encrypted []string // key files skipped for want of a passphrase
	agentConn net.Conn

	only    bool
	allowed map[string]bool // marshalled public keys of the configured files, for only
}

// openKeyring reads the key files. The agent is not asked until the handshake
// gets to public keys.
func openKeyring(identities []string, only bool) *keyring {
	k := &keyring{only: only}
	k.files, k.encrypted = loadKeys(identities)
	if only {
		k.allowed = configuredKeys(identities, k.files)
	}
	return k
}

func (k *keyring) signers() ([]ssh.Signer, error) {
	out := slices.Clone(k.files)
	seen := map[string]bool{}
	for _, s := range out {
		seen[string(s.PublicKey().Marshal())] = true
	}
	for _, s := range k.agentSigners() {
		pub := s.PublicKey().Marshal()
		if !seen[string(pub)] && (!k.only || k.allowed[string(pub)]) {
			out = append(out, s)
		}
	}
	return out, nil
}

// configuredKeys is the public half of every configured key file: from the
// key itself where it could be read, and from the .pub beside it where it
// could not — an encrypted key does not give up its public half without the
// passphrase, which is the case the .pub file is there for, as in ssh.
func configuredKeys(identities []string, loaded []ssh.Signer) map[string]bool {
	out := map[string]bool{}
	for _, s := range loaded {
		out[string(s.PublicKey().Marshal())] = true
	}
	for _, path := range identities {
		data, err := os.ReadFile(expandHome(path) + ".pub")
		if err != nil {
			continue
		}
		if pub, _, _, _, err := ssh.ParseAuthorizedKey(data); err == nil {
			out[string(pub.Marshal())] = true
		}
	}
	return out
}

// agentSigners asks the running ssh-agent, if there is one, for its keys. The
// connection stays open for the signing that follows, until close.
func (k *keyring) agentSigners() []ssh.Signer {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" || k.agentConn != nil {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	k.agentConn = conn
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		return nil
	}
	return signers
}

// close hangs up on the agent. Every dial opened a socket to it and none was
// ever closed, one more per connect and per reconnect.
func (k *keyring) close() {
	if k.agentConn != nil {
		_ = k.agentConn.Close()
		k.agentConn = nil
	}
}

// loadKeys reads the unencrypted private keys among identities. An encrypted key
// is skipped rather than fatal — the agent or a password can still get us in —
// but named, so a refusal can say why the key did not help.
func loadKeys(identities []string) (signers []ssh.Signer, encrypted []string) {
	for _, path := range identities {
		data, err := os.ReadFile(expandHome(path))
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			encrypted = append(encrypted, path)
			continue
		}
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}
	return signers, encrypted
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
	// A host that counts attempts hangs up with "too many authentication
	// failures" once they are spent: the same verdict, said another way.
	if !strings.Contains(msg, "unable to authenticate") && !strings.Contains(msg, "no supported methods") &&
		!strings.Contains(msg, "too many authentication failures") {
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

// knownHosts is where one host's keys are looked up and recorded, as its
// ssh_config says: UserKnownHostsFile (the first is written to) and
// GlobalKnownHostsFile, with HashKnownHosts deciding how a new line is written.
// Left alone, that is ~/.ssh/known_hosts, plus /etc/ssh/ssh_known_hosts to read.
type knownHosts struct {
	read  []string // the files that exist, user files first
	write string   // where an accepted key goes; "" when nowhere
	hash  bool

	// warning is set when an accepted key could not be recorded. The connect
	// goes ahead — the user has just confirmed the key — but the next one
	// will ask again, and the user should know why.
	warning string
}

// knownHostsFor resolves cfg's known_hosts settings.
func knownHostsFor(cfg sshHostConfig) *knownHosts {
	user := cfg.UserKnownHostsFiles
	if len(user) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			user = []string{filepath.Join(home, ".ssh", "known_hosts")}
		}
	}
	global := cfg.GlobalKnownHostsFiles
	if len(global) == 0 {
		global = []string{"/etc/ssh/ssh_known_hosts"}
	}
	if len(user) == 1 && strings.EqualFold(user[0], "none") {
		user = nil
	}
	if len(global) == 1 && strings.EqualFold(global[0], "none") {
		global = nil
	}

	k := &knownHosts{hash: cfg.HashKnownHosts}
	if len(user) > 0 {
		k.write = expandHome(user[0])
	}
	for _, f := range append(user, global...) {
		f = expandHome(f)
		if info, err := os.Stat(f); err == nil && info.Mode().IsRegular() {
			k.read = append(k.read, f)
		}
	}
	return k
}

// callback verifies against known_hosts. An unknown key becomes a
// HostKeyError carrying the fingerprint for the modal to show; when the user has
// confirmed that very key for this host (it is in trusted), it is recorded so
// ssh itself trusts it too. A missing file is "nothing trusted yet", and
// nothing is created just to read it: a read-only home must not stop a connect.
func (k *knownHosts) callback(h Host, trusted []HostKey) (ssh.HostKeyCallback, error) {
	verify, err := knownhosts.New(k.read...)
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
		if err := k.record(hostname, addr, key); err != nil {
			k.warning = "the host key was accepted but could not be saved (" + err.Error() +
				") — it will be asked about again next time"
		}
		return nil
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

// algorithms lists the host key algorithms already recorded for addr,
// so the server offers one we can verify instead of whichever it prefers.
// Without this, a host with only an ed25519 line in known_hosts that answers
// with its ecdsa key looks like a mismatch, and every such host would prompt.
//
// Returning nil leaves the default preference in place, which is right for a
// host we have never seen.
func (k *knownHosts) algorithms(addr string) []string {
	verify, err := knownhosts.New(k.read...)
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

// record writes a newly accepted key to the user's known_hosts: both the name
// we asked for and the address it resolved to, which is what ssh does. With
// HashKnownHosts each name is hashed, on a line of its own as ssh writes them;
// the user who turned that on did not want host names in the file in the clear.
func (k *knownHosts) record(hostname string, addr net.Addr, key ssh.PublicKey) error {
	if k.write == "" {
		return errors.New("UserKnownHostsFile is none")
	}
	if err := os.MkdirAll(filepath.Dir(k.write), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(k.write, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	names := []string{knownhosts.Normalize(hostname)}
	if a := knownhosts.Normalize(addr.String()); a != names[0] {
		names = append(names, a)
	}
	var lines []string
	if k.hash {
		for _, n := range names {
			lines = append(lines, knownhosts.Line([]string{knownhosts.HashHostname(n)}, key))
		}
	} else {
		lines = []string{knownhosts.Line(names, key)}
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(f, l); err != nil {
			return err
		}
	}
	return nil
}
