// Package trash moves files to the desktop trash instead of unlinking them, so
// a delete stays reversible for as long as the trash is not emptied.
//
// On Linux this is the freedesktop.org trash: the file goes to
// $XDG_DATA_HOME/Trash/files and a .trashinfo record beside it in
// $XDG_DATA_HOME/Trash/info says where it came from, which is what makes it
// restorable from any file manager and not just this one. On macOS it is
// ~/.Trash, which keeps no such record — Finder's "Put Back" is backed by
// metadata tyr does not write, so a restore from there is a plain move. On
// Windows it is %LocalAppData%\tyr\Trash, with the same records: tyr's own
// trash rather than the Recycle Bin, which no API restores from the way Ctrl+Z
// needs to (see base_windows.go).
//
// The trash is one directory on one filesystem. Something on another volume
// cannot be renamed into it, and rather than quietly copying gigabytes across a
// disk boundary, Move says so and lets the caller offer a permanent delete.
package trash

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kiruva/tyr/internal/fsutil"
)

// Item is one thing in the trash and where it came from.
type Item struct {
	Original string // the path it was deleted from
	Path     string // where it now sits inside the trash
	Info     string // the .trashinfo record written for it, or "" on macOS
}

// ErrOtherFilesystem is returned when the file cannot be renamed into the trash
// because the trash lives on another volume.
var ErrOtherFilesystem = errors.New("the trash is on another filesystem")

// Available reports whether a trash directory can be used on this machine.
func Available() bool {
	dir, err := filesDir()
	if err != nil {
		return false
	}
	// The directory itself may not exist yet; its parent has to be writable.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	return true
}

// Dir is the directory trashed files are moved into.
func Dir() (string, error) { return filesDir() }

// Describe names the trash for a confirm prompt, without promising it works.
func Describe() string {
	dir, err := filesDir()
	if err != nil {
		return "the trash"
	}
	return dir
}

// Move puts path in the trash and returns what it did, so it can be undone.
func Move(path string) (Item, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Item{}, err
	}
	if _, err := os.Lstat(abs); err != nil {
		return Item{}, err
	}

	files, err := filesDir()
	if err != nil {
		return Item{}, err
	}
	if err := os.MkdirAll(files, 0o700); err != nil {
		return Item{}, err
	}

	item, err := reserve(filepath.Join(files, filepath.Base(abs)), abs)
	if err != nil {
		return Item{}, err
	}

	if err := os.Rename(abs, item.Path); err != nil {
		if item.Info != "" {
			_ = os.Remove(item.Info)
		}
		if fsutil.CrossDevice(err) {
			return Item{}, fmt.Errorf("%s: %w", filepath.Base(abs), ErrOtherFilesystem)
		}
		return Item{}, err
	}
	return item, nil
}

// Restore puts trashed items back where they came from, newest first so a
// directory is restored before the things that were inside it. A path that has
// since been taken is not overwritten: the item comes back beside it under a
// free name.
//
// One item that cannot come back does not keep the rest in the trash: each is
// tried, and Restore returns the ones that came back along with every failure.
// The caller needs both — what is still in the trash is still to be undone.
func Restore(items []Item) (restored []Item, err error) {
	var errs []error
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		if e := restoreOne(it); e != nil {
			errs = append(errs, e)
			continue
		}
		restored = append(restored, it)
	}
	return restored, errors.Join(errs...)
}

func restoreOne(it Item) error {
	if _, err := os.Lstat(it.Path); err != nil {
		return fmt.Errorf("%s is no longer in the trash", filepath.Base(it.Original))
	}
	if err := os.MkdirAll(filepath.Dir(it.Original), 0o755); err != nil {
		return err
	}
	target, err := fsutil.FreeName(it.Original)
	if err != nil {
		return err
	}
	if err := os.Rename(it.Path, target); err != nil {
		return err
	}
	if it.Info != "" {
		_ = os.Remove(it.Info)
	}
	return nil
}

// Label describes a batch for a prompt or a notice.
func Label(items []Item) string {
	switch len(items) {
	case 0:
		return "nothing"
	case 1:
		return filepath.Base(items[0].Original)
	default:
		return fmt.Sprintf("%d items", len(items))
	}
}

// filesDir resolves the directory trashed files live in.
func filesDir() (string, error) {
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".Trash"), nil
	}
	base, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Trash", "files"), nil
}

// infoDir is where the .trashinfo records go (freedesktop only).
func infoDir() (string, error) {
	base, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Trash", "info"), nil
}

// dataDir is $XDG_DATA_HOME, or ~/.local/share, or %LocalAppData%\tyr on
// Windows. XDG_DATA_HOME wins wherever it is set.
func dataDir() (string, error) {
	if base := os.Getenv("XDG_DATA_HOME"); base != "" {
		return base, nil
	}
	return dataBase()
}

// withInfo reports whether this platform's trash keeps origin records. A
// variable so the record-keeping can be tested on a Mac too.
var withInfo = func() bool { return runtime.GOOS != "darwin" }

// writeInfo records where a trashed file came from, in the format the
// freedesktop trash specification defines.
func writeInfo(name, original string) (string, error) {
	dir, err := infoDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	path := filepath.Join(dir, name+".trashinfo")
	body := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		(&url.URL{Path: original}).EscapedPath(),
		time.Now().Format("2006-01-02T15:04:05"))

	// O_EXCL: the record is the reservation (see reserve), so one already
	// there is an error to step past, never something to write over.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// reserve picks the name original goes into the trash under: path itself, or
// the first free "name (2)"-style variant of it. Where the trash keeps records,
// the record goes first and is what claims the name — created only if no
// record by that name exists, so two programs trashing a notes.txt at once
// cannot both take it, and a record nobody cleaned up is never overwritten. A
// file in the trash with no record is a file whose origin is lost, while a
// record with no file is ignored by everyone.
func reserve(path, original string) (Item, error) {
	for n := 1; n < 100000; n++ {
		candidate := fsutil.NthName(path, n)
		if fsutil.Exists(candidate) {
			continue
		}
		item := Item{Original: original, Path: candidate}
		if !withInfo() {
			return item, nil
		}
		info, err := writeInfo(filepath.Base(candidate), original)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return Item{}, err
		}
		item.Info = info
		return item, nil
	}
	return Item{}, fmt.Errorf("no free name left beside %s", filepath.Base(path))
}
