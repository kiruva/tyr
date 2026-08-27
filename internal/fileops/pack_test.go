package fileops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// runJob drains a job to its Result, so a test reads like the app does.
func runJob(t *testing.T, job Job) Result {
	t.Helper()
	var last Result
	for msg := range Run(context.Background(), job) {
		if res, ok := msg.(Result); ok {
			last = res
		}
	}
	return last
}

// packTree writes a small tree to pack and returns its parent and entry names.
func packTree(t *testing.T) (dir string, srcs []string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "d", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"a.txt":       "hello a",
		"d/b.txt":     "hello b",
		"d/sub/c.txt": "hello c",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "d")}
}

func needTools(t *testing.T, bins ...string) {
	t.Helper()
	for _, b := range bins {
		if !onPath(b) {
			t.Skipf("%s is not installed", b)
		}
	}
}

// extracted reads a file out of an extraction directory.
func extracted(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("extracted %s: %v", rel, err)
	}
	return string(b)
}

// Every format round-trips at its own compression level, and the level reaches
// the tool rather than being quietly dropped.
func TestPackRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format PackFormat
		level  int
	}{
		{"tar.gz fast", PackTarGz, 1},
		{"tar.gz max", PackTarGz, 9},
		{"tar.bz2", PackTarBz2, 9},
		{"tar.xz", PackTarXz, 1},
		{"tar.zst", PackTarZst, 3},
		{"tar", PackTar, 0},
		{"zip stored", PackZip, 0},
		{"zip max", PackZip, 9},
		{"7z", Pack7z, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := tc.format.Info()
			needTools(t, info.Tools...)
			if info.Format == PackZip {
				needTools(t, "unzip")
			}

			_, srcs := packTree(t)
			out := filepath.Join(t.TempDir(), "out"+info.Ext)
			res := runJob(t, Job{
				Op:   OpPack,
				Srcs: srcs,
				Out:  out,
				Pack: PackOpts{Format: tc.format, Level: tc.level},
			})
			if res.Err != nil {
				t.Fatalf("pack: %v", res.Err)
			}
			if st, err := os.Stat(out); err != nil || st.Size() == 0 {
				t.Fatalf("archive not written: %v", err)
			}

			dest := t.TempDir()
			res = runJob(t, Job{Op: OpUnpack, Srcs: []string{out}, Dest: dest})
			if res.Err != nil {
				t.Fatalf("unpack: %v", res.Err)
			}
			if got := extracted(t, dest, "a.txt"); got != "hello a" {
				t.Errorf("a.txt = %q", got)
			}
			if got := extracted(t, dest, "d/sub/c.txt"); got != "hello c" {
				t.Errorf("d/sub/c.txt = %q", got)
			}
		})
	}
}

// A higher level has to actually compress harder, or the setting is a lie.
func TestPackLevelChangesSize(t *testing.T) {
	needTools(t, "tar", "gzip")

	dir := t.TempDir()
	// Semi-compressible content: repeated text with varying tails.
	var body []byte
	for i := 0; i < 4000; i++ {
		body = append(body, []byte("the quick brown fox jumps over the lazy dog ")...)
		body = append(body, byte('a'+i%26))
	}
	src := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}

	size := func(level int) int64 {
		out := filepath.Join(t.TempDir(), "out.tar.gz")
		res := runJob(t, Job{
			Op:   OpPack,
			Srcs: []string{src},
			Out:  out,
			Pack: PackOpts{Format: PackTarGz, Level: level},
		})
		if res.Err != nil {
			t.Fatalf("pack level %d: %v", level, res.Err)
		}
		st, err := os.Stat(out)
		if err != nil {
			t.Fatal(err)
		}
		return st.Size()
	}

	fast, best := size(1), size(9)
	if best >= fast {
		t.Errorf("level 9 (%d bytes) should beat level 1 (%d bytes)", best, fast)
	}
}

