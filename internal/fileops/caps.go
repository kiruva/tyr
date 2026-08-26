package fileops

import "strings"

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
	Group string   // section it belongs to in the overlay
	Name  string   // what the user can (or cannot) do
	Needs []string // binaries it runs, in the order tyr calls them
	// AnyOf are binaries that can each do the job on their own — a .zip is
	// unpacked by unzip or by 7-Zip, and either one is enough.
	AnyOf    []string
	Optional bool   // missing tools degrade rather than break the capability
	Absent   bool   // tyr has no support for this at all; no tool would help
	Hint     string // what to do about it, shown when something is missing

	Missing []string // what was not found on PATH, filled in by Capabilities
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
	case len(c.Missing) > 0 && len(c.Needs) == 0 && len(c.AnyOf) > 0:
		// Nothing required, one of several would have done: name them all.
		return "no " + strings.Join(c.Missing, " or ")
	case len(c.Missing) > 0:
		return "no " + strings.Join(c.Missing, ", ")
	case len(c.Needs) == 0:
		return "built in"
	default:
		return strings.Join(c.Needs, ", ")
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
	return capabilities(installed)
}

// capabilities is Capabilities with the PATH lookup injected, for testing. A
// tool that ships under several names (7z / 7zz / 7za) is reported under the
// name that is actually installed, so the row names the command that will run.
func capabilities(have func(string) bool) []Capability {
	caps := capabilityTable()
	found := make(map[string]string, 8)
	resolve := func(bin string) string {
		real, seen := found[bin]
		if !seen {
			real = resolveTool(bin, have)
			found[bin] = real
		}
		return real
	}

	for i := range caps {
		for j, bin := range caps[i].Needs {
			real := resolve(bin)
			if real == "" {
				caps[i].Missing = append(caps[i].Missing, bin)
				continue
			}
			caps[i].Needs[j] = real
		}
		if len(caps[i].AnyOf) == 0 {
			continue
		}
		// Whichever of the alternatives is installed is the one that will run;
		// only when none is does the row name them all.
		if pick := firstResolved(caps[i].AnyOf, resolve); pick != "" {
			caps[i].Needs = append(caps[i].Needs, pick)
		} else {
			caps[i].Missing = append(caps[i].Missing, caps[i].AnyOf...)
		}
	}
	return caps
}

func firstResolved(bins []string, resolve func(string) string) string {
	for _, b := range bins {
		if real := resolve(b); real != "" {
			return real
		}
	}
	return ""
}

// capabilityTable lists every capability in overlay order. The Needs of each
// row are the binaries the matching code path actually runs, so this table has
// to move whenever archive.go, member.go or remote/transfer.go changes tools.
func capabilityTable() []Capability {
	const (
		p7zip = "install 7-Zip"
		unrar = "install unrar"
		zip   = "install zip"
		tarh  = "install tar"
	)
	return []Capability{
		{Group: "Pack", Name: ".tar.gz", Needs: []string{"tar", "gzip"}, Hint: "install gzip"},
		{Group: "Pack", Name: ".tar.bz2", Needs: []string{"tar", "bzip2"}, Hint: "install bzip2"},
		{Group: "Pack", Name: ".tar.xz", Needs: []string{"tar", "xz"}, Hint: "install xz"},
		{Group: "Pack", Name: ".tar.zst", Needs: []string{"tar", "zstd"}, Hint: "install zstd"},
		{Group: "Pack", Name: ".tar", Needs: []string{"tar"}, Hint: tarh},
		{Group: "Pack", Name: ".zip", AnyOf: []string{"zip", "7z"}, Hint: zip},
		{Group: "Pack", Name: ".7z", Needs: []string{"7z"}, Hint: p7zip},
		{Group: "Pack", Name: "add files to .tar*", Needs: []string{"tar"}, Hint: tarh},
		{Group: "Pack", Name: "add files to .zip", Needs: []string{"zip"}, Hint: zip},

		{Group: "Passwords", Name: "AES-256 .7z / .zip", Needs: []string{"7z"}, Hint: p7zip},
		{Group: "Passwords", Name: "ZipCrypto .zip (weak)", Needs: []string{"zip"}, Hint: zip},
		{Group: "Passwords", Name: "unpack encrypted .zip", AnyOf: []string{"7z", "unzip"}, Hint: "7-Zip reads AES"},
		{Group: "Passwords", Name: "unpack encrypted .7z", Needs: []string{"7z"}, Hint: p7zip},
		{Group: "Passwords", Name: "unpack encrypted .rar", Needs: []string{"unrar"}, Hint: unrar},

		{Group: "Unpack", Name: ".tar .tgz .txz …", Needs: []string{"tar"}, Hint: tarh},
		{Group: "Unpack", Name: ".zip", AnyOf: []string{"unzip", "7z"}, Hint: zip},
		{Group: "Unpack", Name: ".7z", Needs: []string{"7z"}, Hint: p7zip},
		{Group: "Unpack", Name: ".rar", Needs: []string{"unrar"}, Hint: unrar},

		{Group: "Browse & edit inside", Name: ".tar family", Needs: []string{"tar"}, Hint: tarh},
		{Group: "Browse & edit inside", Name: ".zip", Needs: []string{"zipinfo", "unzip", "zip"}, Hint: zip},
		{Group: "Browse & edit inside", Name: ".7z, .rar", Absent: true, Hint: "unpack to disk first"},

		{Group: "Remote (SSH)", Name: "connect, browse, delete", Needs: nil},
		{Group: "Remote (SSH)", Name: "copy / move on the host", Needs: nil},
		{Group: "Remote (SSH)", Name: "upload / download", Needs: []string{"tar"}, Hint: "host needs tar too"},

		{Group: "Local", Name: "copy, move, delete", Needs: nil},
		{Group: "Local", Name: "view / edit files", Needs: nil},
		{Group: "Local", Name: "rename, batch, undo", Needs: nil},
		{Group: "Local", Name: "trash & restore", Needs: nil},
		{Group: "Local", Name: "compare & synchronize", Needs: nil},
		{Group: "Local", Name: "properties & chmod", Needs: nil},

		{Group: "Shell", Name: "drop to " + shellLabel, Needs: nil},
		{Group: "Shell", Name: "run a command", Needs: []string{shellBinary}, Hint: "no " + shellBinary + " on PATH"},
	}
}
