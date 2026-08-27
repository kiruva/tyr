package fileops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestArgPathLeavesOrdinaryPathsAlone(t *testing.T) {
	for _, p := range []string{"/tmp/a.tar", "a.tar", "./-x.tar", "sub/-x.tar"} {
		if got := argPath(p); got != p {
			t.Errorf("argPath(%q) = %q, want it unchanged", p, got)
		}
	}
}

func TestArgPathMakesOptionLikePathsAbsolute(t *testing.T) {
	got := argPath("-x.tar")
	if strings.HasPrefix(got, "-") {
		t.Fatalf("argPath(%q) = %q, still reads as an option", "-x.tar", got)
	}
	if !filepath.IsAbs(got) && !strings.HasPrefix(got, "."+string(os.PathSeparator)) {
		t.Fatalf("argPath = %q, want an absolute path or a ./ prefix", got)
	}
	if filepath.Base(got) != "-x.tar" {
		t.Fatalf("argPath = %q, want it to still name -x.tar", got)
	}
}

func TestContainedPathRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	for _, member := range []string{
		"../escape",
		"../../etc/passwd",
		"sub/../../escape",
		"..",
	} {
		if got, err := containedPath(dir, member); err == nil {
			t.Errorf("containedPath(%q) = %q, want an error", member, got)
		}
	}
}

func TestContainedPathAllowsMembersInside(t *testing.T) {
	dir := t.TempDir()
	for _, member := range []string{"a.txt", "sub/a.txt", "sub/../a.txt", "./a.txt"} {
		got, err := containedPath(dir, member)
		if err != nil {
			t.Errorf("containedPath(%q) = %v, want it allowed", member, err)
			continue
		}
		if rel, err := filepath.Rel(dir, got); err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("containedPath(%q) = %q, which is outside %q", member, got, dir)
		}
	}
}

func TestCheckUnzipMemberRejectsOptionLikeNames(t *testing.T) {
	if err := checkUnzipMember("-x"); err == nil {
		t.Fatal("checkUnzipMember(\"-x\") = nil, want an error: unzip would read it as an option")
	}
	if err := checkUnzipMember("real.txt"); err != nil {
		t.Fatalf("checkUnzipMember(\"real.txt\") = %v, want nil", err)
	}
}

// Extraction must never put a password where `ps` can read it. The pack side has
// always fed 7-Zip on stdin; this pins the extract side to the same rule.
func TestSevenZipExtractKeepsPasswordOffTheCommandLine(t *testing.T) {
	if tool("7z") == "" {
		t.Skip("7-Zip is not installed")
	}
	const password = "hunter2"

	cmd, _ := sevenZipExtract("/tmp/a.7z", "/tmp/out", password)
	for _, a := range cmd.args {
		if strings.Contains(a, password) {
			t.Fatalf("password appears in argv: %q", a)
		}
	}
	if cmd.stdin != password+"\n" {
		t.Fatalf("stdin = %q, want the password followed by a newline", cmd.stdin)
	}
}

func TestSevenZipExtractSendsNoStdinWithoutAPassword(t *testing.T) {
	if tool("7z") == "" {
		t.Skip("7-Zip is not installed")
	}
	cmd, _ := sevenZipExtract("/tmp/a.7z", "/tmp/out", "")
	if cmd.stdin != "" {
		t.Fatalf("stdin = %q, want empty so the child reads /dev/null and fails fast", cmd.stdin)
	}
}

// Every tool that takes a member name must get a "--" ahead of it, so a member
// called "--checkpoint-action=exec=..." is a filename and not a switch.
func TestExtractCommandSeparatesOptionsFromTheArchive(t *testing.T) {
	if tool("7z") == "" {
		t.Skip("7-Zip is not installed")
	}
	cmd, _ := extractCommand(fmt7z, "/tmp/a.7z", "/tmp/out", "")
	sep := -1
	for i, a := range cmd.args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		t.Fatalf("args = %v, want a -- separator", cmd.args)
	}
	if sep != len(cmd.args)-2 || cmd.args[len(cmd.args)-1] != "/tmp/a.7z" {
		t.Fatalf("args = %v, want the archive as the only operand after --", cmd.args)
	}
}

// The regression this whole file exists for: a tar member whose name is a GNU
// tar option must be read as a filename. Under the old command line GNU tar
// executed --checkpoint-action=exec=... instead.
func TestReadMemberTreatsOptionLikeNamesAsFilenames(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar is not installed")
	}
	dir := t.TempDir()
	// The payload names the canary relatively, so it has no "/" in it and stays
	// a legal filename. Running from dir is what makes that resolve, and is also
	// where a successful exploit would drop the canary.
	t.Chdir(dir)

	// A member whose name is a tar option, and a canary the exploit would touch.
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	const canary = "canary"
	const member = "--checkpoint-action=exec=touch " + canary
	if err := os.WriteFile(filepath.Join(src, member), []byte("payload\n"), 0o644); err != nil {
		t.Skipf("this filesystem will not hold the member name: %v", err)
	}

	archive := filepath.Join(dir, "evil.tar")
	if out, err := exec.Command("tar", "-cf", archive, "-C", src, "--", member).CombinedOutput(); err != nil {
		t.Skipf("could not build the fixture: %v: %s", err, out)
	}

	members, err := ListMembers(archive)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	var found string
	for _, m := range members {
		if strings.HasPrefix(m.Path, "--checkpoint-action=") {
			found = m.Path
		}
	}
	if found == "" {
		t.Fatalf("member not listed, got %v", members)
	}

	data, err := ReadMember(archive, found)
	if err != nil {
		t.Fatalf("ReadMember: %v", err)
	}
	if string(data) != "payload\n" {
		t.Errorf("ReadMember = %q, want the member's own bytes", data)
	}
	if _, err := os.Stat(filepath.Join(dir, canary)); err == nil {
		t.Fatal("the member name was executed as a tar option: --checkpoint-action ran")
	}
}

// The zip half of the same problem: `unzip -p archive -x` reads -x as the
// exclude switch and dumps every entry instead of the one asked for.
func TestReadMemberDoesNotDumpTheWholeZipForAnOptionLikeName(t *testing.T) {
	if _, err := exec.LookPath("zip"); err != nil {
		t.Skip("zip is not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "-x"), []byte("SECRET\n"), 0o644); err != nil {
		t.Skipf("this filesystem will not hold the member name: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "other.txt"), []byte("OTHER\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(dir, "evil.zip")
	build := exec.Command("zip", "-q", archive, "--", "-x", "other.txt")
	build.Dir = src
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("could not build the fixture: %v: %s", err, out)
	}

	data, err := ReadMember(archive, "-x")
	if err != nil {
		// Refusing is the correct outcome where 7-Zip is absent: unzip has no
		// way to be told that "-x" is a filename.
		if tool("7z") == "" && strings.Contains(err.Error(), "unzip") {
			return
		}
		t.Fatalf("ReadMember: %v", err)
	}
	if strings.Contains(string(data), "OTHER") {
		t.Fatalf("ReadMember returned other entries too: %q", data)
	}
	if string(data) != "SECRET\n" {
		t.Errorf("ReadMember = %q, want the -x member's own bytes", data)
	}
}

// A crafted archive can name a member "../../x"; writing it back must not land
// outside the scratch directory.
func TestWriteMemberFileRefusesToEscape(t *testing.T) {
	dir := t.TempDir()
	if _, err := writeMemberFile(dir, "../escaped", []byte("x")); err == nil {
		t.Fatal("writeMemberFile wrote outside its directory")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped")); err == nil {
		t.Fatal("the escaping file was created")
	}
}