// An encrypted archive round-trips with the password, and refuses without it.
func TestPackEncrypted(t *testing.T) {
	for _, tc := range []struct {
		name    string
		format  PackFormat
		encrypt string
		tools   []string
	}{
		{"7z", Pack7z, "7z", []string{"7z"}},
		{"zip via 7z (AES)", PackZip, "7z", []string{"7z"}},
		{"zip via zip (ZipCrypto)", PackZip, "zip", []string{"zip", "unzip"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			needTools(t, tc.tools...)

			_, srcs := packTree(t)
			out := filepath.Join(t.TempDir(), "secret"+tc.format.Info().Ext)
			opts := PackOpts{Format: tc.format, Level: 1, Password: "s3cr3t", Encrypt: tc.encrypt}
			if res := runJob(t, Job{Op: OpPack, Srcs: srcs, Out: out, Pack: opts}); res.Err != nil {
				t.Fatalf("pack: %v", res.Err)
			}

			// No password: the tool must fail, and fail recognisably.
			res := runJob(t, Job{Op: OpUnpack, Srcs: []string{out}, Dest: t.TempDir()})
			if !errors.Is(res.Err, ErrNeedPassword) {
				t.Fatalf("unpack without password: err = %v, want ErrNeedPassword", res.Err)
			}

			// Wrong password: same.
			res = runJob(t, Job{Op: OpUnpack, Srcs: []string{out}, Dest: t.TempDir(), Password: "nope"})
			if !errors.Is(res.Err, ErrNeedPassword) {
				t.Fatalf("unpack with wrong password: err = %v, want ErrNeedPassword", res.Err)
			}

			// Right password: everything comes back.
			dest := t.TempDir()
			res = runJob(t, Job{Op: OpUnpack, Srcs: []string{out}, Dest: dest, Password: "s3cr3t"})
			if res.Err != nil {
				t.Fatalf("unpack with password: %v", res.Err)
			}
			if got := extracted(t, dest, "d/b.txt"); got != "hello b" {
				t.Errorf("d/b.txt = %q", got)
			}
		})
	}
}

// A password on a format that cannot carry one is refused up front rather than
// producing an archive that looks protected and isn't.
func TestPackPasswordOnPlainFormat(t *testing.T) {
	needTools(t, "tar", "gzip")

	_, srcs := packTree(t)
	out := filepath.Join(t.TempDir(), "out.tar.gz")
	res := runJob(t, Job{
		Op:   OpPack,
		Srcs: srcs,
		Out:  out,
		Pack: PackOpts{Format: PackTarGz, Level: 6, Password: "s3cr3t"},
	})
	if res.Err == nil {
		t.Fatal("packing tar.gz with a password should fail")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a refused pack should not leave an archive behind")
	}
}

// A pack that fails part-way must not leave a truncated archive that looks real.
func TestPackFailureRemovesPartialArchive(t *testing.T) {
	needTools(t, "tar", "gzip")

	dir := t.TempDir()
	missing := filepath.Join(dir, "not-there.txt")
	out := filepath.Join(t.TempDir(), "out.tar.gz")

	res := runJob(t, Job{
		Op:   OpPack,
		Srcs: []string{missing},
		Out:  out,
		Pack: PackOpts{Format: PackTarGz, Level: 6},
	})
	if res.Err == nil {
		t.Fatal("packing a missing source should fail")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("failed pack left an archive behind")
	}
}

