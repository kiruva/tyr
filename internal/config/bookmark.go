package config

import (
	"fmt"
	"sort"
	"strings"
)

// Bookmarks are stored the same way connections are — one flat key each, so the
// config file stays something a person can read and edit:
//
//	bookmark.src = /home/kim/src
//	bookmark.dl = /home/kim/downloads

const bookmarkPrefix = "bookmark."

// Bookmark is a saved directory and the name it answers to.
type Bookmark struct {
	Name string
	Path string
}

// ValidBookmarkName reports whether name can be used as a config key.
func ValidBookmarkName(name string) error { return validKeyName(name) }

// Bookmarks lists the saved directories, by name.
func Bookmarks() ([]Bookmark, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	pairs, err := readPairs(path)
	if err != nil {
		return nil, err
	}
	return bookmarksFrom(pairs), nil
}

func bookmarksFrom(pairs map[string]string) []Bookmark {
	var out []Bookmark
	for key, value := range pairs {
		if len(key) <= len(bookmarkPrefix) || !strings.EqualFold(key[:len(bookmarkPrefix)], bookmarkPrefix) {
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, Bookmark{Name: key[len(bookmarkPrefix):], Path: value})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// SaveBookmark writes or replaces one, leaving everything else in the file
// untouched.
func SaveBookmark(b Bookmark) error {
	if err := ValidBookmarkName(b.Name); err != nil {
		return err
	}
	if strings.TrimSpace(b.Path) == "" {
		return fmt.Errorf("a bookmark needs a directory")
	}
	return rewrite(func(pairs map[string]string) {
		key := bookmarkPrefix + strings.TrimSpace(b.Name)
		deleteKey(pairs, key)
		pairs[key] = b.Path
	})
}

// DeleteBookmark removes one.
func DeleteBookmark(name string) error {
	return rewrite(func(pairs map[string]string) {
		deleteKey(pairs, bookmarkPrefix+strings.TrimSpace(name))
	})
}

// deleteKey removes one exact key, however it happens to be spelled in the
// file. Deleting by prefix would take "bookmark.srcold" along with "bookmark.src".
func deleteKey(pairs map[string]string, key string) {
	for k := range pairs {
		if strings.EqualFold(k, key) {
			delete(pairs, k)
		}
	}
}

// validKeyName is the rule both connections and bookmarks follow: whatever
// would make the "key = value" file ambiguous is out.
func validKeyName(name string) error {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return fmt.Errorf("name cannot be empty")
	case strings.ContainsAny(trimmed, "=#"):
		return fmt.Errorf("name cannot contain '=' or '#'")
	case strings.ContainsAny(trimmed, " \t"):
		return fmt.Errorf("name cannot contain spaces")
	default:
		return nil
	}
}
