//go:build !windows

package app

import (
	"os"
	"os/user"
	"strconv"
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
	links = uint64(st.Nlink)

	uid, gid := strconv.FormatUint(uint64(st.Uid), 10), strconv.FormatUint(uint64(st.Gid), 10)
	owner, group = uid, gid
	if u, err := user.LookupId(uid); err == nil {
		owner = u.Username
	}
	if g, err := user.LookupGroupId(gid); err == nil {
		group = g.Name
	}
	return owner, group, links
}
