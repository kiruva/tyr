package fileops

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Packing is a choice of format, a compression level, and optionally a
// password. Every format is produced by a command-line tool, so a format is
// only offered when its tool is on PATH (see AvailablePackFormats).
//
// Encryption is 7-Zip's when 7z is installed — AES-256, and for .7z the file
// names are encrypted too. Info-ZIP's `zip` is the fallback for .zip, and it
// can only do legacy ZipCrypto, which is weak; the app says so before packing.

// PackFormat is an archive format tyr can create.
type PackFormat int

const (
	PackTarGz PackFormat = iota
	PackTarBz2
	PackTarXz
	PackTarZst
	PackTar
	PackZip
	Pack7z
)

// PackFormatInfo describes one creatable format.
type PackFormatInfo struct {
	Format PackFormat
	Label  string   // "tar.gz", as shown in the pack dialog
	Ext    string   // ".tar.gz", appended to the output name
	Tools  []string // binaries needed to create it

	MinLevel int // 0 where the format can store without compressing
	MaxLevel int
	DefLevel int

	// Encrypt is the tool that would encrypt this format, or "" if none can.
	// Resolved against PATH by AvailablePackFormats.
	Encrypt string
}

// Levels reports whether the format takes a compression level at all.
func (i PackFormatInfo) Levels() bool { return i.MaxLevel > i.MinLevel }

// packFormats lists the creatable formats in dialog order.
func packFormats() []PackFormatInfo {
	return []PackFormatInfo{
		{Format: PackTarGz, Label: "tar.gz", Ext: ".tar.gz", Tools: []string{"tar", "gzip"}, MinLevel: 1, MaxLevel: 9, DefLevel: 6},
		{Format: PackTarBz2, Label: "tar.bz2", Ext: ".tar.bz2", Tools: []string{"tar", "bzip2"}, MinLevel: 1, MaxLevel: 9, DefLevel: 9},
		{Format: PackTarXz, Label: "tar.xz", Ext: ".tar.xz", Tools: []string{"tar", "xz"}, MinLevel: 0, MaxLevel: 9, DefLevel: 6},
		{Format: PackTarZst, Label: "tar.zst", Ext: ".tar.zst", Tools: []string{"tar", "zstd"}, MinLevel: 1, MaxLevel: 19, DefLevel: 3},
		{Format: PackTar, Label: "tar", Ext: ".tar", Tools: []string{"tar"}},
		{Format: PackZip, Label: "zip", Ext: ".zip", Tools: []string{"zip"}, MaxLevel: 9, DefLevel: 6},
		{Format: Pack7z, Label: "7z", Ext: ".7z", Tools: []string{"7z"}, MaxLevel: 9, DefLevel: 5},
	}
}

// Info returns the description of a format.
func (f PackFormat) Info() PackFormatInfo {
	for _, i := range packFormats() {
		if i.Format == f {
			return i
		}
	}
	return PackFormatInfo{Format: f, Label: "?"}
}

// AvailablePackFormats returns the formats this machine can actually create,
// each with its Encrypt tool resolved: 7z where p7zip is installed, `zip` for
// a .zip without it, and "" where nothing here can encrypt the format.
func AvailablePackFormats() []PackFormatInfo {
	// installed, not onPath: the lookup handed in here must answer for one exact
	// command name, so resolveTool can report which variant it found.
	return availablePackFormats(installed)
}

func availablePackFormats(have func(string) bool) []PackFormatInfo {
	out := make([]PackFormatInfo, 0, len(packFormats()))
	sevenZip := resolveTool("7z", have) != ""
	for _, i := range packFormats() {
		// A .zip can be written by either tool, so it is offered as long as one
		// of them is here; every other format has exactly one writer.
		if i.Format == PackZip {
			w := resolveTool("zip", have)
			if w == "" {
				w = resolveTool("7z", have)
			}
			if w == "" {
				continue
			}
			i.Tools = []string{w}
		} else if !haveAllOf(have, i.Tools) {
			continue
		}
		// Report the command names that will actually run, so the dialog names
		// 7zz on a machine that has 7-Zip under that name.
		for j, t := range i.Tools {
			i.Tools[j] = resolveTool(t, have)
		}
		switch {
		case i.Format == Pack7z: // 7z is its own encryptor
			i.Encrypt = "7z"
		case i.Format == PackZip && sevenZip:
			i.Encrypt = "7z" // AES-256 rather than ZipCrypto
		case i.Format == PackZip:
			i.Encrypt = "zip"
		}
		out = append(out, i)

	}
	return out
}

func haveAllOf(have func(string) bool, bins []string) bool {
	for _, b := range bins {
		if resolveTool(b, have) == "" {
			return false
		}
	}
	return true
}

// PackOpts is how OpPack should build Job.Out.
type PackOpts struct {
	Format   PackFormat
	Level    int
	Password string // empty means no encryption

	// Encrypt is the tool to encrypt with, as resolved by AvailablePackFormats.
	// It matters for .zip, where 7z gives AES-256 and `zip` gives ZipCrypto.
	Encrypt string
}

// EncryptionLabel describes what the password will actually buy, which is not
// the same for both zip encryptors and is worth saying out loud.
func (o PackOpts) EncryptionLabel() string {
	switch {
	case o.Password == "":
		return ""
	case o.Encrypt == "7z" && o.Format == Pack7z:
		return "AES-256, names hidden"
	case o.Encrypt == "7z":
		return "AES-256"
	case o.Encrypt == "zip":
		return "ZipCrypto (weak)"
	default:
		return "not encrypted"
	}
}

