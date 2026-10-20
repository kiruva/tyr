package remote

import (
	"os"
	"slices"
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
// affordance to cancel one: the pane is simply waiting for its listing, for
// at most roundTripTimeout. A long-running transfer takes a real context; see
// transfer.go.
//
// List reads a remote directory in a single round trip: cd, report where that
// landed, then list. The GNU --time-style flag gives epoch timestamps; when the
// far side is BSD (macOS) ls rejects it and the fallback runs, in which case the
// three-column date is parsed instead.
//
// ls -l is the only metadata every host can produce, but its names cannot be
// trusted on their own: a name may begin with a space, contain a newline, or
// contain " -> ", and the row it is printed in reads the same as another name's.
// Deleting the entry such a row seemed to show would delete something else. So
// the script first prints the directory's real names, NUL-separated, using
// nothing but shell builtins, and an ls row is only listed when it names one
// of them unambiguously.
func List(h Host, dir string) (Listing, error) {
	cd := `cd -- "$HOME"`
	if dir != "" {
		cd = "cd -- " + shPath(dir)
	}
	script := listScript(cd)

	out, err := quickOutput(h, script)
	if err != nil {
		return Listing{}, err
	}
	return parseListing(out)
}

// listScript is List's script for the far side. It runs under sh whatever the
// login shell is: zsh, the default on a Mac, aborts the whole command on a glob
// that matches nothing (an empty directory, or one with no dotfiles), and the
// name loop is all globs.
func listScript(cd string) string {
	inner := "export LC_ALL=C; " + cd + ` && printf '%s\0' "$(pwd)" &&` +
		` for f in .* *; do case $f in .|..) continue;; esac;` +
		` if [ -e "$f" ] || [ -L "$f" ]; then printf '%s\0' "$f"; fi; done; printf '\0';` +
		` if ls -d --time-style=+%s . >/dev/null 2>&1; then ls -lAn --time-style=+%s; else ls -lAn; fi`
	return "exec sh -c " + shQuote(inner)
}

// parseListing reads List's output: the directory, NUL; each name in it, NUL
// after each; an empty name; then the ls rows.
func parseListing(out string) (Listing, error) {
	dir, rest, ok := strings.Cut(out, "\x00")
	if !ok || strings.TrimSpace(dir) == "" {
		return Listing{}, errEmptyListing
	}

	names := map[string]bool{}
	for {
		var name string
		if name, rest, ok = strings.Cut(rest, "\x00"); !ok {
			return Listing{}, errEmptyListing
		}
		if name == "" {
			break
		}
		names[name] = true
	}

	l := Listing{Dir: dir}
	seen := map[string]bool{}
	for _, line := range strings.Split(rest, "\n") {
		if e, ok := parseListLine(line, names); ok && !seen[e.Name] {
			seen[e.Name] = true
			l.Entries = append(l.Entries, e)
		}
	}
	// A name no row could be matched to (one with a newline in it) is still
	// there, and still the user's to see and remove: it is listed by its exact
	// name, without the size and date its row would have given it.
	var unmatched []string
	for n := range names {
		if !seen[n] {
			unmatched = append(unmatched, n)
		}
	}
	slices.Sort(unmatched)
	for _, n := range unmatched {
		l.Entries = append(l.Entries, Entry{Name: n})
	}
	return l, nil
}

var errEmptyListing = errorString("no output from ls")

type errorString string

func (e errorString) Error() string { return string(e) }

// parseListLine turns one `ls -lAn` row into an Entry, if the name at the end
// of it is one of names — exactly, or, for a symlink, followed by " -> target"
// with only one way to split it. A row that matches nothing (the second line
// of a name with a newline in it) or more than one way is left out.
//
//	drwxr-xr-x  2 1000 1000 4096 1712345678 some dir      (GNU, epoch)
//	drwxr-xr-x  2 1000 1000 4096 Apr  5 09:31 some dir    (BSD, 3 date columns)
//	lrwxrwxrwx  1 1000 1000    7 1712345678 link -> target
//	crw-rw-rw-  1 0    0    1, 3 1712345678 null          (device: size is 2 columns)
func parseListLine(line string, names map[string]bool) (Entry, bool) {
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

	// One space separates the last column from the name; any more belong to
	// the name.
	shown, ok := strings.CutPrefix(rest, " ")
	if !ok {
		return Entry{}, false
	}
	mode := parseMode(perms)
	name, ok := matchName(shown, names, mode&os.ModeSymlink != 0)
	if !ok {
		return Entry{}, false
	}
	return Entry{Name: name, Size: size, ModTime: mtime, Mode: mode, IsDir: perms[0] == 'd'}, true
}

// matchName finds which of names an ls row shows. A symlink's row carries
// " -> target" after its name, and either side can contain " -> " too, so
// every split is tried and only a single match is believed.
func matchName(shown string, names map[string]bool, symlink bool) (string, bool) {
	if !symlink {
		return shown, names[shown]
	}
	found := ""
	if names[shown] {
		found = shown // an ls that does not print the target
	}
	for i := 0; ; {
		j := strings.Index(shown[i:], " -> ")
		if j < 0 {
			break
		}
		if n := shown[:i+j]; names[n] {
			if found != "" {
				return "", false // two names fit: do not guess
			}
			found = n
		}
		i += j + 1
	}
	return found, found != ""
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
