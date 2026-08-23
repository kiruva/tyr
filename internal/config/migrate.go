package config

import (
	"os"
	"path/filepath"
)

// legacyName is the directory name tyr used before it was renamed from
// lazyfiles. It is only ever read, and only by Migrate.
const legacyName = "lazyfiles"

// Migration records a config directory that Migrate moved, so the caller can
// tell the user where their settings went.
type Migration struct {
	From string
	To   string
}

// Migrate moves a pre-rename config directory to the current location, once.
//
// It is deliberately conservative: it does nothing unless the legacy directory
// exists and the current one does not, so it can never overwrite settings the
// user already has under the new name, and it becomes a no-op forever after the
// first successful run. The move is an os.Rename of one sibling directory to
// another, which keeps it atomic and leaves nothing behind to drift out of sync.
//
// A nil Migration with a nil error means there was nothing to do.
func Migrate() (*Migration, error) {
	to, err := Dir()
	if err != nil {
		return nil, err
	}
	from, err := legacyDir()
	if err != nil {
		return nil, err
	}
	if from == to {
		return nil, nil
	}

	switch _, err := os.Lstat(to); {
	case err == nil:
		return nil, nil // already configured under the new name
	case !os.IsNotExist(err):
		return nil, err
	}

	fi, err := os.Lstat(from)
	switch {
	case os.IsNotExist(err):
		return nil, nil // nothing to migrate
	case err != nil:
		return nil, err
	case !fi.IsDir():
		return nil, nil // a stray file by that name is not ours to move
	}

	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return nil, err
	}
	if err := os.Rename(from, to); err != nil {
		return nil, err
	}
	return &Migration{From: from, To: to}, nil
}

// legacyDir is Dir for the pre-rename directory name.
func legacyDir() (string, error) { return dirNamed(legacyName) }
