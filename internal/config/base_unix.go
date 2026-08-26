//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

// configBase is where a config directory goes when XDG_CONFIG_HOME says
// nothing: ~/.config, as the specification's own default.
func configBase() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}