// Weak reports whether the chosen encryption is the legacy zip cipher, which
// the pack dialog warns about rather than silently accepting.
func (o PackOpts) Weak() bool { return o.Password != "" && o.Encrypt == "zip" }

// pack creates job.Out from job.Srcs. All sources come from the same pane, so
// they share a parent directory.
func pack(job Job, r *reporter) error {
	if len(job.Srcs) == 0 {
		return fmt.Errorf("nothing to pack")
	}
	opts := job.Pack
	info := opts.Format.Info()
	if opts.Password != "" && opts.Encrypt == "" {
		return fmt.Errorf("%s cannot be password protected", info.Label)
	}

	// A failed pack leaves a truncated archive behind, which is worse than no
	// archive: drop it, unless we overwrote something that was already there.
	existed := false
	if _, err := os.Stat(job.Out); err == nil {
		existed = true
	}
	err := runPack(job, opts, r)
	if err != nil && !existed {
		_ = os.Remove(job.Out)
	}
	return err
}

func runPack(job Job, opts PackOpts, r *reporter) error {
	switch opts.Format {
	case Pack7z:
		return packWith7z(job, opts, r)
	case PackZip:
		// 7-Zip writes the zip when it is doing the encryption, or when
		// Info-ZIP's zip is not installed.
		if (opts.Password != "" && opts.Encrypt == "7z") || tool("zip") == "" {
			return packWith7z(job, opts, r)
		}
		return packWithZip(job, opts, r)
	default:
		return packTar(job, opts, r)
	}
}

// packTar streams `tar -cvf -` through a compressor into the output file. The
// pipe (rather than tar's own -z/-J) is what makes the compression level
// reachable, and tar's verbose output on stderr drives the progress bar.
func packTar(job Job, opts PackOpts, r *reporter) error {
	parent := filepath.Dir(job.Srcs[0])
	args := []string{"-c", "-v", "-f", "-", "-C", parent}
	for _, s := range job.Srcs {
		args = append(args, filepath.Base(s))
	}

	out, err := os.Create(job.Out)
	if err != nil {
		return err
	}
	defer out.Close()

	comp, compArgs := tarCompressor(opts.Format, opts.Level)
	if comp == "" {
		return runInto("tar", args, out, r, parseTarLine)
	}
	return runPiped("tar", args, comp, compArgs, out, r, parseTarLine)
}

// tarCompressor maps a tar format and level onto the filter that compresses the
// stream. Plain tar has none.
func tarCompressor(f PackFormat, level int) (bin string, args []string) {
	switch f {
	case PackTarGz:
		return "gzip", []string{"-" + strconv.Itoa(level), "-c"}
	case PackTarBz2:
		return "bzip2", []string{"-" + strconv.Itoa(level), "-c"}
	case PackTarXz:
		return "xz", []string{"-" + strconv.Itoa(level), "-c", "-T0"}
	case PackTarZst:
		return "zstd", []string{"-" + strconv.Itoa(level), "-c", "-q", "-T0"}
	default:
		return "", nil
	}
}

// packWithZip runs Info-ZIP. -P puts the password on the command line, which is
// the only way `zip` takes one without a terminal; packWith7z is preferred
// wherever 7z exists.
func packWithZip(job Job, opts PackOpts, r *reporter) error {
	args := []string{"-r", "-" + strconv.Itoa(opts.Level)}
	if opts.Password != "" {
		args = append(args, "-P", opts.Password)
	}
	args = append(args, job.Out)
	for _, s := range job.Srcs {
		args = append(args, filepath.Base(s))
	}
	return runStreamingIn(filepath.Dir(job.Srcs[0]), "", "zip", args, r, parseZipAddLine)
}

// packWith7z runs 7-Zip, which also writes .zip with AES-256. The password goes
// in on stdin (asked for twice) so it never appears in the process arguments;
// -mhe encrypts the file names as well, which only the .7z container supports.
func packWith7z(job Job, opts PackOpts, r *reporter) error {
	args := []string{"a", "-bb1", "-bd", "-y", "-mx=" + strconv.Itoa(opts.Level)}
	if opts.Format == PackZip {
		args = append(args, "-tzip")
		if opts.Password != "" {
			args = append(args, "-mem=AES256")
		}
	}
	stdin := ""
	if opts.Password != "" {
		args = append(args, "-p")
		if opts.Format == Pack7z {
			args = append(args, "-mhe=on")
		}
		stdin = opts.Password + "\n" + opts.Password + "\n"
	}
	args = append(args, job.Out)
	for _, s := range job.Srcs {
		args = append(args, filepath.Base(s))
	}
	bin := tool("7z")
	if bin == "" {
		return fmt.Errorf("7-Zip is not installed (7z, 7zz or 7za)")
	}
	return runStreamingIn(filepath.Dir(job.Srcs[0]), stdin, bin, args, r, parse7zLine)
}

// parseZipAddLine: `zip` prints "  adding: path (deflated 42%)".
func parseZipAddLine(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"adding: ", "updating: ", "deflating: "} {
		if i := strings.Index(s, p); i >= 0 {
			name := s[i+len(p):]
			if j := strings.LastIndex(name, " ("); j >= 0 {
				name = name[:j]
			}
			return strings.TrimSpace(name)
		}
	}
	return ""
}

// parse7zLine: with -bb1, 7z prints "+ path" while packing and "- path" while
// extracting. Everything else is a banner or a summary.
func parse7zLine(s string) string {
	s = strings.TrimRight(s, "\r")
	if len(s) > 2 && (s[0] == '+' || s[0] == '-') && s[1] == ' ' {
		return strings.TrimSpace(s[2:])
	}
	return ""
}
