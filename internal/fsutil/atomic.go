package fsutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Nothing tyr writes is truncated in place. A file is written beside its
// destination under a temporary name and renamed over it once it is whole, so
// a failure, a cancellation or a full disk part-way through leaves the old file
// as it was rather than half of the new one.
//
// The rename makes a new file, which has two costs worth knowing: a hard link
// to the old file keeps the old contents, and the file's owner becomes whoever
// wrote it.

// TempBeside creates an empty, private file next to dst to write into before
// Commit puts it in place. Its name starts with a dot and carries dst's name,
// so a leftover from a crash is hidden and still recognisable.
func TempBeside(dst string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tyr-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// Commit gives a finished temporary file its final mode and renames it over
// dst — dst itself, so a symlink there is replaced, not followed. On failure
// tmp is removed and dst is untouched.
func Commit(tmp, dst string, perm os.FileMode) error {
	err := os.Chmod(tmp, perm)
	if err == nil {
		err = replace(tmp, dst)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// replace renames tmp over dst. Windows refuses to replace a read-only file,
// which Unix does not care about (a rename asks the folder, not the file), so
// there the read-only bit is lifted for the rename and put back if it fails.
func replace(tmp, dst string) error {
	err := os.Rename(tmp, dst)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	info, serr := os.Lstat(dst)
	if serr != nil || info.Mode().Perm()&0o200 != 0 {
		return err
	}
	if os.Chmod(dst, 0o666) != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Chmod(dst, info.Mode().Perm())
		return err
	}
	return nil
}

// Replace writes path through write, by way of a temporary file beside it that
// is synced to disk before it is renamed into place with mode perm. A symlink
// at path is followed: the file it points at is the one being written, and the
// link stays. When write fails, the error is returned and path is untouched.
func Replace(path string, perm os.FileMode, write func(*os.File) error) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	tmp, err := TempBeside(path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	err = write(f)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return Commit(tmp, path, perm)
}

// WriteFile is os.WriteFile done through Replace.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return Replace(path, perm, func(f *os.File) error {
		_, err := f.Write(data)
		return err
	})
}
