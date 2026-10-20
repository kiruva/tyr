// Package remote browses and transfers files over ssh, using an in-process ssh
// client (golang.org/x/crypto/ssh) so that a password collected in the UI can be
// offered to the server directly. Nothing is shelled out, so no password is ever
// exposed in a command line, an environment variable, or a file.
//
// A session is opened once per host through Connect and reused; see client.go.
// The subset of ~/.ssh/config that decides where a connection goes is read by
// sshconfig.go, since a native client does not consult it on its own.
package remote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// Host identifies an ssh destination. The zero value is "not remote".
type Host struct {
	User string
	Name string
	Port string
}

// Target renders the destination as ssh expects it on the command line.
func (h Host) Target() string {
	if h.User != "" {
		return h.User + "@" + h.Name
	}
	return h.Name
}

// String renders the host for display, including a non-default port. An
// IPv6 literal goes in brackets, so the port and a path after it can still be
// told from the address.
func (h Host) String() string {
	s := h.Name
	if strings.Contains(s, ":") {
		s = "[" + s + "]"
	}
	if h.User != "" {
		s = h.User + "@" + s
	}
	if h.Port != "" {
		s += ":" + h.Port
	}
	return s
}

// IsZero reports whether the host is unset.
func (h Host) IsZero() bool { return h.Name == "" }

// Display renders "user@host:/path" the way the address bar shows it.
func (h Host) Display(p string) string { return h.String() + ":" + p }

// Parse recognises the two ways of naming a remote location:
//
//	ssh://[user@]host[:port]/path
//	[user@]host:/path            (scp style; empty path means the login dir)
//
// It deliberately rejects anything that looks like a local path so that typing
// "/etc", "./sub" or "C:\Users" in the address bar never tries to open a
// connection.
func Parse(s string) (Host, string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Host{}, "", false
	}

	if rest, ok := strings.CutPrefix(s, "ssh://"); ok {
		hostPart, p, hadSlash := strings.Cut(rest, "/")
		h, ok := parseHostPart(hostPart, true)
		if !ok {
			return Host{}, "", false
		}
		if !hadSlash {
			return h, "", true
		}
		return h, "/" + p, true
	}

	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~") {
		return Host{}, "", false
	}
	// On Windows a local path carries a colon of its own: "C:\Users" would
	// otherwise parse as the host "C" with the path "\Users".
	if looksLocal(s) {
		return Host{}, "", false
	}
	// The host ends at the first colon — unless it is an IPv6 literal in
	// brackets, which has colons of its own: "user@[::1]:/var/log".
	cut := strings.IndexByte(s, ':')
	if open := strings.IndexByte(s, '['); open >= 0 && (cut < 0 || open < cut) {
		end := strings.Index(s[open:], "]:")
		if end < 0 {
			return Host{}, "", false
		}
		cut = open + end + 1
	}
	if cut < 0 || strings.Contains(s[:cut], "/") {
		return Host{}, "", false
	}
	hostPart, p := s[:cut], s[cut+1:]
	h, ok := parseHostPart(hostPart, false)
	if !ok {
		return Host{}, "", false
	}
	return h, p, true
}

// parseHostPart splits "[user@]host[:port]" ("[user@]host" in scp form, where a
// colon already separated the path).
func parseHostPart(s string, allowPort bool) (Host, bool) {
	var h Host
	if user, rest, ok := strings.Cut(s, "@"); ok {
		if user == "" || rest == "" {
			return Host{}, false
		}
		h.User, s = user, rest
	}
	if rest, ok := strings.CutPrefix(s, "["); ok {
		// An IPv6 literal: [addr] or [addr]:port.
		name, after, ok := strings.Cut(rest, "]")
		if !ok || name == "" || strings.ContainsAny(name, " \t/@[") {
			return Host{}, false
		}
		if after != "" {
			port, ok := strings.CutPrefix(after, ":")
			if !ok || !allowPort || !validPort(port) {
				return Host{}, false
			}
			h.Port = port
		}
		h.Name = name
		return h, true
	}
	if allowPort {
		if name, port, ok := strings.Cut(s, ":"); ok {
			if !validPort(port) {
				return Host{}, false
			}
			s, h.Port = name, port
		}
	}
	if s == "" || strings.ContainsAny(s, " \t/@") {
		return Host{}, false
	}
	h.Name = s
	return h, true
}

