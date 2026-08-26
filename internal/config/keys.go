package config

import (
	"sort"
	"strings"
)

// Rebound keys are one line each, the same shape as everything else in the
// file, with the keys comma-separated:
//
//	key.copy = f5,c
//	key.select = space
//
// Only what differs from the defaults is written, so a config file says what
// was changed rather than restating the whole keymap.

const keyPrefix = "key."

// Keys reads the rebindings. The values are the raw key names; what they mean
// is the app's business, not this package's.
func Keys() (map[string][]string, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	pairs, err := readPairs(path)
	if err != nil {
		return nil, err
	}
	return keysFrom(pairs), nil
}

func keysFrom(pairs map[string]string) map[string][]string {
	out := map[string][]string{}
	for key, value := range pairs {
		if len(key) <= len(keyPrefix) || !strings.EqualFold(key[:len(keyPrefix)], keyPrefix) {
			continue
		}
		if names := splitKeys(value); len(names) > 0 {
			out[strings.ToLower(key[len(keyPrefix):])] = names
		}
	}
	return out
}

// SaveKeys replaces the rebindings with what is passed, so an action left out
// of the map goes back to its default rather than keeping an old line.
func SaveKeys(binds map[string][]string) error {
	return rewrite(func(pairs map[string]string) {
		for key := range pairs {
			if len(key) > len(keyPrefix) && strings.EqualFold(key[:len(keyPrefix)], keyPrefix) {
				delete(pairs, key)
			}
		}
		ids := make([]string, 0, len(binds))
		for id := range binds {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		for _, id := range ids {
			if names := binds[id]; len(names) > 0 {
				pairs[keyPrefix+id] = strings.Join(names, ",")
			}
		}
	})
}

// splitKeys reads "f5,c" into its parts. A key whose own name is a comma is
// written as "comma" by the app, so splitting here is unambiguous.
func splitKeys(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
