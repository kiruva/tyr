package pane

import (
	"os"
	"time"
)

// Entry is a single item shown in a pane.
type Entry struct {
	Name    string
	IsDir   bool
	Size    int64
	Mode    os.FileMode
	ModTime time.Time

	// HasSize reports whether Size is worth showing. A file always has one; a
	// directory only once its contents have been measured (see dirsize.go).
	HasSize bool
}

// isDotfile reports whether the entry is a hidden dotfile (never true for "..").
func (e Entry) isDotfile() bool {
	return e.Name != ".." && len(e.Name) > 0 && e.Name[0] == '.'
}

// readRaw reads path and returns its entries, with ".." prepended unless path is
// a filesystem root. No filtering or sorting is applied here — the pane does that.
func readRaw(path string, hasParent bool) ([]Entry, error) {
	dirents, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(dirents)+1)
	if hasParent {
		entries = append(entries, Entry{Name: "..", IsDir: true})
	}
	for _, d := range dirents {
		e := Entry{Name: d.Name(), IsDir: d.IsDir(), HasSize: !d.IsDir()}
		if info, err := d.Info(); err == nil {
			e.Size = info.Size()
			e.Mode = info.Mode()
			e.ModTime = info.ModTime()
		}
		entries = append(entries, e)
	}
	return entries, nil
}
