package fileops

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// format identifies an archive type detected from a filename.
type format int

const (
	fmtUnknown format = iota
	fmtTar
	fmtTarGz
	fmtTarBz2
	fmtTarXz
	fmtTarZst
	fmtZip
	fmt7z
	fmtRar
)

// suffixes are checked longest-first so ".tar.gz" wins over ".gz".
var suffixes = []struct {
	ext string
	f   format
}{
	{".tar.gz", fmtTarGz}, {".tgz", fmtTarGz},
	{".tar.bz2", fmtTarBz2}, {".tbz2", fmtTarBz2}, {".tbz", fmtTarBz2},
	{".tar.xz", fmtTarXz}, {".txz", fmtTarXz},
	{".tar.zst", fmtTarZst}, {".tzst", fmtTarZst},
	{".tar", fmtTar},
	{".zip", fmtZip},
	{".7z", fmt7z},
	{".rar", fmtRar},
}

func detectFormat(name string) format {
	lower := strings.ToLower(name)
	for _, s := range suffixes {
		if strings.HasSuffix(lower, s.ext) {
			return s.f
		}
	}
	return fmtUnknown
}

// IsArchive reports whether name looks like a supported archive.
func IsArchive(name string) bool { return detectFormat(name) != fmtUnknown }

// CanBeEncrypted reports whether the archive's format supports encryption at
// all, which is what decides whether a failure is worth retrying with a
// password. The tar family compresses but never encrypts.
func CanBeEncrypted(name string) bool {
	switch detectFormat(name) {
	case fmtZip, fmt7z, fmtRar:
		return true
	default:
		return false
	}
}

// ErrNeedPassword is returned when an archive could not be extracted because it
// is encrypted and the password was absent or wrong. The app catches it and
// asks for one rather than reporting the tool's own wording.
var ErrNeedPassword = errors.New("archive is password protected")

// tarComp maps a tar format to its compression flag (empty for plain tar).
func tarComp(f format) string {
	switch f {
	case fmtTarGz:
		return "-z"
	case fmtTarBz2:
		return "-j"
	case fmtTarXz:
		return "-J"
	case fmtTarZst:
		return "--zstd"
	default:
		return ""
	}
}

func isTar(f format) bool {
	return f == fmtTar || f == fmtTarGz || f == fmtTarBz2 || f == fmtTarXz || f == fmtTarZst
}

// extractAll extracts each archive in job.Srcs into job.Dest.
func extractAll(ctx context.Context, job Job, r *reporter) error {
	if err := os.MkdirAll(job.Dest, 0o755); err != nil {
		return err
	}
	for _, arc := range job.Srcs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := extractOne(ctx, arc, job.Dest, job.Password, r); err != nil {
			return err
		}
	}
	return nil
}

func extractOne(ctx context.Context, arc, dest, password string, r *reporter) error {
	f := detectFormat(arc)
	cmd, parse := extractCommand(f, arc, dest, password)
	switch {
	case cmd.bin == "" && f == fmtUnknown:
		return fmt.Errorf("unsupported archive: %s", filepath.Base(arc))
	case cmd.bin == "":
		// The format is known; what is missing is the tool for it.
		return fmt.Errorf("no tool installed to unpack %s", filepath.Base(arc))
	}
	return r.tools.runTool(ctx, cmd, r, parse)
}

