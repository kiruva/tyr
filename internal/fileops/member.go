package fileops

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// Member is a single entry inside an archive.
type Member struct {
	Path  string // full slash-separated path within the archive (no trailing slash)
	IsDir bool
	Size  int64
}

// Browsable reports whether an archive's members can be listed/streamed.
// (tar family and zip; 7z/rar must be unpacked to disk instead.)
func Browsable(name string) bool {
	f := detectFormat(name)
	return isTar(f) || f == fmtZip
}

// ListMembers returns the entries inside an archive.
func ListMembers(archive string) ([]Member, error) {
	f := detectFormat(archive)
	var out []byte
	var err error
	switch {
	case isTar(f):
		a := []string{"-t"}
		if c := tarComp(f); c != "" {
			a = append(a, c)
		}
		a = append(a, "-f", archive)
		out, err = exec.Command("tar", a...).Output()
	case f == fmtZip:
		out, err = exec.Command("zipinfo", "-1", archive).Output()
	default:
		return nil, fmt.Errorf("browsing this archive type is not supported (unpack it instead)")
	}
	if err != nil {
		return nil, err
	}
	return parseMembers(out), nil
}

func parseMembers(out []byte) []Member {
	var ms []Member
	for _, line := range strings.Split(string(out), "\n") {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		ms = append(ms, Member{
			Path:  strings.TrimSuffix(p, "/"),
			IsDir: strings.HasSuffix(p, "/"),
		})
	}
	return ms
}

// ReadMember streams a single member's bytes into memory.
func ReadMember(archive, member string) ([]byte, error) {
	f := detectFormat(archive)
	switch {
	case isTar(f):
		a := []string{"-x"}
		if c := tarComp(f); c != "" {
			a = append(a, c)
		}
		a = append(a, "-O", "-f", archive, member)
		return exec.Command("tar", a...).Output()
	case f == fmtZip:
		return exec.Command("unzip", "-p", archive, member).Output()
	default:
		return nil, fmt.Errorf("reading members of this archive type is not supported")
	}
}

// WriteMember writes edited bytes back to a single member, preserving the
// archive's format. tar/zip get targeted updates; compressed tar is rewritten.
func WriteMember(archive, member string, data []byte) error {
	switch f := detectFormat(archive); {
	case f == fmtZip:
		return writeZipMember(archive, member, data)
	case f == fmtTar:
		return writeUncompressedTarMember(archive, member, data)
	case isTar(f):
		return rewriteCompressedTarMember(f, archive, member, data)
	default:
		return fmt.Errorf("editing members of this archive type is not supported")
	}
}

// writeMemberFile writes data to <dir>/<member>, creating parent dirs.
func writeMemberFile(dir, member string, data []byte) (string, error) {
	dst := filepath.Join(dir, filepath.FromSlash(member))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

func writeZipMember(archive, member string, data []byte) error {
	tmp, err := os.MkdirTemp("", "tyr-zip-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := writeMemberFile(tmp, member, data); err != nil {
		return err
	}
	cmd := exec.Command("zip", "-q", archive, member)
	cmd.Dir = tmp
	return runQuiet(cmd)
}

func writeUncompressedTarMember(archive, member string, data []byte) error {
	if err := runQuiet(exec.Command("tar", "--delete", "-f", archive, member)); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "tyr-tar-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := writeMemberFile(tmp, member, data); err != nil {
		return err
	}
	return runQuiet(exec.Command("tar", "-r", "-f", archive, "-C", tmp, member))
}

