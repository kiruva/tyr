package fileops

import (
	"context"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/kiruva/tyr/internal/fsutil"
)

// Member is a single entry inside an archive.
type Member struct {
	Path    string // full slash-separated path within the archive (no trailing slash)
	IsDir   bool
	Size    int64
	ModTime time.Time // zero when the listing came from a tool
	NoSize  bool      // the listing came from a tool, which gives no size
}

// Browsable reports whether an archive's members can be listed/streamed.
// (tar family and zip; 7z/rar must be unpacked to disk instead.)
func Browsable(name string) bool {
	f := detectFormat(name)
	return isTar(f) || f == fmtZip
}

// ListMembers returns the entries inside an archive, read natively where the
// format allows (see memberread.go) and through the tools otherwise.
func ListMembers(archive string) ([]Member, error) {
	f := detectFormat(archive)
	if ms, err := listNative(archive, f); !errors.Is(err, errNotNative) {
		return ms, err
	}
	var out []byte
	var err error
	switch {
	case isTar(f):
		out, err = exec.Command("tar", tarArgs("-t", f, "-f", archive)...).Output()
	case f == fmtZip:
		out, err = exec.Command("zipinfo", "-1", argPath(archive)).Output()
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
			Path:   strings.TrimSuffix(p, "/"),
			IsDir:  strings.HasSuffix(p, "/"),
			NoSize: true,
		})
	}
	return ms
}

// ReadMember reads a single member's bytes into memory, up to MaxMemberBytes.
// It is read natively where the format allows (see memberread.go); otherwise a
// tool streams it. The member name comes out of the archive, so it is
// attacker-controlled and never goes on a command line where a tool could read
// it as an option — see argsafe.go — or as a pattern.
func ReadMember(archive, member string) ([]byte, error) {
	f := detectFormat(archive)
	if data, err := readNative(archive, f, member); !errors.Is(err, errNotNative) {
		return data, err
	}
	if patternLike(member) {
		return nil, fmt.Errorf("cannot open %q from inside this archive: the tool would read the name as a pattern — unpack it instead", member)
	}

	var cmd *exec.Cmd
	switch {
	case isTar(f):
		cmd = exec.Command("tar", tarArgs("-x", f, "-O", "-f", archive, "--", member)...)
	case f == fmtZip:
		// 7-Zip honours "--" and unzip does not, so an entry whose name starts
		// with a "-" is only readable where 7-Zip is installed. unzip stays the
		// default otherwise: it is the native tool for a zip on Unix.
		if bin := tool("7z"); bin != "" && optionLike(member) {
			cmd = exec.Command(bin, "x", "-so", "--", argPath(archive), member)
			break
		}
		if err := checkUnzipMember(member); err != nil {
			return nil, err
		}
		cmd = exec.Command("unzip", "-p", argPath(archive), member)
	default:
		return nil, fmt.Errorf("reading members of this archive type is not supported")
	}
	return outputCapped(cmd)
}

// outputCapped is cmd.Output with the cap ReadMember keeps to: the tool is
// stopped once it has written more than MaxMemberBytes.
func outputCapped(cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	data, readErr := readCapped(out)
	if errors.Is(readErr, ErrMemberTooLarge) {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, readErr
	}
	return data, waitErr
}

// WriteMember writes edited bytes back to a single member, preserving the
// archive's format. A zip gets a targeted update (zip itself writes a new
// archive and renames it into place); a tar of any kind is repacked.
func WriteMember(archive, member string, data []byte) error {
	switch f := detectFormat(archive); {
	case f == fmtZip:
		return writeZipMember(archive, member, data)
	case isTar(f):
		return repackTar(context.Background(), f, archive, func(dir string) error {
			_, err := writeMemberFile(dir, member, data)
			return err
		})
	default:
		return fmt.Errorf("editing members of this archive type is not supported")
	}
}

// writeMemberFile writes data to <dir>/<member>, creating parent dirs. The
// member name is whatever the archive says, so it is confined to dir first:
// "../../.bashrc" is a legal member name and filepath.Join would resolve the
// escape rather than stop it.
func writeMemberFile(dir, member string, data []byte) (string, error) {
	dst, err := containedPath(dir, member)
	if err != nil {
		return "", err
	}
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
	// -nw: the name is a name. zip reads a [ in it as a wildcard otherwise.
	cmd := exec.Command("zip", "-q", "-nw", argPath(archive), "--", member)
	cmd.Dir = tmp
	return runQuiet(cmd)
}

