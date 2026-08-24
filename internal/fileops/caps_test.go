package fileops

import (
	"strings"
	"testing"
)

// haveAll / haveNone stand in for the PATH lookup.
func haveAll(string) bool  { return true }
func haveNone(string) bool { return false }

func TestCapabilitiesAllToolsPresent(t *testing.T) {
	caps := capabilities(haveAll)
	if len(caps) == 0 {
		t.Fatal("no capabilities reported")
	}
	for _, c := range caps {
		if c.Absent {
			continue
		}
		if got := c.Status(); got != StatusOK {
			t.Errorf("%s/%s: status = %v, want StatusOK", c.Group, c.Name, got)
		}
		if len(c.Missing) != 0 {
			t.Errorf("%s/%s: missing = %v, want none", c.Group, c.Name, c.Missing)
		}
	}
}

func TestCapabilitiesNoToolsPresent(t *testing.T) {
	for _, c := range capabilities(haveNone) {
		switch {
		case len(c.Needs) == 0:
			// built-in or unsupported: PATH cannot change the answer
			if c.Absent && c.Status() != StatusMissing {
				t.Errorf("%s: unsupported capability reported as available", c.Name)
			}
			if !c.Absent && c.Status() != StatusOK {
				t.Errorf("%s: built-in capability needs no tools but is unavailable", c.Name)
			}
		case c.Optional:
			if got := c.Status(); got != StatusPartial {
				t.Errorf("%s/%s: status = %v, want StatusPartial", c.Group, c.Name, got)
			}
		default:
			if got := c.Status(); got != StatusMissing {
				t.Errorf("%s/%s: status = %v, want StatusMissing", c.Group, c.Name, got)
			}
			if len(c.Missing) != len(c.Needs) {
				t.Errorf("%s/%s: missing = %v, want all of %v", c.Group, c.Name, c.Missing, c.Needs)
			}
		}
	}
}

// A missing tool has to name itself and say what to do, since the overlay is
// the only place the user finds out before an operation fails.
func TestCapabilityDetail(t *testing.T) {
	only7z := func(bin string) bool { return bin != "7z" }

	var short, full string
	for _, c := range capabilities(only7z) {
		if c.Name == ".7z" && c.Group == "Unpack" {
			short, full = c.Detail(), c.DetailFull()
		}
	}
	if !strings.Contains(short, "7z") {
		t.Fatalf("detail for missing 7z = %q, want the binary named", short)
	}
	if !strings.Contains(full, "7z") || !strings.Contains(full, "p7zip") {
		t.Fatalf("full detail for missing 7z = %q, want the binary and the hint", full)
	}
	if len(full) <= len(short) {
		t.Fatalf("full detail %q should add the hint to %q", full, short)
	}

	ok := Capability{Name: "x", Needs: []string{"tar", "zip"}}
	if want := "tar, zip"; ok.Detail() != want {
		t.Fatalf("detail = %q, want %q", ok.Detail(), want)
	}
	builtin := Capability{Name: "y"}
	if want := "built in"; builtin.Detail() != want {
		t.Fatalf("detail = %q, want %q", builtin.Detail(), want)
	}
	// Nothing is missing, so there is no hint to add.
	if ok.DetailFull() != ok.Detail() || builtin.DetailFull() != builtin.Detail() {
		t.Fatal("DetailFull should match Detail when nothing is missing")
	}
}

// The table drives the overlay's two-column layout, which groups rows by the
// Group field in table order: a group must not be split across two runs.
func TestCapabilityGroupsAreContiguous(t *testing.T) {
	seen := map[string]bool{}
	prev := ""
	for _, c := range capabilities(haveAll) {
		if c.Group == prev {
			continue
		}
		if seen[c.Group] {
			t.Fatalf("group %q appears in two runs", c.Group)
		}
		seen[c.Group] = true
		prev = c.Group
	}
}

// Every row is either built in, unsupported, or backed by tools tyr runs.
func TestCapabilityTableIsWellFormed(t *testing.T) {
	for _, c := range capabilityTable() {
		if c.Group == "" || c.Name == "" {
			t.Errorf("row %+v: group and name are required", c)
		}
		if c.Absent && (len(c.Needs) > 0 || c.Hint == "") {
			t.Errorf("%s: an unsupported row needs a hint and no tools", c.Name)
		}
		if !c.Absent && len(c.Needs) > 0 && c.Hint == "" {
			t.Errorf("%s/%s: a row with tools needs a hint for when they are missing", c.Group, c.Name)
		}
	}
}
