package config

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestConnectionNameWithDots pins the key scheme: the field is split off the end
// of the key, so a connection named after a host round-trips intact.
func TestConnectionNameWithDots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	in := Connection{Name: "web01.example.com", Host: "web01.example.com", User: "kim", Path: "/srv"}
	if err := SaveConnection(in); err != nil {
		t.Fatal(err)
	}
	conns, err := Connections()
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 || conns[0] != in {
		t.Fatalf("conns = %+v, want %+v", conns, in)
	}

	if err := DeleteConnection("web01.example.com"); err != nil {
		t.Fatal(err)
	}
	if conns, _ := Connections(); len(conns) != 0 {
		t.Fatalf("delete left %+v", conns)
	}
}

func TestConnectionRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	in := Connection{
		Name: "prod", Host: "web01.example.com", User: "deploy",
		Port: "2222", Path: "/srv/www", Identity: "~/.ssh/deploy_key", LastUsed: 1700,
	}
	if err := SaveConnection(in); err != nil {
		t.Fatalf("save: %v", err)
	}

	conns, err := Connections()
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 {
		t.Fatalf("conns = %+v", conns)
	}
	if conns[0] != in {
		t.Fatalf("round trip = %+v, want %+v", conns[0], in)
	}
}

// TestConnectionsNeverStorePassword is the guarantee the feature rests on: there
// is no field for one, and nothing a connection carries reaches the file except
// the fields listed here.
func TestConnectionsNeverStorePassword(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := SaveConnection(Connection{
		Name: "prod", Host: "example.com", User: "deploy", Path: "/srv",
	}); err != nil {
		t.Fatal(err)
	}
	if err := Save(Config{Theme: "nord"}); err != nil {
		t.Fatal(err)
	}

	path, _ := Path()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(body))
	for _, forbidden := range []string{"password", "passwd", "secret"} {
		// The header comment mentions passwords on purpose; a key would not.
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if strings.Contains(line, forbidden) {
				t.Fatalf("config contains %q: %s", forbidden, line)
			}
		}
	}

	// Windows has no permission bits: a file there is protected by an ACL, and
	// os.Chmod only flips the read-only attribute.
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("config permissions = %o, want 600", perm)
		}
	}
}

func TestConnectionsOrderedByLastUsed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for _, c := range []Connection{
		{Name: "old", Host: "a", LastUsed: 100},
		{Name: "newest", Host: "b", LastUsed: 300},
		{Name: "middle", Host: "c", LastUsed: 200},
		{Name: "never", Host: "d"},
	} {
		if err := SaveConnection(c); err != nil {
			t.Fatal(err)
		}
	}

	conns, err := Connections()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(conns))
	for _, c := range conns {
		got = append(got, c.Name)
	}
	want := []string{"newest", "middle", "old", "never"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestTouchConnection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := SaveConnection(Connection{Name: "prod", Host: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := TouchConnection("prod"); err != nil {
		t.Fatal(err)
	}
	conns, _ := Connections()
	if conns[0].LastUsed == 0 {
		t.Fatal("touch did not record a timestamp")
	}

	// Touching an unknown connection must not invent one.
	if err := TouchConnection("nope"); err != nil {
		t.Fatal(err)
	}
	if conns, _ := Connections(); len(conns) != 1 {
		t.Fatalf("conns = %+v", conns)
	}
}

func TestDeleteConnectionLeavesOthers(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := SaveConnection(Connection{Name: "keep", Host: "a", User: "u"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveConnection(Connection{Name: "drop", Host: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(Config{Theme: "nord"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConnection("drop"); err != nil {
		t.Fatal(err)
	}

	conns, _ := Connections()
	if len(conns) != 1 || conns[0].Name != "keep" || conns[0].User != "u" {
		t.Fatalf("conns = %+v", conns)
	}
	cfg, _ := Load()
	if cfg.Theme != "nord" {
		t.Fatalf("deleting a connection lost the theme: %+v", cfg)
	}
}

// TestSaveConnectionReplacesFields makes sure clearing a field in the form
// actually clears it, rather than leaving the old value behind.
func TestSaveConnectionReplacesFields(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := SaveConnection(Connection{Name: "prod", Host: "a", User: "old", Port: "2222"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveConnection(Connection{Name: "prod", Host: "a"}); err != nil {
		t.Fatal(err)
	}

	conns, _ := Connections()
	if len(conns) != 1 {
		t.Fatalf("conns = %+v", conns)
	}
	if conns[0].User != "" || conns[0].Port != "" {
		t.Fatalf("stale fields survived: %+v", conns[0])
	}
}

func TestValidConnectionName(t *testing.T) {
	for _, bad := range []string{"", "   ", "has space", "has=eq", "has#hash"} {
		if err := ValidConnectionName(bad); err == nil {
			t.Fatalf("%q should be rejected", bad)
		}
	}
	// Dots are allowed: naming a connection after its host is the obvious default.
	for _, good := range []string{"prod", "web-01", "web_01", "Prod2", "web01.example.com"} {
		if err := ValidConnectionName(good); err != nil {
			t.Fatalf("%q should be accepted: %v", good, err)
		}
	}
}

func TestConnectionLabel(t *testing.T) {
	cases := []struct {
		want string
		conn Connection
	}{
		{"example.com", Connection{Host: "example.com"}},
		{"kim@example.com", Connection{Host: "example.com", User: "kim"}},
		{"kim@example.com:2222", Connection{Host: "example.com", User: "kim", Port: "2222"}},
		{"kim@example.com /srv", Connection{Host: "example.com", User: "kim", Path: "/srv"}},
		{"example.com", Connection{Host: "example.com", Port: "22"}}, // the default port is not noise worth showing
	}
	for _, c := range cases {
		if got := c.conn.Label(); got != c.want {
			t.Fatalf("Label() = %q, want %q", got, c.want)
		}
	}
}

func TestConnectionsIgnoreEntriesWithoutHost(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := rewrite(func(pairs map[string]string) {
		pairs["conn.broken.user"] = "kim" // hand-edited, no host
		pairs["conn.fine.host"] = "example.com"
	}); err != nil {
		t.Fatal(err)
	}

	conns, _ := Connections()
	if len(conns) != 1 || conns[0].Name != "fine" {
		t.Fatalf("conns = %+v", conns)
	}
}