// extractCommand returns the command and line parser to extract arc into dest.
// A password is only meaningful for the encrypting formats; an encrypted .zip
// goes through 7z when it is installed, because Info-ZIP's unzip cannot read
// the AES entries that 7-Zip (and most modern zip tools) write.
func extractCommand(f format, arc, dest, password string) (toolCmd, func(string) string) {
	arc = argPath(arc)
	switch {
	case isTar(f):
		a := []string{"-x"}
		if c := tarComp(f); c != "" {
			a = append(a, c)
		}
		a = append(a, "-v", "-f", arc, "-C", dest)
		return toolCmd{bin: "tar", args: a}, parseTarLine
	case f == fmtZip:
		// unzip reports progress per file, so it handles a plain zip; 7-Zip takes
		// over for encrypted entries (unzip cannot read AES), wherever unzip is
		// not installed at all, and on Windows, where the unzip on PATH is a
		// Unix build that does not read a native path reliably.
		if password != "" || preferSevenZipForZip || tool("unzip") == "" {
			if cmd, parse := sevenZipExtract(arc, dest, password); cmd.bin != "" {
				return cmd, parse
			}
		}
		if tool("unzip") == "" {
			return toolCmd{}, nil
		}
		a := []string{"-o"}
		if password != "" {
			// -P is the only way unzip takes a password, so it lands in the
			// process table where any local user can read it. This branch is
			// reached only when 7-Zip — which takes one on stdin instead — is
			// not installed, and the alternative is refusing the archive.
			a = append(a, "-P", password)
		}
		a = append(a, arc, "-d", dest)
		return toolCmd{bin: "unzip", args: a}, parseUnzipLine
	case f == fmt7z:
		return sevenZipExtract(arc, dest, password)
	case f == fmtRar:
		// -p- keeps unrar from stopping to ask when the archive turns out to be
		// encrypted; the failure is then classified and the app asks instead.
		// A supplied password has to go on the command line, as it does for
		// unzip above: unrar re-prompts on a wrong one, so feeding it on stdin
		// would risk a hang for a tool tyr cannot test for.
		pw := "-p-"
		if password != "" {
			pw = "-p" + password
		}
		return toolCmd{bin: "unrar", args: []string{"x", "-y", pw, arc, dest + string(os.PathSeparator)}}, parseNone
	default:
		return toolCmd{}, nil
	}
}

// sevenZipExtract builds the 7-Zip command. The password goes in on stdin
// rather than as -p<password>, which would put it in the process table for
// every user on the machine to read — the same reasoning as packWith7z, which
// has always done it this way.
//
// Note that a bare -p means an *empty* password when extracting, unlike `7z a`
// where it means "prompt", so the switch is left off entirely: 7-Zip prompts
// whenever the archive turns out to be encrypted and reads the answer from
// stdin. Handing a password to an archive that is not encrypted is harmless —
// nothing prompts and the reader is never read — and a missing or wrong one
// makes 7-Zip print "Enter password" and "Can not open encrypted archive",
// which needsPassword turns into ErrNeedPassword so the app can ask. stdin is a
// strings.Reader either way, so a re-prompt hits EOF and fails instead of
// hanging.
func sevenZipExtract(arc, dest, password string) (toolCmd, func(string) string) {
	bin := tool("7z")
	if bin == "" {
		return toolCmd{}, nil
	}
	stdin := ""
	if password != "" {
		stdin = password + "\n"
	}
	a := []string{"x", "-bb1", "-bd", "-y", "-o" + dest, "--", arc}
	return toolCmd{bin: bin, args: a, stdin: stdin}, parse7zLine
}

// countArchiveEntries returns the number of members in an archive, or 0 if it
// cannot be determined cheaply (progress falls back to indeterminate).
func countArchiveEntries(ctx context.Context, arc string) int {
	f := detectFormat(arc)
	switch {
	case isTar(f):
		a := []string{"-t"}
		if c := tarComp(f); c != "" {
			a = append(a, c)
		}
		a = append(a, "-f", arc)
		return countLines(exec.CommandContext(ctx, "tar", a...))
	case f == fmtZip:
		// Entry names stay readable in an encrypted zip, so the bar is exact
		// even when the contents need a password.
		return countLines(exec.CommandContext(ctx, "zipinfo", "-1", argPath(arc)))
	default:
		return 0
	}
}

