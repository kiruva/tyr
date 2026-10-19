//go:build !windows

package app

import (
	"os"
	"os/user"
	"strconv"
	"sync"
	"syscall"
)

// ownership resolves the owning user and group, and the link count, from what
// the platform's stat gave us. Where that is not available the names come back
// empty and the dialog leaves the rows out.
func ownership(info os.FileInfo) (owner, group string, links uint64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", "", 0
	}
	// Nlink is uint16 on darwin and the BSDs and uint64 on Linux, so the
	// conversion is redundant on exactly one of the platforms this file builds
	// for.
	links = uint64(st.Nlink) //nolint:unconvert // needed off Linux

	uid, gid := strconv.FormatUint(uint64(st.Uid), 10), strconv.FormatUint(uint64(st.Gid), 10)
	return idName(userNames, uid, func(id string) (string, error) {
			u, err := user.LookupId(id)
			if err != nil {
				return "", err
			}
			return u.Username, nil
		}), idName(groupNames, gid, func(id string) (string, error) {
			g, err := user.LookupGroupId(id)
			if err != nil {
				return "", err
			}
			return g.Name, nil
		}), links
}

// Looking a user or group up can mean asking a directory server, and the
// answer does not change while tyr runs, so each id is looked up once.
var (
	idMu       sync.Mutex
	userNames  = map[string]string{}
	groupNames = map[string]string{}
)

// idName is the name for id, looked up with lookup the first time; an id that
// has no name is shown as itself.
func idName(names map[string]string, id string, lookup func(string) (string, error)) string {
	idMu.Lock()
	defer idMu.Unlock()
	if name, ok := names[id]; ok {
		return name
	}
	name, err := lookup(id)
	if err != nil {
		name = id
	}
	names[id] = name
	return name
}
