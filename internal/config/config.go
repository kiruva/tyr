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

	// Startup is what the panes open on: "cwd" (the default) starts where the
	// shell was, "last" restores the directories from the previous run.
	Startup string

	// Delete is what F8 does: "trash" (the default) moves to the desktop trash,
	// "remove" unlinks straight away.
	Delete string

	// Mouse is whether clicks and the wheel are acted on: "on" (the default) or
	// "off", for a terminal where the mouse should stay the terminal's.
	Mouse string

	// Panes is the view state each side was left in, saved on exit.
	Panes [2]PaneState

	// Keys is the rebound actions, by the name the app knows them under.
	Keys map[string][]string
}

// PaneState is one pane's remembered view: where it was and how it was showing
// things. A pane that was on a remote host saves no path — the connection is
// not restored on the next run, so neither is the location.
type PaneState struct {
	Path   string
	Sort   string // "name", "size" or "time"
	Hidden bool   // dotfiles were visible
}

// RestorePaths reports whether the saved directories should be reopened.
func (c Config) RestorePaths() bool { return strings.EqualFold(c.Startup, "last") }

// MouseEnabled reports whether tyr should ask the terminal for mouse events.
// Only an explicit "off" turns it off.
func (c Config) MouseEnabled() bool { return !strings.EqualFold(c.Mouse, "off") }

// TrashDeletes reports whether a delete should go to the desktop trash. Only an
// explicit "remove" turns it off: an unset or misspelled value keeps the
// reversible behaviour, which is the one that cannot lose anything.
func (c Config) TrashDeletes() bool { return !strings.EqualFold(c.Delete, "remove") }

// paneKeys names the config keys for one side. The prefix is what a hand-edited
// file reads as: "left.path", "right.sort", and so on.
var paneKeys = [2]string{"left", "right"}

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

	cfg := Config{
		Theme:   lookup(pairs, "theme"),
		Startup: lookup(pairs, "startup"),
		Delete:  lookup(pairs, "delete"),
		Mouse:   lookup(pairs, "mouse"),
		Keys:    keysFrom(pairs),
	}
	for i, prefix := range paneKeys {
		cfg.Panes[i] = PaneState{
			Path:   lookup(pairs, prefix+".path"),
			Sort:   lookup(pairs, prefix+".sort"),
			Hidden: truthy(lookup(pairs, prefix+".hidden")),
		}
	}
	return cfg, nil
}

// SaveSession writes the per-pane view state back, leaving every other setting
// alone. An empty path is not written: a pane that ended the run on a remote
// host or could not report a location keeps whatever was saved before.
func SaveSession(panes [2]PaneState) error {
	return rewrite(func(pairs map[string]string) {
		for i, prefix := range paneKeys {
			p := panes[i]
			if p.Path != "" {
				setPreservingCase(pairs, prefix+".path", p.Path)
			}
			if p.Sort != "" {
				setPreservingCase(pairs, prefix+".sort", p.Sort)
			}
			setPreservingCase(pairs, prefix+".hidden", boolText(p.Hidden))
		}
	})
}

// truthy reads the spellings a hand-written config might use for "yes".
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "on", "1":
		return true
	}
	return false
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
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