func TestAvailablePackFormatsResolvesEncryptor(t *testing.T) {
	all := func(string) bool { return true }
	// 7-Zip answers to three names; a machine without it has none of them.
	noSevenZip := func(bin string) bool {
		switch bin {
		case "7z", "7zz", "7za":
			return false
		}
		return true
	}

	byFormat := func(have func(string) bool) map[PackFormat]PackFormatInfo {
		m := map[PackFormat]PackFormatInfo{}
		for _, i := range availablePackFormats(have) {
			m[i.Format] = i
		}
		return m
	}

	with := byFormat(all)
	if got := with[PackZip].Encrypt; got != "7z" {
		t.Errorf("zip encryptor with 7z installed = %q, want 7z (AES-256)", got)
	}
	if got := with[Pack7z].Encrypt; got != "7z" {
		t.Errorf("7z encryptor = %q, want 7z", got)
	}
	if got := with[PackTarGz].Encrypt; got != "" {
		t.Errorf("tar.gz encryptor = %q, want none", got)
	}

	without := byFormat(noSevenZip)
	if got := without[PackZip].Encrypt; got != "zip" {
		t.Errorf("zip encryptor without 7z = %q, want zip (ZipCrypto)", got)
	}
	if _, ok := without[Pack7z]; ok {
		t.Error(".7z should not be offered when 7z is missing")
	}

	// Only the tools decide what is offered, so a machine with nothing offers
	// nothing at all.
	if got := availablePackFormats(func(string) bool { return false }); len(got) != 0 {
		t.Errorf("formats without any tool = %d, want 0", len(got))
	}
}

func TestPackOptsEncryptionLabel(t *testing.T) {
	tests := []struct {
		opts PackOpts
		want string
	}{
		{PackOpts{}, ""},
		{PackOpts{Format: Pack7z, Password: "x", Encrypt: "7z"}, "AES-256, names hidden"},
		{PackOpts{Format: PackZip, Password: "x", Encrypt: "7z"}, "AES-256"},
		{PackOpts{Format: PackZip, Password: "x", Encrypt: "zip"}, "ZipCrypto (weak)"},
	}
	for _, tt := range tests {
		if got := tt.opts.EncryptionLabel(); got != tt.want {
			t.Errorf("label for %+v = %q, want %q", tt.opts, got, tt.want)
		}
	}
	if !(PackOpts{Password: "x", Encrypt: "zip"}).Weak() {
		t.Error("ZipCrypto should be reported as weak")
	}
	if (PackOpts{Password: "x", Encrypt: "7z"}).Weak() {
		t.Error("AES-256 should not be reported as weak")
	}
}

// On a machine where 7-Zip is installed as 7zz, both .7z and .zip are still
// offered, and the format reports the command that will actually run — the
// lookup handed to availablePackFormats answers for exact names only.
func TestAvailablePackFormatsNamesTheInstalledVariant(t *testing.T) {
	only7zz := func(bin string) bool { return bin == "7zz" }

	got := map[PackFormat]PackFormatInfo{}
	for _, i := range availablePackFormats(only7zz) {
		got[i.Format] = i
	}
	if len(got) != 2 {
		t.Fatalf("formats offered = %d, want .7z and .zip", len(got))
	}
	for _, f := range []PackFormat{Pack7z, PackZip} {
		i, ok := got[f]
		if !ok {
			t.Fatalf("%v is not offered with 7zz installed", f)
		}
		if len(i.Tools) != 1 || i.Tools[0] != "7zz" {
			t.Errorf("%s tools = %v, want [7zz]", i.Label, i.Tools)
		}
		if i.Encrypt != "7z" {
			t.Errorf("%s encryptor = %q, want 7z", i.Label, i.Encrypt)
		}
	}
}

// Info-ZIP alone still writes a .zip, and it is the writer tyr names.
func TestAvailablePackFormatsZipWithoutSevenZip(t *testing.T) {
	onlyZip := func(bin string) bool { return bin == "zip" }

	formats := availablePackFormats(onlyZip)
	if len(formats) != 1 || formats[0].Format != PackZip {
		t.Fatalf("formats = %+v, want only .zip", formats)
	}
	if got := formats[0].Tools; len(got) != 1 || got[0] != "zip" {
		t.Errorf("tools = %v, want [zip]", got)
	}
	if got := formats[0].Encrypt; got != "zip" {
		t.Errorf("encryptor = %q, want zip (ZipCrypto)", got)
	}
}