func countLines(cmd *exec.Cmd) int {
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	return bytes.Count(bytes.TrimRight(out, "\n"), []byte{'\n'}) + boolToInt(len(bytes.TrimSpace(out)) > 0)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Running the tools -----------------------------------------------------------

// toolRunner runs the external tools an archive job needs. Every pack and
// unpack goes through it rather than reaching for os/exec directly, which is
// the seam a test stands in for: it makes the orchestration reachable — which
// binary gets chosen, what goes on its stdin, whether a truncated archive is
// cleaned up, how a non-zero exit is classified — on a machine that has neither
// 7-Zip nor unrar installed.
//
// execTools is the only implementation outside tests, and reporter.tools is
// where a job picks one up.
type toolRunner interface {
	runTool(ctx context.Context, c toolCmd, r *reporter, parse func(string) string) error
	runPiped(ctx context.Context, src, filter toolCmd, out io.Writer, r *reporter, parse func(string) string) error
}

// execTools is the real thing: it starts child processes.
type execTools struct{}

// runStreamingIn runs a command in dir, optionally feeding it stdin (a password
// prompt, which is how 7z takes one without it landing in the process table).
func runStreamingIn(ctx context.Context, dir, stdin, bin string, args []string, r *reporter, parse func(string) string) error {
	return r.tools.runTool(ctx, toolCmd{bin: bin, args: args, dir: dir, stdin: stdin}, r, parse)
}

// runInto runs a command whose stdout is the archive itself, so only stderr
// carries progress.
func runInto(ctx context.Context, bin string, args []string, out io.Writer, r *reporter, parse func(string) string) error {
	return r.tools.runTool(ctx, toolCmd{bin: bin, args: args, stdout: out}, r, parse)
}

// toolCmd is one child process in a pack/unpack pipeline.
type toolCmd struct {
	bin    string
	args   []string
	dir    string
	stdin  string
	stdout io.Writer // nil merges stdout into the progress stream
}

// runTool runs one command, reporting progress from the lines parse accepts.
// Cancelling ctx kills the child, which is the only way to stop a tool that is
// half-way through a large archive.
func (execTools) runTool(ctx context.Context, c toolCmd, r *reporter, parse func(string) string) error {
	prog, err := newProgressPipe(r, parse)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, c.bin, c.args...)
	cmd.Dir = c.dir
	cmd.Stderr = prog.w
	cmd.Stdout = c.stdout
	if c.stdout == nil {
		cmd.Stdout = prog.w
	}
	if c.stdin != "" {
		cmd.Stdin = strings.NewReader(c.stdin)
	}

	if err := cmd.Start(); err != nil {
		prog.abort()
		return err
	}
	prog.startedChildren()

	err = cmd.Wait()
	return prog.finish(c.bin, err)
}

// runPiped runs `bin | filter > out`: the first command writes the archive
// stream to the second, which compresses it into out. Both report progress on
// stderr, and a failure in either is reported.
func (execTools) runPiped(ctx context.Context, src, filter toolCmd, out io.Writer, r *reporter, parse func(string) string) error {
	prog, err := newProgressPipe(r, parse)
	if err != nil {
		return err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		prog.abort()
		return err
	}

	srcCmd := exec.CommandContext(ctx, src.bin, src.args...)
	srcCmd.Stdout = pw
	srcCmd.Stderr = prog.w

	dst := exec.CommandContext(ctx, filter.bin, filter.args...)
	dst.Stdin = pr
	dst.Stdout = out
	dst.Stderr = prog.w

	if err := dst.Start(); err != nil {
		pr.Close()
		pw.Close()
		prog.abort()
		return err
	}
	if err := srcCmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		_ = dst.Wait()
		prog.abort()
		return err
	}
	// The parent holds no end of either pipe: the filter sees EOF when the
	// source exits, and the scanner when both have.
	pr.Close()
	pw.Close()
	prog.startedChildren()

	srcErr := srcCmd.Wait()
	dstErr := dst.Wait()
	if srcErr != nil {
		return prog.finish(src.bin, srcErr)
	}
	return prog.finish(filter.bin, dstErr)
}