// repackTar changes a tar of any kind: it is extracted whole into a scratch
// folder, mutate changes that folder, and the result is packed into a new file
// beside the archive that replaces it only once it is complete. A tar cannot be
// edited in place portably — bsdtar has no --delete — and packing straight over
// the original destroyed it whenever the pack failed.
func repackTar(ctx context.Context, f format, archive string, mutate func(dir string) error) error {
	// A link is followed: the archive it points at is the one being changed.
	if real, err := filepath.EvalSymlinks(archive); err == nil {
		archive = real
	}
	info, err := os.Stat(archive)
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "tyr-repack-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := runQuiet(exec.CommandContext(ctx, "tar", tarArgs("-x", f, "-f", archive, "-C", tmp)...)); err != nil {
		return err
	}

	if err := mutate(tmp); err != nil {
		return err
	}

	names, err := topLevel(tmp)
	if err != nil {
		return err
	}
	out, err := fsutil.TempBeside(archive)
	if err != nil {
		return err
	}
	create := append(tarArgs("-c", f, "-f", out, "-C", tmp, "--"), names...)
	if err := runQuiet(exec.CommandContext(ctx, "tar", create...)); err != nil {
		_ = os.Remove(out)
		return err
	}
	return fsutil.Commit(out, archive, info.Mode().Perm())
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
func addToArchive(ctx context.Context, job Job, r *reporter, rv *resolver) error {
	var err error
	switch f := detectFormat(job.Dest); {
	case isTar(f):
		err = addToTar(ctx, f, job.Dest, job.VDir, job.Srcs, r, rv)
	case f == fmtZip:
		err = addToZip(ctx, job.Dest, job.VDir, job.Srcs, r, rv)
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
// and returns the slash-separated relative paths that were staged. A collision
// in root is put to rv: for a tar, root holds the archive's own contents, so a
// name taken there is a name taken in the archive.
func stageSources(ctx context.Context, root, vdir string, srcs []string, r *reporter, rv *resolver) ([]string, error) {
	destDir := filepath.Join(root, filepath.FromSlash(vdir))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	rn := &runner{ctx: ctx, r: r, rv: rv}

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

// addToTar repacks the tar with the new files spliced in at vdir. Repacking
// (rather than -r append) keeps one code path for both plain and compressed tar.
func addToTar(ctx context.Context, f format, archive, vdir string, srcs []string, r *reporter, rv *resolver) error {
	return repackTar(ctx, f, archive, func(dir string) error {
		_, err := stageSources(ctx, dir, vdir, srcs, r, rv)
		return err
	})
}

// addToZip stages the sources and lets `zip` splice them in (updates existing
// members in place, so no full rewrite is needed).
//
// zip replaces a member of the same name without a word, so before it runs,
// every staged file that is already in the archive is asked about — against
// the member's own size and date, as a copy would ask. A skip takes the staged
// file out, a keep-both renames it; nothing of the archive's own is staged, so
// nothing of it is written back.
func addToZip(ctx context.Context, archive, vdir string, srcs []string, r *reporter, rv *resolver) error {
	tmp, err := os.MkdirTemp("", "tyr-addzip-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	// The scratch folder is empty: nothing in it can collide.
	if _, err := stageSources(ctx, tmp, vdir, srcs, r, newResolver(ctx, nil, ConflictOverwrite)); err != nil {
		return err
	}
	members, err := ListMembers(archive)
	if err != nil {
		return err
	}
	if err := resolveAgainstMembers(ctx, archive, tmp, members, srcs, vdir, rv); err != nil {
		return err
	}

	names, err := topLevel(filepath.Join(tmp, filepath.FromSlash(vdir)))
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil // every one was skipped
	}
	rels := make([]string, 0, len(names))
	for _, n := range names {
		rels = append(rels, path.Join(vdir, n))
	}
	cmd := exec.CommandContext(ctx, "zip", append([]string{"-r", "-q", argPath(archive), "--"}, rels...)...)
	cmd.Dir = tmp
	return runQuiet(cmd)
}

// resolveAgainstMembers walks the staged files under root and puts each one the
// archive already holds to rv. srcs are the originals, so the question shows
// the incoming file as it is on disk.
func resolveAgainstMembers(ctx context.Context, archive, root string, members []Member, srcs []string, vdir string, rv *resolver) error {
	taken := make(map[string]Member, len(members))
	for _, m := range members {
		taken[m.Path] = m
	}
	origin := make(map[string]string, len(srcs)) // staged top-level name → original
	for _, s := range srcs {
		origin[filepath.Base(s)] = s
	}

	base := filepath.Join(root, filepath.FromSlash(vdir))
	return filepath.WalkDir(base, func(p string, d iofs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, _ := filepath.Rel(base, p)
		name := path.Join(vdir, filepath.ToSlash(rel))
		m, ok := taken[name]
		if !ok || m.IsDir {
			return nil
		}
		top, rest, _ := strings.Cut(filepath.ToSlash(rel), "/")
		src := filepath.Join(origin[top], filepath.FromSlash(rest))

		action, err := rv.resolveWith(src, filepath.Join(archive, filepath.FromSlash(name)), Meta{Size: m.Size, ModTime: m.ModTime})
		if err != nil {
			return err
		}
		switch action {
		case ConflictSkip:
			return os.Remove(p)
		case ConflictKeepBoth:
			return os.Rename(p, filepath.Join(filepath.Dir(p), path.Base(freeMemberName(name, taken))))
		}
		return nil // overwrite: zip replaces it
	})
}

// freeMemberName finds a name beside member that the archive does not hold,
// the way freeName does on disk: notes.txt becomes "notes (2).txt".
func freeMemberName(member string, taken map[string]Member) string {
	dir, base := path.Split(member)
	ext := path.Ext(base)
	if strings.HasPrefix(base, ".") && ext == base {
		ext = ""
	}
	stem := strings.TrimSuffix(base, ext)
	for n := 2; ; n++ {
		candidate := dir + fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if _, ok := taken[candidate]; !ok {
			return candidate
		}
	}
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
