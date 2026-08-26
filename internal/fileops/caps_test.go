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
		table := byName(capabilityTable(), c.Group, c.Name)
		switch {
		case len(table.Needs) == 0 && len(table.AnyOf) == 0:
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
			if want := len(table.Needs) + len(table.AnyOf); len(c.Missing) != want {
				t.Errorf("%s/%s: missing = %v, want %d entries", c.Group, c.Name, c.Missing, want)
			}
		}
	}
}

// A missing tool has to name itself and say what to do, since the overlay is
// the only place the user finds out before an operation fails.
func TestCapabilityDetail(t *testing.T) {
	// 7-Zip ships under three names; missing means none of them is there.
	only7z := func(bin string) bool {
		switch bin {
		case "7z", "7zz", "7za":
			return false
		}
		return true
	}

	var short, full string
	for _, c := range capabilities(only7z) {
		if c.Name == ".7z" && c.Group == "Unpack" {
			short, full = c.Detail(), c.DetailFull()
		}
	}
	if !strings.Contains(short, "7z") {
		t.Fatalf("detail for missing 7z = %q, want the binary named", short)
	}
	if !strings.Contains(full, "7z") || !strings.Contains(full, "install") {
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

// 7-Zip is packaged under three different command names. A machine with only
// one of them can do everything, and the overlay names the one it will run.
func TestCapabilitiesResolveSevenZipVariants(t *testing.T) {
	for _, bin := range []string{"7z", "7zz", "7za"} {
		t.Run(bin, func(t *testing.T) {
			only := func(b string) bool { return b == bin }

			var row Capability
			for _, c := range capabilities(only) {
				if c.Group == "Unpack" && c.Name == ".7z" {
					row = c
				}
			}
			if got := row.Status(); got != StatusOK {
				t.Fatalf("status with only %s installed = %v, want StatusOK", bin, got)
			}
			if got := row.Detail(); got != bin {
				t.Errorf("detail = %q, want the installed name %q", got, bin)
			}
		})
	}
}

// byName finds a row in the table, so a test can compare what a capability
// asked for against what probing made of it.
func byName(caps []Capability, group, name string) Capability {
	for _, c := range caps {
		if c.Group == group && c.Name == name {
			return c
		}
	}
	return Capability{}
}

// A .zip needs either Info-ZIP or 7-Zip, and the row names whichever is there.
func TestCapabilitiesEitherToolSatisfiesZip(t *testing.T) {
	only := func(want string) func(string) bool {
		return func(bin string) bool { return bin == want }
	}
	for _, tc := range []struct{ have, want string }{
		{"unzip", "unzip"},
		{"7zz", "7zz"},
	} {
		row := byName(capabilities(only(tc.have)), "Unpack", ".zip")
		if got := row.Status(); got != StatusOK {
			t.Errorf("unpacking .zip with only %s = %v, want StatusOK", tc.have, got)
		}
		if got := row.Detail(); got != tc.want {
			t.Errorf("detail with only %s = %q, want %q", tc.have, got, tc.want)
		}
	}

	row := byName(capabilities(haveNone), "Unpack", ".zip")
	if got := row.Status(); got != StatusMissing {
		t.Errorf("status with neither tool = %v, want StatusMissing", got)
	}
	if got := row.Detail(); !strings.Contains(got, " or ") {
		t.Errorf("detail with neither tool = %q, want both named as alternatives", got)
	}
}
