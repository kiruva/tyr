package remote

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

// Entry is one item in a remote directory. It mirrors the fields the pane
// displays; the pane converts these into its own entries.
type Entry struct {
	Name    string
	IsDir   bool
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
}

// Listing is the result of reading a remote directory. Dir is the canonical
// absolute path, which is how "", "~" and relative input get resolved.
type Listing struct {
	Dir     string
	Entries []Entry
}

// These run one short round trip for the UI to draw with, and there is no
// affordance to cancel one: the pane is simply waiting for its listing.
// A long-running transfer takes a real context; see transfer.go.
//
// List reads a remote directory in a single round trip: cd, report where that
// landed, then list. The GNU --time-style flag gives epoch timestamps; when the
// far side is BSD (macOS) ls rejects it and the fallback runs, in which case the
// three-column date is parsed instead.
func List(h Host, dir string) (Listing, error) {
	cd := `cd -- "$HOME"`
	if dir != "" {
		cd = "cd -- " + shQuote(dir)
	}
	script := cd + ` && pwd && { LC_ALL=C ls -lAn --time-style=+%s 2>/dev/null || LC_ALL=C ls -lAn; }`

	out, err := output(context.Background(), h, script)
	if err != nil {
		return Listing{}, err
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return Listing{}, errEmptyListing
	}

	l := Listing{Dir: strings.TrimSpace(lines[0])}
	for _, line := range lines[1:] {
		if e, ok := parseListLine(line); ok {
			l.Entries = append(l.Entries, e)
		}
	}
	return l, nil
}

// Stat reports whether a remote path exists and whether it is a directory.
func Stat(h Host, p string) (exists, isDir bool, err error) {
	script := "if [ -d " + shQuote(p) + " ]; then echo dir; elif [ -e " + shQuote(p) +
		" ]; then echo file; else echo none; fi"
	out, err := output(context.Background(), h, script)
	if err != nil {
		return false, false, err
	}
	switch strings.TrimSpace(out) {
	case "dir":
		return true, true, nil
	case "file":
		return true, false, nil
	default:
		return false, false, nil
	}
}

// AnyExist reports whether any of names already exists under dir — the remote
// half of the overwrite warning.
func AnyExist(h Host, dir string, names []string) (bool, error) {
	var b strings.Builder
	b.WriteString("cd -- " + shQuote(dir) + " && for n in " + quoteAll(names) + "; do ")
	b.WriteString(`[ -e "$n" ] && { echo yes; break; }; done; :`)

	out, err := output(context.Background(), h, b.String())
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "yes", nil
}

var errEmptyListing = errorString("no output from ls")

type errorString string

func (e errorString) Error() string { return string(e) }

// parseListLine turns one `ls -lAn` row into an Entry.
//
//	drwxr-xr-x  2 1000 1000 4096 1712345678 some dir      (GNU, epoch)
//	drwxr-xr-x  2 1000 1000 4096 Apr  5 09:31 some dir    (BSD, 3 date columns)
//	lrwxrwxrwx  1 1000 1000    7 1712345678 link -> target
//	crw-rw-rw-  1 0    0    1, 3 1712345678 null          (device: size is 2 columns)
func parseListLine(line string) (Entry, bool) {
	if line == "" || strings.HasPrefix(line, "total ") {
		return Entry{}, false
	}

	perms, rest, ok := nextField(line)
	if !ok || len(perms) < 10 {
		return Entry{}, false
	}

	// links, uid, gid
	for range 3 {
		if _, rest, ok = nextField(rest); !ok {
			return Entry{}, false
		}
	}

	sizeField, rest, ok := nextField(rest)
	if !ok {
		return Entry{}, false
	}
	if strings.HasSuffix(sizeField, ",") { // device node: "1, 3"
		if _, rest, ok = nextField(rest); !ok {
			return Entry{}, false
		}
		sizeField = "0"
	}
	size, _ := strconv.ParseInt(sizeField, 10, 64)

	stamp, rest, ok := nextField(rest)
	if !ok {
		return Entry{}, false
	}
	var mtime time.Time
	if epoch, err := strconv.ParseInt(stamp, 10, 64); err == nil {
		mtime = time.Unix(epoch, 0)
	} else {
		// BSD layout: "Apr  5 09:31" or "Apr  5  2024" — two more columns.
		day, rest2, ok2 := nextField(rest)
		if !ok2 {
			return Entry{}, false
		}
		timeOrYear, rest3, ok3 := nextField(rest2)
		if !ok3 {
			return Entry{}, false
		}
		rest = rest3
		mtime = parseBSDTime(stamp, day, timeOrYear)
	}

	name := strings.TrimLeft(rest, " \t")
	if name == "" || name == "." || name == ".." {
		return Entry{}, false
	}

	e := Entry{Name: name, Size: size, ModTime: mtime, Mode: parseMode(perms)}
	if e.Mode&os.ModeSymlink != 0 {
		// "link -> target": keep the link's own name.
		if before, _, found := strings.Cut(name, " -> "); found {
			e.Name = before
		}
	}
	e.IsDir = perms[0] == 'd'
	return e, true
}

// parseBSDTime reconstructs the timestamp from BSD ls's three date columns.
// Recent entries carry a time and no year, which ls means as "within the last
// six months" — a date that would land in the future belongs to last year.
func parseBSDTime(month, day, timeOrYear string) time.Time {
	if t, err := time.ParseInLocation("Jan 2 2006", month+" "+day+" "+timeOrYear, time.Local); err == nil {
		return t
	}
	t, err := time.ParseInLocation("Jan 2 15:04", month+" "+day+" "+timeOrYear, time.Local)
	if err != nil {
		return time.Time{}
	}
	now := time.Now()
	t = t.AddDate(now.Year(), 0, 0)
	if t.After(now.AddDate(0, 0, 1)) {
		t = t.AddDate(-1, 0, 0)
	}
	return t
}

// nextField splits off the next whitespace-separated token, returning the
// remainder with its leading whitespace intact so a name can keep its spaces.
func nextField(s string) (field, rest string, ok bool) {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return "", "", false
	}
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, "", true
	}
	return s[:i], s[i:], true
}

// parseMode converts the rwx string into the bits the pane cares about.
func parseMode(perms string) os.FileMode {
	var m os.FileMode
	switch perms[0] {
	case 'd':
		m |= os.ModeDir
	case 'l':
		m |= os.ModeSymlink
	case 'c':
		m |= os.ModeCharDevice | os.ModeDevice
	case 'b':
		m |= os.ModeDevice
	case 'p':
		m |= os.ModeNamedPipe
	case 's':
		m |= os.ModeSocket
	}
	for i, bit := range []os.FileMode{0400, 0200, 0100, 0040, 0020, 0010, 0004, 0002, 0001} {
		if len(perms) > i+1 && perms[i+1] != '-' {
			m |= bit
		}
	}
	return m
}