// validPort is as much checking as a port gets here; a wrong number fails at
// the dial with a message that says so.
func validPort(port string) bool {
	return port != "" && !strings.ContainsAny(port, "abcdefghijklmnopqrstuvwxyz")
}

// Join resolves a path the user typed against the pane's current remote
// directory. Remote paths are always POSIX. "~" and "~/…" are the home
// directory and are kept as they are, for List to expand (see shPath); any
// other name starting with a tilde is a name like any other.
//
// Join is for typed input only, and does not trim it: that is the caller's to
// do, since a path built from a listing can end in a space that belongs to it. The path of an entry in a listing is Child:
// "~$report.docx" is a file in this directory, not something under $HOME.
func Join(dir, p string) string {
	switch {
	case p == "":
		return dir
	case strings.HasPrefix(p, "/"):
		return path.Clean(p)
	case p == "~" || strings.HasPrefix(p, "~/"):
		return path.Clean(p)
	default:
		return path.Clean(path.Join(dir, p))
	}
}

// Child is the path of the entry called name in dir. name comes from a
// listing (or is a name being created there), so nothing in it is interpreted.
func Child(dir, name string) string {
	return path.Join(dir, name)
}

// shPath quotes a remote path for the shell like shQuote, except that a
// leading "~" or "~/" — the home directory, as Join keeps it — becomes "$HOME",
// which the shell expands where it would not expand a quoted tilde.
func shPath(p string) string {
	switch {
	case p == "~":
		return `"$HOME"`
	case strings.HasPrefix(p, "~/"):
		return `"$HOME"/` + shQuote(strings.TrimPrefix(p, "~/"))
	default:
		return shQuote(p)
	}
}

// Parent returns the parent directory of a remote path.
func Parent(p string) string {
	if p == "/" || p == "" {
		return p
	}
	return path.Dir(p)
}

// shQuote wraps s for the remote shell. Every remote command is assembled by
// concatenation and interpreted by a login shell on the far side, so any path
// that reaches it must go through here.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteAll joins names as separate shell-quoted words.
func quoteAll(names []string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, shQuote(n))
	}
	return strings.Join(out, " ")
}

// closeOnCancel closes c once ctx is done, and returns a function that stops
// watching — call it with defer, before the deferred Close. An ssh.Session has
// no context of its own, and closing it is the only thing that makes a blocked
// Run or Wait return, so this is how a transfer is cut short.
func closeOnCancel(ctx context.Context, c io.Closer) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

// output runs a script on the far side and returns its stdout. The remote end
// runs it through the login shell, exactly as `ssh host '<script>'` would, so
// every path embedded in a script must go through shQuote.
func output(ctx context.Context, h Host, script string) (string, error) {
	c, err := client(h)
	if err != nil {
		return "", err
	}
	sess, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer func() { _ = sess.Close() }()
	defer closeOnCancel(ctx, sess)()

	var stdout, stderr strings.Builder
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	if err := sess.Run(script); err != nil {
		return "", remoteError(h, stderr.String(), err)
	}
	return stdout.String(), nil
}

// roundTripTimeout bounds the short commands the UI waits on with no way to
// cancel them — a listing, a mkdir. A variable, for the tests.
var roundTripTimeout = time.Minute

// quickOutput is output for one of those, with roundTripTimeout as its limit:
// a host that went quiet without the connection noticing left the pane
// "loading…" for good.
func quickOutput(h Host, script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), roundTripTimeout)
	defer cancel()
	out, err := output(ctx, h, script)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%s: no answer within %s", h.String(), roundTripTimeout)
	}
	return out, err
}

// run executes a remote script, discarding stdout.
func run(ctx context.Context, h Host, script string) error {
	_, err := output(ctx, h, script)
	return err
}

// remoteError prefers the remote command's own complaint ("No such file or
// directory") over the transport's generic non-zero exit status.
func remoteError(h Host, stderr string, err error) error {
	if msg := firstUsefulLine(stderr); msg != "" {
		return fmt.Errorf("%s: %s", h.String(), msg)
	}
	return fmt.Errorf("%s: %w", h.String(), err)
}

// firstUsefulLine picks the first stderr line that isn't ssh chatter.
func firstUsefulLine(s string) string {
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "",
			strings.HasPrefix(line, "Warning: Permanently added"),
			strings.HasPrefix(line, "Pseudo-terminal"):
			continue
		}
		return line
	}
	return ""
}
