package remote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
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
	// COPYFILE_DISABLE: a Mac host's tar would otherwise add a ._name file of
	// extended attributes beside each one. Other tars ignore the variable.
	script := "COPYFILE_DISABLE=1 tar -C " + shQuote(parent) + " -cf - -- " + quoteAll(dotSlash(names))

	// -o: files are owned by whoever runs tyr, not by the uid the host's
	// archive names — which a tar run as root would otherwise restore.
	extract := exec.CommandContext(ctx, "tar", "-C", destDir, "-x", "-o", "-v", "-f", "-")
	return remoteToLocal(ctx, h, script, extract, names, step)
}

// Upload copies local srcs (which share a parent directory) into a remote dir.
func Upload(ctx context.Context, h Host, srcs []string, destDir string, step func(string)) error {
	if len(srcs) == 0 {
		return nil
	}

	parent := filepath.Dir(srcs[0])
	args := []string{"-C", parent, "-cvf", "-"}
	if runtime.GOOS == "darwin" {
		// macOS's tar otherwise packs every extended attribute and an
		// AppleDouble ._name file beside each one: junk on the host, and
		// attributes such as where a file was downloaded from going with it.
		args = append(args, "--no-mac-metadata", "--no-xattrs")
	}
	args = append(args, "--")
	for _, s := range srcs {
		args = append(args, dotSlash([]string{filepath.Base(s)})...)
	}
	script := "mkdir -p -- " + shQuote(destDir) + " && tar -C " + shQuote(destDir) + " -x -o -f -"

	create := exec.CommandContext(ctx, "tar", args...)
	if runtime.GOOS == "darwin" {
		create.Env = append(os.Environ(), "COPYFILE_DISABLE=1")
	}
	return localToRemote(ctx, h, script, create, step)
}

// dotSlash prefixes each name with ./ for tar's command line. bsdtar reads a
// name starting with @ as another archive whose contents are to be included,
// even after --: @notes would fail, and @old.tar would send what is inside
// old.tar instead of the file. ./ makes every name a path.
func dotSlash(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "./" + n
	}
	return out
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

// remoteToLocal pipes a remote command's stdout into a local one's stdin,
// through allowOnly so that only the requested names can come out the far end.
func remoteToLocal(ctx context.Context, h Host, script string, local *exec.Cmd, names []string, step func(string)) error {
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

	pr, pw := io.Pipe()
	local.Stdin = pr
	tailText, wait, err := startWithProgress(local, step)
	if err != nil {
		return err
	}

	// The filter runs beside the session. When it refuses an entry it closes
	// the session too: nothing is reading the host's output any more, and a
	// remote tar blocked on a full channel would keep Run from returning.
	// refused is the filter's verdict, to be read once filterDone is closed.
	var refused error
	filterDone := make(chan struct{})
	go func() {
		defer close(filterDone)
		refused = allowOnly(data, pw, names)
		if refused == nil {
			_, refused = io.Copy(io.Discard, data) // the zero padding after the end
		}
		_ = pw.CloseWithError(refused)
		if refused != nil {
			_ = sess.Close()
		}
	}()

	if err := sess.Start(script); err != nil {
		_ = pw.CloseWithError(err)
		<-filterDone // the start failure is the error worth reporting
		_ = wait()
		return remoteError(h, remoteErr.String(), err)
	}

	// Both ends are waited on together. Normally the remote one finishes first
	// and the local one follows on the EOF. But a local tar that gives up early
	// — a full disk — stops reading: the filter blocks writing to it, the host's
	// output backs up, and the remote tar waits on a full channel for ever. So
	// when the local end fails first, the pipe to it and the session are closed
	// under the other two.
	localDone := make(chan error, 1)
	go func() { localDone <- wait() }()
	runDone := make(chan error, 1)
	go func() { runDone <- sess.Wait() }()

	var runErr, localErr error
	select {
	case runErr = <-runDone:
		localErr = <-localDone
	case localErr = <-localDone:
		if localErr != nil {
			_ = pr.CloseWithError(errLocalGaveUp)
			_ = sess.Close()
		}
		runErr = <-runDone
	}
	<-filterDone

	// A refusal is the reason for everything else that failed after it. Any
	// other filter error is a symptom: the local tar gave up and stopped
	// reading, and what it said is the better message.
	var unrequested *unrequestedError
	switch {
	case errors.As(refused, &unrequested):
		return refused
	case runErr != nil && localErr == nil:
		return remoteError(h, remoteErr.String(), runErr)
	case localErr != nil:
		return fmt.Errorf("extract failed: %s", tailText())
	}
	return refused
}

// errLocalGaveUp is what the filter is told when the local end of a download
// stopped reading.
var errLocalGaveUp = errors.New("the local tar stopped reading")

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
			return strings.TrimPrefix(strings.TrimSpace(rest), "./")
		}
	}
	if strings.Contains(line, ": ") || strings.HasPrefix(line, "Warning:") {
		return "" // diagnostics, not a file name
	}
	return strings.TrimPrefix(line, "./") // dotSlash put it there
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
