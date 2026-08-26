package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSSHConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	setHome(t, home)
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSSHConfigResolvesAlias(t *testing.T) {
	path := writeSSHConfig(t, `
# a comment
Host prod
  HostName web01.example.com
  User deploy
  Port 2222
  IdentityFile ~/.ssh/deploy_key

Host *.internal
  User admin

Host *
  Port 2200
`)

	cfg := parseSSHConfig(path, "prod", 0)
	if cfg.HostName != "web01.example.com" {
		t.Fatalf("HostName = %q", cfg.HostName)
	}
	if cfg.User != "deploy" {
		t.Fatalf("User = %q", cfg.User)
	}
	// First match wins, so the catch-all Port must not override the block above.
	if cfg.Port != "2222" {
		t.Fatalf("Port = %q, want 2222", cfg.Port)
	}
	if len(cfg.IdentityFiles) != 1 || filepath.Base(cfg.IdentityFiles[0]) != "deploy_key" {
		t.Fatalf("IdentityFiles = %v", cfg.IdentityFiles)
	}

	// A host that only matches the wildcard picks up its Port.
	other := parseSSHConfig(path, "somewhere-else", 0)
	if other.Port != "2200" {
		t.Fatalf("wildcard Port = %q", other.Port)
	}
	if other.HostName != "" {
		t.Fatalf("wildcard should not set HostName, got %q", other.HostName)
	}

	// Pattern blocks apply to what they match.
	inner := parseSSHConfig(path, "db.internal", 0)
	if inner.User != "admin" {
		t.Fatalf("pattern User = %q", inner.User)
	}
}

func TestSSHConfigKeyValueForms(t *testing.T) {
	path := writeSSHConfig(t, "Host box\n\tHostName=example.com\n\tUser  \t kim\n\tProxyJump jump.example.com\n")

	cfg := parseSSHConfig(path, "box", 0)
	if cfg.HostName != "example.com" || cfg.User != "kim" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.ProxyJump != "jump.example.com" {
		t.Fatalf("ProxyJump = %q", cfg.ProxyJump)
	}
}

func TestSSHConfigNegatedPattern(t *testing.T) {
	path := writeSSHConfig(t, "Host * !secret\n  User general\n\nHost secret\n  User hidden\n")

	if got := parseSSHConfig(path, "anything", 0).User; got != "general" {
		t.Fatalf("User = %q", got)
	}
	if got := parseSSHConfig(path, "secret", 0).User; got != "hidden" {
		t.Fatalf("negated host took the wildcard block: User = %q", got)
	}
}

func TestSSHConfigInclude(t *testing.T) {
	path := writeSSHConfig(t, "Include extra.conf\n\nHost box\n  User fallback\n")
	dir := filepath.Dir(path)
	if err := os.WriteFile(filepath.Join(dir, "extra.conf"),
		[]byte("Host box\n  HostName from-include.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := parseSSHConfig(path, "box", 0)
	if cfg.HostName != "from-include.example.com" {
		t.Fatalf("HostName = %q", cfg.HostName)
	}
	if cfg.User != "fallback" {
		t.Fatalf("User = %q", cfg.User)
	}
}

func TestSSHConfigMissingFile(t *testing.T) {
	setHome(t, t.TempDir())
	cfg := parseSSHConfig(filepath.Join(t.TempDir(), "nope"), "box", 0)
	if cfg.HostName != "" || cfg.User != "" || cfg.Port != "" || cfg.ProxyJump != "" || len(cfg.IdentityFiles) != 0 {
		t.Fatalf("missing file should yield the zero config, got %+v", cfg)
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"*", "anything", true},
		{"web*", "web01", true},
		{"web*", "db01", false},
		{"*.example.com", "a.example.com", true},
		{"*.example.com", "example.com", false},
		{"web?1", "web01", true},
		{"web?1", "web001", false},
		{"exact", "exact", true},
		{"exact", "exactly", false},
		{"*-*", "a-b", true},
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxbyy", false},
	}
	for _, c := range cases {
		if got := matchPattern(c.pattern, c.s); got != c.want {
			t.Fatalf("matchPattern(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}
