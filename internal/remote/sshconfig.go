package remote

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// A native ssh client does not read ~/.ssh/config, so the keys that decide where
// a connection actually goes are parsed here. This is deliberately a subset —
// HostName, User, Port, IdentityFile, IdentitiesOnly, ProxyJump, ProxyCommand and
// the known_hosts options — which is what an alias needs to resolve. Anything else
// in the file is ignored.

type sshHostConfig struct {
	HostName      string
	User          string
	Port          string
	IdentityFiles []string

	// ProxyJump and ProxyCommand are two ways to the host, and the first one
	// the file gives is the one used: once either is set, the other is not.
	ProxyJump    string
	ProxyCommand string

	// IdentitiesOnly offers only the IdentityFile keys, and the agent's
	// copies of them, rather than everything the agent holds.
	IdentitiesOnly bool
	onlySet        bool // IdentitiesOnly was given, yes or no

	UserKnownHostsFiles   []string
	GlobalKnownHostsFiles []string
	HashKnownHosts        bool
	hashSet               bool // HashKnownHosts was given, yes or no
}

// lookupSSHConfig resolves alias through ~/.ssh/config. First match wins per key,
// which is how ssh itself reads the file. Identity files always fall back to the
// standard key names so an entry without IdentityFile still authenticates.
func lookupSSHConfig(alias string) sshHostConfig {
	cfg := parseSSHConfig(sshConfigPath(), alias, 0)
	expandTokens(&cfg, alias)
	cfg.IdentityFiles = append(cfg.IdentityFiles, defaultIdentities()...)
	return cfg
}

// expandTokens replaces ssh's % tokens where ssh does: %h in HostName,
// %d %u %h %p %r %% in the file names, and %h %p %r %n %% in ProxyCommand. "IdentityFile ~/.ssh/%h" is a common way
// to keep a key per host, and was read as a file literally named "%h".
func expandTokens(cfg *sshHostConfig, alias string) {
	home, _ := os.UserHomeDir()
	cfg.HostName = strings.ReplaceAll(cfg.HostName, "%h", alias)
	host := firstNonEmpty(cfg.HostName, alias)
	r := strings.NewReplacer(
		"%%", "%",
		"%d", home,
		"%u", currentUser(),
		"%h", host,
		"%p", firstNonEmpty(cfg.Port, "22"),
		"%r", firstNonEmpty(cfg.User, currentUser()),
	)
	if cfg.ProxyCommand != "" {
		cfg.ProxyCommand = strings.NewReplacer(
			"%%", "%",
			"%h", host,
			"%p", firstNonEmpty(cfg.Port, "22"),
			"%r", firstNonEmpty(cfg.User, currentUser()),
			"%n", alias,
		).Replace(cfg.ProxyCommand)
	}
	for i, f := range cfg.IdentityFiles {
		cfg.IdentityFiles[i] = filepath.Clean(expandHome(r.Replace(f))) // "%d/.ssh" mixes separators on Windows
	}
	for i, f := range cfg.UserKnownHostsFiles {
		cfg.UserKnownHostsFiles[i] = r.Replace(f)
	}
	for i, f := range cfg.GlobalKnownHostsFiles {
		cfg.GlobalKnownHostsFiles[i] = r.Replace(f)
	}
}

func sshConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// parseSSHConfig walks the file, collecting values from every Host block whose
// pattern matches alias. depth bounds Include recursion.
func parseSSHConfig(path, alias string, depth int) sshHostConfig {
	return parseSSHConfigFrom(path, alias, depth, true)
}

