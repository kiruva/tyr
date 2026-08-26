package trash

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolate points both the XDG data directory and the home directory at a temp
// dir, so a test never touches the real trash on either platform.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("HOME", dir)
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMoveAndRestore(t *testing.T) {
	isolate(t)
	work := t.TempDir()
	file := filepath.Join(work, "notes.txt")
	write(t, file, "keep me")

	item, err := Move(file)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("the file is still where it was")
	}
	if _, err := os.Stat(item.Path); err != nil {
		t.Fatalf("nothing at the trashed path: %v", err)
	}

	if err := Restore([]Item{item}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the file did not come back: %v", err)
	}
	if string(data) != "keep me" {
		t.Errorf("restored contents = %q", data)
	}
}

// Two files of the same name can be trashed without one replacing the other.
func TestMoveKeepsBothNames(t *testing.T) {
	isolate(t)
	first, second := t.TempDir(), t.TempDir()
	write(t, filepath.Join(first, "same.txt"), "first")
	write(t, filepath.Join(second, "same.txt"), "second")

	a, err := Move(filepath.Join(first, "same.txt"))
	if err != nil {
		t.Fatalf("move first: %v", err)
	}
	b, err := Move(filepath.Join(second, "same.txt"))
	if err != nil {
		t.Fatalf("move second: %v", err)
	}
	if a.Path == b.Path {
		t.Fatalf("both went to %s — one replaced the other", a.Path)
	}

	for _, it := range []Item{a, b} {
		if _, err := os.Stat(it.Path); err != nil {
			t.Errorf("%s is gone: %v", it.Path, err)
		}
	}
}

// Restoring onto a name that has been taken since puts the file back beside it
// rather than over it.
func TestRestoreDoesNotOverwrite(t *testing.T) {
	isolate(t)
	work := t.TempDir()
	file := filepath.Join(work, "notes.txt")
	write(t, file, "original")

	item, err := Move(file)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	write(t, file, "something else")

	if err := Restore([]Item{item}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got, _ := os.ReadFile(file); string(got) != "something else" {
		t.Errorf("the new file was overwritten: %q", got)
	}
	if got, err := os.ReadFile(filepath.Join(work, "notes (2).txt")); err != nil {
		t.Errorf("the restored file is missing: %v", err)
	} else if string(got) != "original" {
		t.Errorf("restored contents = %q", got)
	}
}

// On the platforms that use it, a .trashinfo record says where the file came
// from, which is what makes any file manager able to put it back.
func TestTrashInfoRecord(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS trash keeps no origin record")
	}
	base := isolate(t)
	work := t.TempDir()
	file := filepath.Join(work, "notes.txt")
	write(t, file, "x")

	item, err := Move(file)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if item.Info == "" {
		t.Fatal("no record was written")
	}
	if want := filepath.Join(base, "Trash", "info"); filepath.Dir(item.Info) != want {
		t.Errorf("record in %s, want %s", filepath.Dir(item.Info), want)
	}

	body, err := os.ReadFile(item.Info)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if !strings.Contains(string(body), "[Trash Info]") || !strings.Contains(string(body), "Path=") {
		t.Errorf("record is not in the expected format:\n%s", body)
	}
	if !strings.Contains(string(body), file) {
		t.Errorf("record does not name the original path:\n%s", body)
	}

	// Restoring takes the record with it.
	if err := Restore([]Item{item}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(item.Info); !os.IsNotExist(err) {
		t.Error("the record outlived the file it described")
	}
}

func TestRestoreReportsAMissingItem(t *testing.T) {
	isolate(t)
	err := Restore([]Item{{Original: "/tmp/gone.txt", Path: filepath.Join(t.TempDir(), "gone.txt")}})
	if err == nil {
		t.Fatal("restoring something that is not there succeeded")
	}
	if !strings.Contains(err.Error(), "gone.txt") {
		t.Errorf("err = %v, want it to name the file", err)
	}
}

func TestLabel(t *testing.T) {
	if got := Label(nil); got != "nothing" {
		t.Errorf("Label(nil) = %q", got)
	}
	if got := Label([]Item{{Original: "/a/b.txt"}}); got != "b.txt" {
		t.Errorf("Label(one) = %q, want b.txt", got)
	}
	if got := Label([]Item{{}, {}}); got != "2 items" {
		t.Errorf("Label(two) = %q, want 2 items", got)
	}
}
