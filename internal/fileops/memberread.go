package fileops

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Browsing an archive and opening a file inside it read the archive here, in
// Go, whenever the standard library knows the format: tar, gzip- and
// bzip2-compressed tar, and zip entries that are stored or deflated. The tools
// are the fallback, for xz and zstd and the rarer zip methods.
//
// The tools are not the default because of how they take a member's name: tar
// and unzip read it as a pattern. Opening a[1].txt handed back the bytes of
// a1.txt, and saving the editor wrote them into a[1].txt; a*.txt returned
// every match run together. Read here, a name is only ever itself.

// MaxMemberBytes is the most of one member ReadMember will hold. A small
// archive can carry a member that expands to gigabytes, and all of it would
// otherwise land in memory to be shown.
const MaxMemberBytes = 64 << 20

// ErrMemberTooLarge is a member bigger than MaxMemberBytes.
var ErrMemberTooLarge = fmt.Errorf("too large to open from inside the archive (over %d MB) — unpack it instead", MaxMemberBytes>>20)

// errNotNative is a member this file cannot read itself, for the caller to hand
// to a tool instead.
var errNotNative = errors.New("not readable natively")

// openTarStream opens a tar archive as a stream of entries, for the formats
// the standard library can decompress.
func openTarStream(archive string, f format) (*tar.Reader, io.Closer, error) {
	file, err := os.Open(archive)
	if err != nil {
		return nil, nil, err
	}
	var r io.Reader = file
	switch f {
	case fmtTar:
	case fmtTarGz:
		gz, err := gzip.NewReader(file)
		if err != nil {
			file.Close()
			return nil, nil, err
		}
		r = gz
	case fmtTarBz2:
		r = bzip2.NewReader(file)
	default:
		file.Close()
		return nil, nil, errNotNative
	}
	return tar.NewReader(r), file, nil
}

// memberName is how a stored name is shown and looked up: the path with the
// trailing slash a directory entry carries taken off. It is what the tools'
// listings printed, so browsing reads the same either way.
func memberName(stored string) (name string, isDir bool) {
	return strings.TrimSuffix(stored, "/"), strings.HasSuffix(stored, "/")
}

// listNative lists an archive's members with their sizes.
func listNative(archive string, f format) ([]Member, error) {
	if f == fmtZip {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return nil, err
		}
		defer func() { _ = z.Close() }()
		ms := make([]Member, 0, len(z.File))
		for _, zf := range z.File {
			name, dir := memberName(zf.Name)
			ms = append(ms, Member{Path: name, IsDir: dir || zf.FileInfo().IsDir(), Size: int64(zf.UncompressedSize64), ModTime: zf.Modified})
		}
		return ms, nil
	}

	tr, closer, err := openTarStream(archive, f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	var ms []Member
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return ms, nil
		}
		if err != nil {
			return nil, err
		}
		name, dir := memberName(hdr.Name)
		dir = dir || hdr.Typeflag == tar.TypeDir
		size := hdr.Size
		if dir {
			size = 0
		}
		ms = append(ms, Member{Path: name, IsDir: dir, Size: size, ModTime: hdr.ModTime})
	}
}

// readNative reads one member whose name is exactly member.
func readNative(archive string, f format, member string) ([]byte, error) {
	if f == fmtZip {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return nil, err
		}
		defer func() { _ = z.Close() }()
		for _, zf := range z.File {
			if name, _ := memberName(zf.Name); name != member {
				continue
			}
			if zf.Flags&0x1 != 0 {
				return nil, fmt.Errorf("%s is encrypted — unpack the archive to open it", member)
			}
			if zf.UncompressedSize64 > MaxMemberBytes {
				return nil, ErrMemberTooLarge
			}
			rc, err := zf.Open()
			if errors.Is(err, zip.ErrAlgorithm) {
				return nil, errNotNative // a method only the tools have
			}
			if err != nil {
				return nil, err
			}
			defer func() { _ = rc.Close() }()
			return readCapped(rc)
		}
		return nil, fmt.Errorf("%s is not in the archive", member)
	}

	tr, closer, err := openTarStream(archive, f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s is not in the archive", member)
		}
		if err != nil {
			return nil, err
		}
		if name, _ := memberName(hdr.Name); name != member {
			continue
		}
		if hdr.Size > MaxMemberBytes {
			return nil, ErrMemberTooLarge
		}
		return readCapped(tr)
	}
}

// readCapped reads r to the end, refusing anything past MaxMemberBytes — the
// size an entry declares is the archive's word, and the data is what counts.
func readCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxMemberBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxMemberBytes {
		return nil, ErrMemberTooLarge
	}
	return data, nil
}

// patternLike reports whether a member name would match more than itself when
// a tool reads it as a pattern. Only the tool fallback has to care.
func patternLike(member string) bool { return strings.ContainsAny(member, `*?[\`) }
