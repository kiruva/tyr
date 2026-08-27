package remote

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// Transfers stream a tar archive over one ssh session rather than using scp or
// sftp. Every path is quoted by us and interpreted by exactly one shell, and the
// local `tar -v` names each file as it moves, which is what drives the progress
// bar. The far side needs `tar` and a POSIX shell; nothing is installed.
//
// Cancelling ctx stops a transfer from both ends: the local tar is killed by
// os/exec, and the ssh session is closed, which is what makes a blocked Run or
// Wait return. An ssh.Session takes no context of its own — closeOnCancel below
// is the whole of the mechanism.

// Download copies remote srcs (which share a parent directory) into a local dir.
func Download(ctx context.Context, h Host, srcs []string, destDir string, step func(string)) error {
	if len(srcs) == 0 {
		return nil
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	parent := path.Dir(srcs[0])
	names := make([]string, 0, len(srcs))
	for _, s := range srcs {
		names = append(names, path.Base(s))
	}
	script := "tar -C " + shQuote(parent) + " -cf - -- " + quoteAll(names)

	extract := exec.CommandContext(ctx, "tar", "-C", destDir, "-xvf", "-")
	return remoteToLocal(ctx, h, script, extract, step)
}

// Upload copies local srcs (which share a parent directory) into a remote dir.
func Upload(ctx context.Context, h Host, srcs []string, destDir string, step func(string)) error {
	if len(srcs) == 0 {
		return nil
	}

	parent := filepath.Dir(srcs[0])
	args := []string{"-C", parent, "-cvf", "-", "--"}
	for _, s := range srcs {
		args = append(args, filepath.Base(s))
	}
	script := "mkdir -p -- " + shQuote(destDir) + " && tar -C " + shQuote(destDir) + " -xf -"

	create := exec.CommandContext(ctx, "tar", args...)
	return localToRemote(ctx, h, script, create, step)
}

// Delete removes remote paths recursively, one at a time so each reports.
func Delete(ctx context.Context, h Host, paths []string, step func(string)) error {
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := run(ctx, h, "rm -rf -- "+shQuote(p)); err != nil {
			return err
		}
		step(p)
	}
	return nil
}

// Transfer copies or moves paths within a single host, without the data ever
// crossing the network.
func Transfer(ctx context.Context, h Host, srcs []string, destDir string, move bool, step func(string)) error {
	if err := run(ctx, h, "mkdir -p -- "+shQuote(destDir)); err != nil {
		return err
	}
	for _, s := range srcs {
		if err := ctx.Err(); err != nil {
			return err
		}
		cmd := "cp -R -p --"
		if move {
			cmd = "mv --"
		}
		if err := run(ctx, h, cmd+" "+shQuote(s)+" "+shQuote(destDir)+"/"); err != nil {
			return err
		}
		step(s)
	}
	return nil
}

// remoteToLocal pipes a remote command's stdout into a local one's stdin.
func remoteToLocal(ctx context.Context, h Host, script string, local *exec.Cmd, step func(string)) error {
	c, err := client(h)
	if err != nil {
		return err
	}
	sess, err := c.NewSession()
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	defer closeOnCancel(ctx, sess)()

	data, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	var remoteErr strings.Builder
	sess.Stderr = &remoteErr

	local.Stdin = data
	names, wait, err := startWithProgress(local, step)
	if err != nil {
		return err
	}

	runErr := sess.Run(script)
	// The local end only sees EOF once the remote command has finished, so it is
	// waited on after, not before.
	localErr := wait()

	if runErr != nil {
		return remoteError(h, remoteErr.String(), runErr)
	}
	if localErr != nil {
		return fmt.Errorf("extract failed: %s", names())
	}
	return nil
}

// localToRemote pipes a local command's stdout into a remote one's stdin.
func localToRemote(ctx context.Context, h Host, script string, local *exec.Cmd, step func(string)) error {
	c, err := client(h)
	if err != nil {
		return err
	}
	sess, err := c.NewSession()
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	defer closeOnCancel(ctx, sess)()

	data, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	var remoteErr strings.Builder
	sess.Stderr = &remoteErr

	local.Stdout = writeCloser{data}
	names, wait, err := startWithProgress(local, step)
	if err != nil {
		return err
	}
	if err := sess.Start(script); err != nil {
		_ = wait()
		return remoteError(h, remoteErr.String(), err)
	}

	// The remote tar reads until its stdin closes, which happens when the local
	// tar exits — so the local end is waited on first here.
	localErr := wait()
	_ = data.Close()

	if runErr := sess.Wait(); runErr != nil {
		return remoteError(h, remoteErr.String(), runErr)
	}
	if localErr != nil {
		return fmt.Errorf("archive failed: %s", names())
	}
	return nil
}

// writeCloser adapts an ssh stdin pipe for use as a command's Stdout: os/exec
// closes what it is given, and closing the ssh pipe here would end the transfer
// before the remote tar has drained it.
type writeCloser struct{ w io.Writer }

func (w writeCloser) Write(p []byte) (int, error) { return w.w.Write(p) }

// startWithProgress runs the local half of a transfer, reporting each file tar
// names on stderr. It returns the accumulated tail (for error messages) and a
// wait function.
func startWithProgress(cmd *exec.Cmd, step func(string)) (tailFn func() string, wait func() error, err error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	// tar -v writes member names to stderr when the archive itself is on stdout.
	cmd.Stderr = pw
	if cmd.Stdout == nil {
		cmd.Stdout = pw
	}

	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, nil, err
	}
	pw.Close() // the child holds the only write end now

	t := newTail(6)
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			mu.Lock()
			t.add(line)
			mu.Unlock()
			if name := parseTarVerbose(line); name != "" {
				step(name)
			}
		}
		pr.Close()
	}()

	tailFn = func() string {
		mu.Lock()
		defer mu.Unlock()
		return t.String()
	}
	wait = func() error {
		err := cmd.Wait()
		<-done
		return err
	}
	return tailFn, wait, nil
}

// parseTarVerbose extracts a member name from a `tar -v` line. GNU tar prints
// the bare path; bsdtar prefixes "x " when extracting and "a " when adding.
func parseTarVerbose(line string) string {
	for _, p := range []string{"x ", "a "} {
		if rest, ok := strings.CutPrefix(line, p); ok {
			return strings.TrimSpace(rest)
		}
	}
	if strings.Contains(line, ": ") || strings.HasPrefix(line, "Warning:") {
		return "" // diagnostics, not a file name
	}
	return line
}

// tail keeps the last n lines for error reporting.
type tail struct {
	lines []string
	n     int
}

func newTail(n int) *tail { return &tail{n: n} }

func (t *tail) add(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.n {
		t.lines = t.lines[len(t.lines)-t.n:]
	}
}

func (t *tail) String() string {
	if len(t.lines) == 0 {
		return "no output"
	}
	return strings.Join(t.lines, "; ")
}
