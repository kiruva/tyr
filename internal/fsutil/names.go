package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Exists reports whether anything (file, dir, or dangling symlink) is at path.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// maxNth is how far FreeName counts before giving up on a name.
const maxNth = 100000

// FreeName returns path itself when nothing is there, or the first free
// variant beside it: "notes.txt" becomes "notes (2).txt", then "notes (3).txt",
// and so on. When every one is taken it fails rather than hand back an
// occupied path, which a rename would land on.
func FreeName(path string) (string, error) {
	for n := 1; n < maxNth; n++ {
		if candidate := NthName(path, n); !Exists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free name left beside %s", filepath.Base(path))
}

// NthName is path for n = 1 and "name (n).ext" beside it after that. The
// suffix goes before the extension so the file still opens in the same
// program; a dotfile is all "extension", so the whole of it is the stem.
func NthName(path string, n int) string {
	if n == 1 {
		return path
	}
	dir, base := filepath.Dir(path), filepath.Base(path)
	ext := filepath.Ext(base)
	if strings.HasPrefix(base, ".") && ext == base {
		ext = ""
	}
	stem := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
}
