//go:build !windows

package trash

import (
	"os"
	"path/filepath"
)

// dataBase is the XDG default: ~/.local/share, holding Trash/files and
// Trash/info.
func dataBase() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}