// parseSSHConfigFrom is parseSSHConfig with the state the file starts in. The
// top of the main file applies to every host — options before the first Host
// line are defaults, as in ssh — and so does the top of an included file,
// within the block that included it.
func parseSSHConfigFrom(path, alias string, depth int, applies bool) sshHostConfig {
	var cfg sshHostConfig
	if path == "" || depth > 8 {
		return cfg
	}
	f, err := os.Open(path)
	if err != nil {
		return cfg
	}
	defer f.Close()

	sc := bufio.NewScanner(f) // applies: are we inside a block that matches alias?
	for sc.Scan() {
		key, value, ok := configLine(sc.Text())
		if !ok {
			continue
		}

		switch key {
		case "host":
			applies = matchesAny(value, alias)
			continue
		case "match":
			// Match blocks use a different language; treat them as not applying
			// rather than guessing.
			applies = false
			continue
		case "include":
			// Inside a Host block an Include is conditional, like everything
			// else there: another host's include is not this host's settings.
			if applies {
				for _, inc := range expandInclude(value) {
					merge(&cfg, parseSSHConfigFrom(inc, alias, depth+1, true))
				}
			}
			continue
		}
		if !applies {
			continue
		}

		switch key {
		case "hostname":
			setOnce(&cfg.HostName, value)
		case "user":
			setOnce(&cfg.User, value)
		case "port":
			setOnce(&cfg.Port, value)
		case "identityfile":
			cfg.IdentityFiles = append(cfg.IdentityFiles, unquote(value)) // expanded in lookupSSHConfig
		case "proxyjump":
			if cfg.ProxyCommand == "" {
				setOnce(&cfg.ProxyJump, value)
			}
		case "proxycommand":
			if cfg.ProxyJump == "" {
				setOnce(&cfg.ProxyCommand, value)
			}
		case "userknownhostsfile":
			setListOnce(&cfg.UserKnownHostsFiles, value)
		case "globalknownhostsfile":
			setListOnce(&cfg.GlobalKnownHostsFiles, value)
		case "identitiesonly":
			if !cfg.onlySet {
				cfg.IdentitiesOnly, cfg.onlySet = strings.EqualFold(unquote(value), "yes"), true
			}
		case "hashknownhosts":
			if !cfg.hashSet {
				cfg.HashKnownHosts, cfg.hashSet = strings.EqualFold(unquote(value), "yes"), true
			}
		}
	}
	return cfg
}

// configLine splits "Key value" or "Key = value", lowercasing the key.
func configLine(raw string) (key, value string, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	if i := strings.IndexAny(line, " \t="); i >= 0 {
		key = strings.ToLower(line[:i])
		value = strings.TrimSpace(strings.TrimLeft(line[i:], " \t="))
	} else {
		return "", "", false
	}
	if key == "" || value == "" {
		return "", "", false
	}
	return key, value, true
}

// matchesAny reports whether alias matches any pattern in a Host line. A leading
// "!" negates, and a negated match rules the whole line out, as in ssh.
func matchesAny(patterns, alias string) bool {
	matched := false
	for _, p := range strings.Fields(patterns) {
		if negated, ok := strings.CutPrefix(p, "!"); ok {
			if matchPattern(negated, alias) {
				return false
			}
			continue
		}
		if matchPattern(p, alias) {
			matched = true
		}
	}
	return matched
}

// matchPattern implements ssh's glob: "*" spans any run, "?" one character.
func matchPattern(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	return globMatch(pattern, s)
}

func globMatch(pattern, s string) bool {
	// Iterative backtracking match — no allocation, no regexp.
	var pi, si, star, mark int
	star = -1
	for si < len(s) {
		switch {
		case pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]):
			pi++
			si++
		case pi < len(pattern) && pattern[pi] == '*':
			star = pi
			mark = si
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

// expandInclude resolves an Include directive's globs, relative to ~/.ssh.
func expandInclude(value string) []string {
	var out []string
	for _, raw := range strings.Fields(value) {
		p := expandHome(unquote(raw))
		if !filepath.IsAbs(p) {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			p = filepath.Join(home, ".ssh", p)
		}
		matches, err := filepath.Glob(p)
		if err != nil {
			continue
		}
		out = append(out, matches...)
	}
	return out
}

func setOnce(dst *string, value string) {
	if *dst == "" {
		*dst = unquote(value)
	}
}

func merge(dst *sshHostConfig, src sshHostConfig) {
	setOnce(&dst.HostName, src.HostName)
	setOnce(&dst.User, src.User)
	setOnce(&dst.Port, src.Port)
	if dst.ProxyJump == "" && dst.ProxyCommand == "" {
		dst.ProxyJump, dst.ProxyCommand = src.ProxyJump, src.ProxyCommand
	}
	dst.IdentityFiles = append(dst.IdentityFiles, src.IdentityFiles...)
	if len(dst.UserKnownHostsFiles) == 0 {
		dst.UserKnownHostsFiles = src.UserKnownHostsFiles
	}
	if len(dst.GlobalKnownHostsFiles) == 0 {
		dst.GlobalKnownHostsFiles = src.GlobalKnownHostsFiles
	}
	if !dst.hashSet {
		dst.HashKnownHosts, dst.hashSet = src.HashKnownHosts, src.hashSet
	}
	if !dst.onlySet {
		dst.IdentitiesOnly, dst.onlySet = src.IdentitiesOnly, src.onlySet
	}
}

// setListOnce stores a space-separated list of files, unless one was set.
func setListOnce(dst *[]string, value string) {
	if len(*dst) > 0 {
		return
	}
	for _, f := range strings.Fields(value) {
		*dst = append(*dst, unquote(f))
	}
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
