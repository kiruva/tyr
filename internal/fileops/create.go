package fileops

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kiruva/tyr/internal/fsutil"
)

// Creating a name is not a Job: it touches one inode, finishes instantly, and
// has nothing to report progress about. Both helpers refuse to clobber an
// existing name — overwriting is what copy/move ask about, not create.

// CreateDir makes a directory, including any missing parents.
func CreateDir(path string) error {
	if fsutil.Exists(path) {
		return alreadyExists(path)
	}
	return os.MkdirAll(path, 0o755)
}

// CreateFile makes an empty file, creating any missing parent directories.
func CreateFile(path string) error {
	if fsutil.Exists(path) {
		return alreadyExists(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func alreadyExists(path string) error {
	return fmt.Errorf("%s already exists", filepath.Base(path))
}
