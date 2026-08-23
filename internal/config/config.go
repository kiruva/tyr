// Package config reads and writes tyr's tiny key = value settings file.
//
// Nothing here ever stores an ssh password: a saved connection records where to
// connect and as whom, and the password is asked for each time the app starts.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config holds the persisted scalar settings.
type Config struct {
	Theme string
}

// dirName is the config directory tyr owns, under whichever base applies.
const dirName = "tyr"

// Dir is the directory holding the config file: $XDG_CONFIG_HOME/tyr,
// falling back to ~/.config/tyr.
func Dir() (string, error) { return dirNamed(dirName) }

// dirNamed resolves a config directory by name. Migrate uses it for the
// pre-rename name, so the two paths cannot drift apart.
func dirNamed(name string) (string, error) {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, name), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", name), nil
}

// Path is the full path of the config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config"), nil
}

// Load reads the config file. A missing file is not an error — it yields the
// zero Config, so a first run behaves like an empty config.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	pairs, err := readPairs(path)
	if err != nil {
		return Config{}, err
	}
	return Config{Theme: lookup(pairs, "theme")}, nil
}

// Save writes the scalar settings back, preserving connections and any keys
// tyr doesn't know.
func Save(c Config) error {
	return rewrite(func(pairs map[string]string) {
		if c.Theme != "" {
			setPreservingCase(pairs, "theme", c.Theme)
		}
	})
}

// rewrite loads the file, hands the key/value pairs to mutate, and writes the
// result back. Every write goes through here, so no path can drop a setting it
// does not know about.
func rewrite(mutate func(map[string]string)) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	pairs, err := readPairs(path)
	if err != nil {
		return err
	}
	mutate(pairs)

	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("# tyr configuration\n")
	b.WriteString("# ssh passwords are never stored here\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %s\n", k, pairs[k])
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// lookup finds a scalar setting case-insensitively. Keys keep the case they were
// written with — a connection name is a display label — so reads normalise
// instead of the file.
func lookup(pairs map[string]string, key string) string {
	if v, ok := pairs[key]; ok {
		return v
	}
	for k, v := range pairs {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// setPreservingCase updates a key without changing how it is spelled in the file.
func setPreservingCase(pairs map[string]string, key, value string) {
	for k := range pairs {
		if strings.EqualFold(k, key) {
			pairs[k] = value
			return
		}
	}
	pairs[key] = value
}

// readPairs parses "key = value" lines, ignoring blanks and # comments.
func readPairs(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	defer f.Close()

	pairs := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		pairs[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return pairs, sc.Err()
}