// rewriteCompressedTarMember extracts the whole archive, replaces one member,
// and repacks — the only way to update a member of a compressed tar via the CLI.
func rewriteCompressedTarMember(f format, archive, member string, data []byte) error {
	tmp, err := os.MkdirTemp("", "tyr-rewrite-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	// extract everything
	ex := []string{"-x"}
	if c := tarComp(f); c != "" {
		ex = append(ex, c)
	}
	ex = append(ex, "-f", archive, "-C", tmp)
	if err := runQuiet(exec.Command("tar", ex...)); err != nil {
		return err
	}

	// replace the edited member
	if _, err := writeMemberFile(tmp, member, data); err != nil {
		return err
	}

	// repack all top-level entries back to the original path
	names, err := topLevel(tmp)
	if err != nil {
		return err
	}
	create := []string{"-c"}
	if c := tarComp(f); c != "" {
		create = append(create, c)
	}
	create = append(create, "-f", archive, "-C", tmp)
	create = append(create, names...)
	return runQuiet(exec.Command("tar", create...))
}

func topLevel(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// addToArchive adds real files/dirs (job.Srcs) into archive job.Dest at the
// virtual directory job.VDir, preserving the archive's format. If job.Move is
// set, the sources are removed afterward.
func addToArchive(job Job, r *reporter) error {
	var err error
	switch f := detectFormat(job.Dest); {
	case isTar(f):
		err = addToTar(f, job.Dest, job.VDir, job.Srcs, r)
	case f == fmtZip:
		err = addToZip(job.Dest, job.VDir, job.Srcs, r)
	default:
		return fmt.Errorf("adding to this archive type is not supported")
	}
	if err != nil {
		return err
	}
	if job.Move {
		for _, s := range job.Srcs {
			if err := os.RemoveAll(s); err != nil {
				return err
			}
		}
	}
	return nil
}

// stageSources copies each src into <root>/<vdir>/<basename>, reporting progress,
// and returns the slash-separated relative paths that were staged.
func stageSources(root, vdir string, srcs []string, r *reporter) ([]string, error) {
	destDir := filepath.Join(root, filepath.FromSlash(vdir))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	// Staging is tyr's own scratch directory: nothing there can collide with
	// anything, so the copy answers its own (non-existent) conflicts.
	rn := &runner{r: r, rv: newResolver(nil, ConflictOverwrite)}

	rels := make([]string, 0, len(srcs))
	for _, s := range srcs {
		base := filepath.Base(s)
		if _, err := rn.copyPath(s, filepath.Join(destDir, base)); err != nil {
			return nil, err
		}
		rels = append(rels, path.Join(vdir, base))
	}
	return rels, nil
}

// addToTar rewrites the tar with the new files spliced in at vdir. Rewriting
// (rather than -r append) keeps one code path for both plain and compressed tar.
func addToTar(f format, archive, vdir string, srcs []string, r *reporter) error {
	tmp, err := os.MkdirTemp("", "tyr-add-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	ex := []string{"-x"}
	if c := tarComp(f); c != "" {
		ex = append(ex, c)
	}
	ex = append(ex, "-f", archive, "-C", tmp)
	if err := runQuiet(exec.Command("tar", ex...)); err != nil {
		return err
	}

	if _, err := stageSources(tmp, vdir, srcs, r); err != nil {
		return err
	}

	names, err := topLevel(tmp)
	if err != nil {
		return err
	}
	create := []string{"-c"}
	if c := tarComp(f); c != "" {
		create = append(create, c)
	}
	create = append(create, "-f", archive, "-C", tmp)
	create = append(create, names...)
	return runQuiet(exec.Command("tar", create...))
}

// addToZip stages the sources and lets `zip` splice them in (updates existing
// members in place, so no full rewrite is needed).
func addToZip(archive, vdir string, srcs []string, r *reporter) error {
	tmp, err := os.MkdirTemp("", "tyr-addzip-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	rels, err := stageSources(tmp, vdir, srcs, r)
	if err != nil {
		return err
	}
	cmd := exec.Command("zip", append([]string{"-r", "-q", archive}, rels...)...)
	cmd.Dir = tmp
	return runQuiet(cmd)
}

func runQuiet(cmd *exec.Cmd) error {
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s: %s", cmd.Args[0], msg)
		}
		return fmt.Errorf("%s: %w", cmd.Args[0], err)
	}
	return nil
}
