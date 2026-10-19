package fileops

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// skipIfAllPowerful skips a test that relies on a permission being refused,
// which root (and Windows, for a folder) does not do.
func skipIfAllPowerful(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for this user")
	}
}

// A read-only image strips like any other. The final mode went onto the
// temporary file before it was written, so writing it was refused.
func TestStripReadOnlyImage(t *testing.T) {
	skipIfAllPowerful(t)
	src, dst := t.TempDir(), t.TempDir()
	in := filepath.Join(src, "photo.jpg")
	writeFile(t, in, dirtyJPEG(t, 0), 0o444)

	res := runConvert(t, &fakeTools{}, imageJob([]string{in}, dst), ConflictAsk)
	if res.Err != nil {
		t.Fatalf("strip: %v", res.Err)
	}
	if got := modeOf(t, filepath.Join(dst, "photo.jpg")); got != 0o444 {
		t.Errorf("copy mode = %v, want the original's 0444", got)
	}
}

// Overwriting a file that then cannot be copied keeps the file that was there:
// the old one goes only once the new one is whole.
func TestCopyOverwriteFailureKeepsTheTarget(t *testing.T) {
	skipIfAllPowerful(t)
	src, dst := t.TempDir(), t.TempDir()
	in := filepath.Join(src, "a.txt")
	seed(t, in, "new", 0)
	if err := os.Chmod(in, 0); err != nil {
		t.Fatal(err)
	}
	seed(t, filepath.Join(dst, "a.txt"), "old", 0)

	res := run(t, Job{Op: OpCopy, Srcs: []string{in}, Dest: dst}, ConflictOverwrite)
	if res.Err == nil {
		t.Fatal("copying an unreadable file succeeded")
	}
	if got := read(t, filepath.Join(dst, "a.txt")); got != "old" {
		t.Errorf("target = %q, want the old file kept", got)
	}
	if extra := entries(t, dst); len(extra) != 1 {
		t.Errorf("destination holds %v, want only a.txt", extra)
	}
}

// A pack over an existing archive replaces it whole, whatever the format: zip
// and 7z used to add to it, and a failed tar pack had already truncated it.
func TestPackOverwriteReplacesWhole(t *testing.T) {
	dir, srcs := packTree(t)
	for _, f := range []struct {
		name   string
		format PackFormat
		bins   []string
	}{
		{"out.tar.gz", PackTarGz, []string{"tar", "gzip"}},
		{"out.zip", PackZip, []string{"zip", "unzip"}},
	} {
		t.Run(f.name, func(t *testing.T) {
			needTools(t, f.bins...)
			out := filepath.Join(t.TempDir(), f.name)
			if f.format == PackZip {
				writeZip(t, out, entry{"stale.txt", []byte("old")})
			} else {
				writeTar(t, out+".tgz", entry{"stale.txt", []byte("old")})
				if err := os.Rename(out+".tgz", out); err != nil {
					t.Fatal(err)
				}
			}
			before := read(t, out)

			// A failing pack leaves the old archive alone.
			res := runJob(t, Job{Op: OpPack, Srcs: []string{filepath.Join(dir, "missing")}, Out: out,
				Pack: PackOpts{Format: f.format, Level: 6}})
			if res.Err == nil {
				t.Fatal("packing a missing source succeeded")
			}
			if read(t, out) != before {
				t.Error("a failed pack changed the existing archive")
			}

			// A good one replaces it.
			res = runJob(t, Job{Op: OpPack, Srcs: srcs, Out: out, Pack: PackOpts{Format: f.format, Level: 6}})
			if res.Err != nil {
				t.Fatalf("pack: %v", res.Err)
			}
			if _, err := ReadMember(out, "stale.txt"); err == nil {
				t.Error("the old archive's member survived an overwrite")
			}
			if got, _ := ReadMember(out, "a.txt"); string(got) != "hello a" {
				t.Errorf("a.txt = %q", got)
			}
			if extra := entries(t, filepath.Dir(out)); len(extra) != 1 {
				t.Errorf("left behind: %v", extra)
			}
		})
	}
}

// Saving a member of a plain tar works with any tar: bsdtar has no --delete,
// so on macOS and Windows it always failed.
func TestWriteMemberPlainTar(t *testing.T) {
	needTools(t, "tar")
	for _, name := range []string{"a.tar", "a.tgz"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			arc := filepath.Join(dir, name)
			writeTar(t, arc, entry{"docs/", nil}, entry{"docs/a.txt", []byte("old")}, entry{"b.txt", []byte("b")})
			if err := os.Chmod(arc, 0o640); err != nil {
				t.Fatal(err)
			}

			if err := WriteMember(arc, "docs/a.txt", []byte("new")); err != nil {
				t.Fatalf("WriteMember: %v", err)
			}
			if got, _ := ReadMember(arc, "docs/a.txt"); string(got) != "new" {
				t.Errorf("docs/a.txt = %q, want new", got)
			}
			if got, _ := ReadMember(arc, "b.txt"); string(got) != "b" {
				t.Errorf("b.txt = %q, want it kept", got)
			}
			if runtime.GOOS != "windows" {
				if got := modeOf(t, arc); got != 0o640 {
					t.Errorf("archive mode = %v, want 0640 kept", got)
				}
			}
			if extra := entries(t, dir); len(extra) != 1 {
				t.Errorf("left behind: %v", extra)
			}
		})
	}
}

// A zip member's name is a name, not a pattern: saving a[1].txt leaves a1.txt.
func TestWriteMemberZipLiteralName(t *testing.T) {
	needTools(t, "zip", "unzip")
	arc := filepath.Join(t.TempDir(), "a.zip")
	writeZip(t, arc, entry{"a[1].txt", []byte("bracket")}, entry{"a1.txt", []byte("plain")})

	if err := WriteMember(arc, "a[1].txt", []byte("edited")); err != nil {
		t.Fatalf("WriteMember: %v", err)
	}
	if got, _ := ReadMember(arc, "a[1].txt"); string(got) != "edited" {
		t.Errorf("a[1].txt = %q", got)
	}
	if got, _ := ReadMember(arc, "a1.txt"); string(got) != "plain" {
		t.Errorf("a1.txt = %q, want it untouched", got)
	}
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, e.Name())
	}
	return out
}
