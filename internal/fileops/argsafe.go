package fileops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Everything in this package drives an external archive tool, and two kinds of
// string reach those command lines from somewhere the user does not control:
// a path on disk, and a member name read out of an archive. Both can begin with
// a "-", and tar, zip, 7-Zip and unrar all parse options anywhere on the line,
// not just before the first operand. A member called
// "--checkpoint-action=exec=..." in a downloaded tarball is arbitrary code
// execution under GNU tar; a member called "-x" quietly turns `unzip -p` into
// "dump every file" instead of "dump this one".
//
// The rules below are what keeps that from happening. internal/remote solves
// the same problem for the shell side with shQuote plus a "--" on every command.
//
// Which tools honour "--" is not a matter of taste, it was measured:
//
//	tar    -- works (GNU and bsdtar)
//	zip    -- works
//	7z     -- works
//	unzip  -- NOT honoured; it is taken as a literal filename pattern
//
// So member names go after a "--" for tar, zip and 7-Zip, and unzip — which has
// no opt-out at all — is only ever handed a member name that cannot be read as
// an option. Paths are made absolute instead, which no tool can mistake for a
// switch and every tool accepts.

// argPath makes a path safe to pass as a positional argument. Only a relative
// path can start with a "-", so making it absolute is enough, and it leaves the
// overwhelmingly common case untouched.
func argPath(p string) string {
	if !strings.HasPrefix(p, "-") {
		return p
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	// Abs only fails when the working directory is unreadable. "./" in front is
	// the weaker fallback: still not an option, still the same file.
	return "." + string(os.PathSeparator) + p
}

// optionLike reports whether a member name would be read as a switch. A member
// name is a path *inside* an archive, so it cannot be made absolute the way
// argPath does it — "/foo" and "foo" are different members.
func optionLike(member string) bool { return strings.HasPrefix(member, "-") }

// checkUnzipMember rejects a member name that Info-ZIP's unzip would swallow as
// an option. Every other tool takes "--", so this is the one place that has to
// refuse the work rather than quote its way out of it; the caller prefers 7-Zip
// when it is installed and only falls back here when it is not.
func checkUnzipMember(member string) error {
	if optionLike(member) {
		return fmt.Errorf("cannot read %q with unzip: the name would be read as an option; install 7-Zip to open this entry", member)
	}
	return nil
}

// containedPath joins a member name onto a directory and confirms the result is
// still inside it. Archive members are attacker-controlled and "../../.bashrc"
// is a legal name; filepath.Join cleans the "..", which resolves the escape
// rather than preventing it.
func containedPath(dir, member string) (string, error) {
	dst := filepath.Join(dir, filepath.FromSlash(member))
	rel, err := filepath.Rel(dir, dst)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive member path: %s", member)
	}
	return dst, nil
}
