package fileops

import (
	"os/exec"
	"strings"
)

// Every archive operation shells out to a command-line tool, so what tyr can do
// depends on what is installed. Capabilities() probes PATH and reports it, which
// is what the capabilities overlay shows: a missing tool becomes visible before
// an operation fails halfway through.

// Status is how a capability turned out on this machine.
type Status int

const (
	StatusOK      Status = iota // every tool it needs is on PATH
	StatusPartial               // an optional helper is missing; it may still work
	StatusMissing               // a required tool is not on PATH
)

// Capability is one thing tyr can do, and the tools it needs to do it.
type Capability struct {
	Group    string   // section it belongs to in the overlay
	Name     string   // what the user can (or cannot) do
	Needs    []string // binaries it runs, in the order tyr calls them
	Optional bool     // missing tools degrade rather than break the capability
	Absent   bool     // tyr has no support for this at all; no tool would help
	Hint     string   // what to do about it, shown when something is missing

	Missing []string // the Needs not found on PATH, filled in by Capabilities
}

// Status reports whether the capability is usable on this machine.
func (c Capability) Status() Status {
	switch {
	case c.Absent:
		return StatusMissing
	case len(c.Missing) == 0:
		return StatusOK
	case c.Optional:
		return StatusPartial
	default:
		return StatusMissing
	}
}

// Detail is the one-line explanation shown next to the capability: the tools it
// uses when they are all there, what is missing when they are not.
func (c Capability) Detail() string {
	switch {
	case c.Absent:
		return c.Hint
	case len(c.Needs) == 0:
		return "built in"
	case len(c.Missing) == 0:
		return strings.Join(c.Needs, ", ")
	default:
		return "no " + strings.Join(c.Missing, ", ")
	}
}

// DetailFull is Detail with the hint about what to do appended. The overlay
// shows it when the window is wide enough and falls back to Detail when not,
// so the missing binary is the part that always survives a narrow terminal.
func (c Capability) DetailFull() string {
	if c.Absent || len(c.Missing) == 0 || c.Hint == "" {
		return c.Detail()
	}
	return c.Detail() + " · " + c.Hint
}

// Capabilities probes PATH and reports what tyr can do here. Each binary is
// looked up once however many capabilities need it.
func Capabilities() []Capability {
	return capabilities(func(bin string) bool {
		_, err := exec.LookPath(bin)
		return err == nil
	})
}

// capabilities is Capabilities with the PATH lookup injected, for testing.
func capabilities(have func(string) bool) []Capability {
	caps := capabilityTable()
	found := make(map[string]bool, 8)
	for i := range caps {
		for _, bin := range caps[i].Needs {
			ok, seen := found[bin]
			if !seen {
				ok = have(bin)
				found[bin] = ok
			}
			if !ok {
				caps[i].Missing = append(caps[i].Missing, bin)
			}
		}
	}
	return caps
}

// capabilityTable lists every capability in overlay order. The Needs of each
// row are the binaries the matching code path actually runs, so this table has
// to move whenever archive.go, member.go or remote/transfer.go changes tools.
func capabilityTable() []Capability {
	const (
		p7zip = "install p7zip"
		unrar = "install unrar"
		zip   = "install zip"
		tarh  = "install tar"
	)
	return []Capability{
		{Group: "Pack", Name: "pack to .tar.gz", Needs: []string{"tar"}, Hint: tarh},
		{Group: "Pack", Name: "add files to .tar*", Needs: []string{"tar"}, Hint: tarh},
		{Group: "Pack", Name: "add files to .zip", Needs: []string{"zip"}, Hint: zip},

		{Group: "Unpack", Name: ".tar .tgz .txz …", Needs: []string{"tar"}, Hint: tarh},
		{Group: "Unpack", Name: ".zip", Needs: []string{"unzip"}, Hint: zip},
		{Group: "Unpack", Name: ".7z", Needs: []string{"7z"}, Hint: p7zip},
		{Group: "Unpack", Name: ".rar", Needs: []string{"unrar"}, Hint: unrar},

		{Group: "Browse & edit inside", Name: ".tar family", Needs: []string{"tar"}, Hint: tarh},
		{Group: "Browse & edit inside", Name: ".zip", Needs: []string{"zipinfo", "unzip", "zip"}, Hint: zip},
		{Group: "Browse & edit inside", Name: ".7z, .rar", Absent: true, Hint: "unpack to disk first"},

		{Group: "tar compressors", Name: ".gz", Needs: []string{"gzip"}, Optional: true, Hint: "if tar shells out"},
		{Group: "tar compressors", Name: ".bz2", Needs: []string{"bzip2"}, Optional: true, Hint: "if tar shells out"},
		{Group: "tar compressors", Name: ".xz", Needs: []string{"xz"}, Optional: true, Hint: "if tar shells out"},
		{Group: "tar compressors", Name: ".zst", Needs: []string{"zstd"}, Optional: true, Hint: "if tar shells out"},

		{Group: "Remote (SSH)", Name: "connect, browse, delete", Needs: nil},
		{Group: "Remote (SSH)", Name: "copy / move on the host", Needs: nil},
		{Group: "Remote (SSH)", Name: "upload / download", Needs: []string{"tar"}, Hint: "host needs tar too"},

		{Group: "Local", Name: "copy, move, delete", Needs: nil},
		{Group: "Local", Name: "view / edit files", Needs: nil},
		{Group: "Local", Name: "rename, batch, undo", Needs: nil},
	}
}