// progressPipe fans a child's output into the reporter and keeps the last lines
// so a failure can say more than an exit status — which is how a missing
// password is recognised.
type progressPipe struct {
	w    *os.File
	r    *os.File
	tail chan []string
}

// tailLines is how much of a tool's output is kept to explain a failure.
const tailLines = 12

func newProgressPipe(r *reporter, parse func(string) string) (*progressPipe, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	p := &progressPipe{w: pw, r: pr, tail: make(chan []string, 1)}
	go p.scan(r, parse)
	return p, nil
}

// startedChildren hands the write end to the children and starts scanning.
// Using one pipe for stdout and stderr keeps their lines in order and applies
// natural backpressure: if the UI is slow to drain progress, the child blocks.
func (p *progressPipe) startedChildren() {
	p.w.Close() // the parent drops its end; the reader sees EOF at child exit
}

func (p *progressPipe) abort() {
	p.w.Close()
	p.r.Close()
}

// finish stops the scan and turns a child's exit error into something the app
// can act on.
func (p *progressPipe) finish(bin string, waitErr error) error {
	lines := <-p.tail
	p.r.Close()
	if waitErr == nil {
		return nil
	}
	if needsPassword(lines) {
		return ErrNeedPassword
	}
	if msg := lastMeaningful(lines); msg != "" {
		return fmt.Errorf("%s failed: %s", bin, msg)
	}
	return fmt.Errorf("%s failed: %w", bin, waitErr)
}

// scan drains the pipe until every writer is gone, stepping the progress bar
// and keeping the tail of the output for finish.
func (p *progressPipe) scan(r *reporter, parse func(string) string) {
	sc := bufio.NewScanner(p.r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	tail := make([]string, 0, tailLines)
	for sc.Scan() {
		line := sc.Text()
		if len(tail) == tailLines {
			tail = tail[1:]
		}
		tail = append(tail, line)
		if name := parse(line); name != "" {
			r.step(name)
		}
	}
	p.tail <- tail
}

// needsPassword recognises the many ways the archive tools say "encrypted".
func needsPassword(lines []string) bool {
	markers := []string{
		"unable to get password", // unzip, no password given
		"incorrect password",     // unzip / unrar
		"wrong password",         // 7z
		"headers error",          // 7z, encrypted file names
		"encrypted",              // 7z "Can not open encrypted archive"
		"password is incorrect",  // unrar
		"needs password",         // unrar
		"enter password",         // any tool that got as far as prompting
		"password incorrect",     // zip
	}
	for _, l := range lines {
		low := strings.ToLower(l)
		for _, m := range markers {
			if strings.Contains(low, m) {
				return true
			}
		}
	}
	return false
}

// lastMeaningful picks the most useful line of a failing tool's output: the
// last one that isn't a banner, a blank, or a progress line.
func lastMeaningful(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		switch {
		case l == "", strings.HasPrefix(l, "+ "), strings.HasPrefix(l, "- "):
			continue
		case strings.HasPrefix(l, "7-Zip"), strings.HasPrefix(l, "p7zip"):
			continue
		case strings.HasPrefix(l, "Files:"), strings.HasPrefix(l, "Size:"), strings.HasPrefix(l, "Compressed:"):
			continue
		}
		return truncErr(l)
	}
	return ""
}

// truncErr keeps a tool's complaint short enough for the status bar.
func truncErr(s string) string {
	const max = 120
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// Line parsers ----------------------------------------------------------------

// parseTarLine: `tar -v` prints one member path per line.
func parseTarLine(s string) string { return strings.TrimSpace(s) }

// parseUnzipLine: `unzip` prints "  inflating: path", "extracting: path", etc.
func parseUnzipLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ": "); i >= 0 {
		return strings.TrimSpace(s[i+2:])
	}
	return ""
}

// parseNone drives an indeterminate bar (tool output not worth parsing).
func parseNone(string) string { return "" }
