package config

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Connections are stored as flat namespaced keys so the file stays a plain
// "key = value" list that a person can read and edit:
//
//	conn.prod.host = example.com
//	conn.prod.user = deploy
//	conn.prod.port = 2222
//	conn.prod.path = /srv/www
//	conn.prod.lastused = 1755400000
//
// A password is never among them. Nothing in this package writes or reads one.

const connPrefix = "conn."

// Connection is a saved ssh destination.
type Connection struct {
	Name     string // the label shown in the picker, and the config key
	Host     string // hostname or an ~/.ssh/config alias
	User     string // empty means "whatever ssh_config or $USER says"
	Port     string // empty means 22
	Path     string // directory to open on connect; empty means the login dir
	Identity string // explicit private key file, optional
	LastUsed int64  // unix seconds, for ordering the picker
}

// Label renders the connection the way the picker lists it.
func (c Connection) Label() string {
	target := c.Host
	if c.User != "" {
		target = c.User + "@" + c.Host
	}
	if c.Port != "" && c.Port != "22" {
		target += ":" + c.Port
	}
	if c.Path != "" {
		target += " " + c.Path
	}
	return target
}

// ValidConnectionName reports whether name can be used as a config key. The
// separator and comment characters are what would make the file ambiguous.
func ValidConnectionName(name string) error { return validKeyName(name) }

// Connections lists the saved connections, most recently used first.
func Connections() ([]Connection, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	pairs, err := readPairs(path)
	if err != nil {
		return nil, err
	}
	return connectionsFrom(pairs), nil
}

func connectionsFrom(pairs map[string]string) []Connection {
	byName := map[string]*Connection{}
	for key, value := range pairs {
		if !strings.EqualFold(key[:min(len(key), len(connPrefix))], connPrefix) {
			continue
		}
		// Split on the LAST dot: a connection named after a host contains dots,
		// and the field name never does.
		rest := key[len(connPrefix):]
		dot := strings.LastIndex(rest, ".")
		if dot <= 0 {
			continue
		}
		name, field := rest[:dot], strings.ToLower(rest[dot+1:])
		c, seen := byName[name]
		if !seen {
			c = &Connection{Name: name}
			byName[name] = c
		}
		switch field {
		case "host":
			c.Host = value
		case "user":
			c.User = value
		case "port":
			c.Port = value
		case "path":
			c.Path = value
		case "identity":
			c.Identity = value
		case "lastused":
			c.LastUsed, _ = strconv.ParseInt(value, 10, 64)
		}
	}

	out := make([]Connection, 0, len(byName))
	for _, c := range byName {
		if c.Host == "" {
			continue // an entry without a host cannot be connected to
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastUsed != out[j].LastUsed {
			return out[i].LastUsed > out[j].LastUsed
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// SaveConnection writes or replaces a connection, leaving every other setting
// (and every unknown key) in the file untouched.
func SaveConnection(c Connection) error {
	if err := ValidConnectionName(c.Name); err != nil {
		return err
	}
	return rewrite(func(pairs map[string]string) {
		prefix := connPrefix + strings.TrimSpace(c.Name) + "."
		deletePrefixed(pairs, prefix)
		pairs[prefix+"host"] = c.Host
		setIfPresent(pairs, prefix+"user", c.User)
		setIfPresent(pairs, prefix+"port", c.Port)
		setIfPresent(pairs, prefix+"path", c.Path)
		setIfPresent(pairs, prefix+"identity", c.Identity)
		if c.LastUsed > 0 {
			pairs[prefix+"lastused"] = strconv.FormatInt(c.LastUsed, 10)
		}
	})
}

// TouchConnection records that a connection was just used, so the picker offers
// it first next time.
func TouchConnection(name string) error {
	return rewrite(func(pairs map[string]string) {
		prefix := connPrefix + strings.TrimSpace(name) + "."
		if _, ok := pairs[prefix+"host"]; !ok {
			return
		}
		pairs[prefix+"lastused"] = strconv.FormatInt(time.Now().Unix(), 10)
	})
}

// DeleteConnection removes a saved connection.
func DeleteConnection(name string) error {
	return rewrite(func(pairs map[string]string) {
		deletePrefixed(pairs, connPrefix+strings.TrimSpace(name)+".")
	})
}

// deletePrefixed removes every key under a connection's namespace, matching the
// prefix case-insensitively so a hand-edited file still lines up.
func deletePrefixed(pairs map[string]string, prefix string) {
	for key := range pairs {
		if len(key) >= len(prefix) && strings.EqualFold(key[:len(prefix)], prefix) {
			delete(pairs, key)
		}
	}
}

func setIfPresent(pairs map[string]string, key, value string) {
	if strings.TrimSpace(value) != "" {
		pairs[key] = value
	}
}
