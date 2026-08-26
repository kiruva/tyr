package remote

import "testing"

// A Windows path is not a host, however much scp syntax it resembles.
func TestParseRejectsWindowsPaths(t *testing.T) {
	for _, in := range []string{
		`C:\Users\kim`,
		`c:/src/tyr`,
		`D:\`,
		`\\server\share`,
		`\Users`,
	} {
		if h, p, ok := Parse(in); ok {
			t.Errorf("Parse(%q) = %v %q, want it read as a local path", in, h, p)
		}
	}
}

// The real remote forms still parse on Windows.
func TestParseStillAcceptsHosts(t *testing.T) {
	for _, in := range []string{
		"kim@nas:/srv",
		"web01:",
		"ssh://deploy@web01:2222/srv/www",
	} {
		if _, _, ok := Parse(in); !ok {
			t.Errorf("Parse(%q) did not parse as a remote target", in)
		}
	}
}
